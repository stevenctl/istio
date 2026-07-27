// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package krtlint contains static analysis checks for correct usage of the krt
// (Kubernetes declarative controller runtime) library.
//
// krt relies on runtime reflection and type assertions for several of its core
// operations: deriving an object's key, comparing two objects for equality, and
// extracting labels or selectors during a filtered Fetch. When a type does not
// satisfy the (unenforceable, because Go generics cannot express them) implicit
// constraints, the failure mode is either a panic deep inside the framework or,
// worse, silently incorrect change detection. These analyzers recover those
// constraints statically.
package krtlint

import (
	"go/ast"
	"go/types"
	"strings"
	"sync"

	"golang.org/x/tools/go/analysis"
)

// PkgPath is the import path of the krt library.
const PkgPath = "istio.io/istio/pkg/kube/krt"

const (
	configPkgPath = "istio.io/istio/pkg/config"
	kubePkgPath   = "istio.io/istio/pkg/kube"
)

// NamedFrom reports whether t is the named type pkgPath.name, ignoring any type arguments.
func NamedFrom(t types.Type, pkgPath, name string) (*types.Named, bool) {
	n, ok := types.Unalias(t).(*types.Named)
	if !ok {
		return nil, false
	}
	obj := n.Obj()
	if obj.Pkg() == nil || obj.Pkg().Path() != pkgPath || obj.Name() != name {
		return nil, false
	}
	return n, true
}

// IsKrtType reports whether t is the named krt type with the given name.
func IsKrtType(t types.Type, name string) bool {
	_, ok := NamedFrom(t, PkgPath, name)
	return ok
}

// collectionLike are the krt types that hold a single element type. RecomputeProtected is
// excluded: it stores its value in a plain field and never keys or compares it.
var collectionLike = []string{
	"Collection",
	"Singleton",
	"StaticCollection",
	"StaticSingleton",
}

// IsSingleton reports whether t is one of the krt single-value types, which expose Get.
func IsSingleton(t types.Type) bool {
	return IsKrtType(t, "Singleton") || IsKrtType(t, "StaticSingleton")
}

// IsTypeParam reports whether t is an unsubstituted generic type parameter. Constraints on
// such a type are the caller's responsibility, so generic helpers are not checked.
func IsTypeParam(t types.Type) bool {
	_, ok := types.Unalias(t).(*types.TypeParam)
	return ok
}

// CollectionElem returns the element type of a krt collection-like type.
func CollectionElem(t types.Type) (types.Type, bool) {
	for _, name := range collectionLike {
		n, ok := NamedFrom(t, PkgPath, name)
		if !ok {
			continue
		}
		if args := n.TypeArgs(); args != nil && args.Len() == 1 {
			return args.At(0), true
		}
	}
	return nil, false
}

// IsCollection reports whether t is a krt collection-like type.
func IsCollection(t types.Type) bool {
	_, ok := CollectionElem(t)
	return ok
}

// IsIndex reports whether t is a krt Index.
func IsIndex(t types.Type) bool {
	return IsKrtType(t, "Index")
}

// IsHandlerContext reports whether t is krt.HandlerContext.
func IsHandlerContext(t types.Type) bool {
	return IsKrtType(t, "HandlerContext")
}

// Method returns the method with the given name from the method set of t, or nil.
// Note this uses the *method set*, not the declared methods: for a value type T this
// excludes pointer-receiver methods, which matters because krt performs type
// assertions against boxed values.
func Method(t types.Type, name string) *types.Func {
	ms := types.NewMethodSet(t)
	for i := range ms.Len() {
		fn, ok := ms.At(i).Obj().(*types.Func)
		if ok && fn.Name() == name {
			return fn
		}
	}
	return nil
}

// HasMethod reports whether t's method set contains a method with the given name.
func HasMethod(t types.Type, name string) bool {
	return Method(t, name) != nil
}

// returnsStringMethod reports whether t has a method `name() string` taking no arguments.
func returnsStringMethod(t types.Type, name string) bool {
	fn := Method(t, name)
	if fn == nil {
		return false
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Params().Len() != 0 || sig.Results().Len() != 1 {
		return false
	}
	basic, ok := sig.Results().At(0).Type().(*types.Basic)
	return ok && basic.Kind() == types.String
}

// returnsStringMapMethod reports whether t has a method `name() map[string]string`.
func returnsStringMapMethod(t types.Type, name string) bool {
	fn := Method(t, name)
	if fn == nil {
		return false
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Params().Len() != 0 || sig.Results().Len() != 1 {
		return false
	}
	m, ok := types.Unalias(sig.Results().At(0).Type()).(*types.Map)
	if !ok {
		return false
	}
	key, ok := m.Key().(*types.Basic)
	if !ok || key.Kind() != types.String {
		return false
	}
	elem, ok := m.Elem().(*types.Basic)
	return ok && elem.Kind() == types.String
}

// isConfigConfig reports whether t is istio config.Config or *config.Config.
func isConfigConfig(t types.Type) bool {
	if p, ok := types.Unalias(t).(*types.Pointer); ok {
		t = p.Elem()
	}
	_, ok := NamedFrom(t, configPkgPath, "Config")
	return ok
}

// IsProtoMessage reports whether t implements proto.Message. Rather than resolving the
// protobuf package (which may not be imported by the package under analysis) we look for
// the ProtoReflect marker method, which only generated protobuf types have.
func IsProtoMessage(t types.Type) bool {
	fn := Method(t, "ProtoReflect")
	if fn == nil {
		return false
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Params().Len() != 0 || sig.Results().Len() != 1 {
		return false
	}
	res, ok := types.Unalias(sig.Results().At(0).Type()).(*types.Named)
	return ok && res.Obj().Name() == "Message"
}

// KeyStrategy describes how krt.GetKey will derive a key for a type.
type KeyStrategy string

const (
	// KeyNone means GetKey will panic on this type.
	KeyNone KeyStrategy = ""
	// KeyString means the type is the builtin string.
	KeyString KeyStrategy = "string"
	// KeyResourceNamer means the type implements ResourceName() string.
	KeyResourceNamer KeyStrategy = "ResourceName()"
	// KeyKubernetes means the type is a Kubernetes object.
	KeyKubernetes KeyStrategy = "Kubernetes object metadata"
	// KeyConfig means the type is an Istio config.Config.
	KeyConfig KeyStrategy = "config.Config"
	// KeyInternal means krt handles the type internally (collections, clients, apply configurations).
	KeyInternal KeyStrategy = "krt internal"
)

// KeyOf reports how krt.GetKey derives a key for values of type t, or KeyNone if it
// would panic.
//
// This deliberately mirrors GetKey's use of type assertions on a boxed value: only the
// *value* method set of t is consulted, so a ResourceName method declared on *T does not
// make T keyable.
func KeyOf(t types.Type) KeyStrategy {
	// `any(a).(string)` only matches the builtin string exactly; a defined string type
	// such as `type Host string` boxes as Host and does not match.
	if types.Identical(t, types.Typ[types.String]) {
		return KeyString
	}
	// controllers.Object, handled via cache.MetaNamespaceKeyFunc.
	if returnsStringMethod(t, "GetName") && returnsStringMethod(t, "GetNamespace") && HasMethod(t, "DeepCopyObject") {
		return KeyKubernetes
	}
	if isConfigConfig(t) {
		return KeyConfig
	}
	if returnsStringMethod(t, "ResourceName") {
		return KeyResourceNamer
	}
	// uidable: an unexported `uid()` method that only krt's own types can implement.
	// Collections themselves are keyed this way, including nested collections.
	if IsCollection(t) || HasMethod(t, "uid") {
		return KeyInternal
	}
	// kube.Client is keyed by its cluster ID.
	if _, ok := NamedFrom(t, kubePkgPath, "Client"); ok {
		return KeyInternal
	}
	if isApplyConfiguration(t) {
		return KeyInternal
	}
	return KeyNone
}

// isApplyConfiguration mirrors GetApplyConfigKey, which keys off the type name suffix.
func isApplyConfiguration(t types.Type) bool {
	base := t
	if p, ok := types.Unalias(base).(*types.Pointer); ok {
		base = p.Elem()
	}
	n, ok := types.Unalias(base).(*types.Named)
	if !ok {
		return false
	}
	return strings.HasSuffix(n.Obj().Name(), "ApplyConfiguration")
}

// EqualsMethod returns the Equals method that krt.Equal would dispatch to for values of
// type t, or nil if krt would fall back to proto.Equal / reflect.DeepEqual.
//
// krt.Equal tries Equaler[T] and Equaler[*T] against both the value and its address, so
// both value- and pointer-receiver declarations are accepted, but the parameter type must
// be exactly T or *T.
func EqualsMethod(t types.Type) *types.Func {
	for _, recv := range []types.Type{t, types.NewPointer(t)} {
		fn := Method(recv, "Equals")
		if fn == nil {
			continue
		}
		sig, ok := fn.Type().(*types.Signature)
		if !ok || sig.Params().Len() != 1 || sig.Results().Len() != 1 {
			continue
		}
		res, ok := sig.Results().At(0).Type().(*types.Basic)
		if !ok || res.Kind() != types.Bool {
			continue
		}
		param := sig.Params().At(0).Type()
		if types.Identical(param, t) || types.Identical(param, types.NewPointer(t)) {
			return fn
		}
	}
	return nil
}

// DeclaredEquals returns any method named Equals declared on t or *t, regardless of
// whether it has a signature krt can dispatch to.
func DeclaredEquals(t types.Type) *types.Func {
	for _, recv := range []types.Type{t, types.NewPointer(t)} {
		if fn := Method(recv, "Equals"); fn != nil {
			return fn
		}
	}
	return nil
}

// StructOf returns the struct underlying t, dereferencing a single pointer.
func StructOf(t types.Type) (*types.Struct, bool) {
	base := types.Unalias(t)
	if p, ok := base.(*types.Pointer); ok {
		base = types.Unalias(p.Elem())
	}
	s, ok := base.Underlying().(*types.Struct)
	return s, ok
}

// TypeName renders a type for use in diagnostics, without package qualification noise.
func TypeName(t types.Type) string {
	return types.TypeString(t, func(p *types.Package) string { return p.Name() })
}

// CalleeFrom returns the object called by call if it resolves to a function in pkgPath.
func CalleeFrom(info *types.Info, call *ast.CallExpr, pkgPath string) *types.Func {
	var id *ast.Ident
	switch fn := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		id = fn
	case *ast.SelectorExpr:
		id = fn.Sel
	case *ast.IndexExpr:
		id = identOf(fn.X)
	case *ast.IndexListExpr:
		id = identOf(fn.X)
	}
	if id == nil {
		return nil
	}
	obj, ok := info.Uses[id].(*types.Func)
	if !ok || obj.Pkg() == nil || obj.Pkg().Path() != pkgPath {
		return nil
	}
	return obj
}

func identOf(e ast.Expr) *ast.Ident {
	switch v := ast.Unparen(e).(type) {
	case *ast.Ident:
		return v
	case *ast.SelectorExpr:
		return v.Sel
	}
	return nil
}

// Analyzers returns every krt analyzer, each honoring the //nokrtlint directive.
func Analyzers() []*analysis.Analyzer {
	analyzers.Do(func() {
		for _, a := range all {
			withIgnores(a)
		}
	})
	return all
}

var (
	analyzers sync.Once
	all       = []*analysis.Analyzer{
		KeyAnalyzer,
		EqualAnalyzer,
		EqualsFieldsAnalyzer,
		FetchAnalyzer,
		FilterAnalyzer,
	}
)
