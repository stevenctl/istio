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

package krtlint

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

// EqualAnalyzer reports collection element types that krt cannot reliably compare.
var EqualAnalyzer = &analysis.Analyzer{
	Name: "krtequal",
	Doc: `check that krt collection element types can be compared reliably

krt decides whether an object changed with krt.Equal, which dispatches to an Equals
method if one is present and otherwise falls back to proto.Equal or reflect.DeepEqual.

Two things are reported. An Equals method krt cannot dispatch to, or an embedded protobuf
message, is a defect in the type declaration: the first is silently ignored in favour of
reflect.DeepEqual, the second panics. Each is reported once, since one fix serves every
collection built on that type.

Reaching the reflect.DeepEqual fallback with a protobuf, func or lock field is reported at
every construction site instead, because whether it costs anything depends on the
collection rather than the type: a transformation that passes an object straight through
compares the same pointer, while one that builds a fresh object every time does not. These
err towards reporting a change that did not happen, so the cost is recomputation rather
than data that never updates, and a collection that provably never compares anything can
carry an opt-out.`,
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      runEqual,
}

// maxDepth bounds the search for equality hazards in deeply nested types.
const maxDepth = 8

func runEqual(pass *analysis.Pass) (any, error) {
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	// A defect in the type declaration is reported once per package: one fix serves every
	// collection built on the type, so repeating it per construction site would only
	// multiply the same remedy. Whether an unguarded field actually costs anything is a
	// property of the collection rather than the type, so those are reported at every site,
	// each one its own judgment call and its own opt-out.
	declaredDefect := map[string]bool{}
	for call := range insp.PreorderSeq((*ast.CallExpr)(nil)) {
		call := call.(*ast.CallExpr)
		elem, ok := constructedElem(pass.TypesInfo, call)
		if !ok || IsTypeParam(elem) {
			continue
		}
		checkEquality(pass, call.Pos(), elem, declaredDefect)
	}
	return nil, nil
}

func checkEquality(pass *analysis.Pass, pos token.Pos, elem types.Type, declaredDefect map[string]bool) {
	name := TypeName(elem)
	once := func(format string, args ...any) {
		if declaredDefect[name] {
			return
		}
		declaredDefect[name] = true
		pass.Reportf(pos, format, args...)
	}

	// An Equals method that krt cannot dispatch to is worse than none at all: the author
	// believes comparison is handled, but krt silently uses reflect.DeepEqual.
	if EqualsMethod(elem) == nil {
		if declared := DeclaredEquals(elem); declared != nil {
			once("krt collection element type %s declares %s, but krt.Equal cannot dispatch to it: "+
				"the parameter must be exactly %s or *%s. krt will silently fall back to reflect.DeepEqual",
				name, signatureOf(declared), name, name)
			return
		}
	} else {
		// krt will use the custom implementation; nothing further to verify here.
		return
	}

	// A type that promotes ProtoReflect from an embedded message is not itself a message.
	// krt.Equal detects this at runtime and panics rather than risk a wrong answer.
	if embedsProto(elem) {
		once("krt collection element type %s embeds a protobuf message; krt.Equal panics on this "+
			"rather than compare it incorrectly. Implement `Equals(%s) bool`", name, name)
		return
	}
	if IsProtoMessage(elem) {
		// A genuine protobuf message; krt.Equal uses proto.Equal.
		return
	}

	remedy := remedyFor(pass, elem, name)
	for _, h := range equalityHazards(elem) {
		pass.Reportf(pos,
			"krt collection element type %s has no Equals method, so krt compares it with "+
				"reflect.DeepEqual. %s. %s",
			name, fmt.Sprintf(hazardEffects[h.kind], h.path), remedy)
	}
}

// remedyFor names the fix available at this site. Go only allows a method to be declared in
// the package that defines the type, so for an element type from elsewhere -- a Kubernetes
// CRD being the usual one -- telling the author to implement Equals asks for something the
// compiler will not accept.
func remedyFor(pass *analysis.Pass, elem types.Type, name string) string {
	if n := namedOf(elem); n != nil && n.Obj().Pkg() != nil && n.Obj().Pkg() != pass.Pkg {
		return "Equals cannot be declared on a type from another package: wrap it, or mark this " +
			"collection " + IgnoreDirective + " if it never compares anything"
	}
	return fmt.Sprintf("Implement `Equals(%s) bool`", name)
}

// hazardEffects says what reflect.DeepEqual actually gets wrong for each kind of field, keyed
// by hazard kind and taking the field path.
//
// All of them err in the same direction: a genuine difference is still caught, so the cost is
// recomputation that changes nothing rather than a change that never propagates. That is worth
// stating, because it is what makes an opt-out a reasonable answer at a site where the
// comparison provably cannot run.
var hazardEffects = map[string]string{
	"protobuf messages": "Field %s reaches a protobuf message, which carries unexported state that " +
		"marshaling writes in place, so two equal objects can compare unequal",
	"func values": "Field %s is a func value, which is never equal unless both sides are nil, so " +
		"every object looks changed on every recomputation",
	"synchronization primitives": "Field %s holds a synchronization primitive, whose lock state is " +
		"compared as if it were data",
}

type hazard struct {
	path string
	kind string
}

// equalityHazards finds fields reachable from t whose values reflect.DeepEqual cannot
// compare meaningfully. At most one hazard of each kind is reported to keep diagnostics
// actionable.
func equalityHazards(t types.Type) []hazard {
	found := map[string]hazard{}
	visited := map[string]bool{}
	walkEquality(t, "", 0, visited, found)
	out := make([]hazard, 0, len(found))
	for _, kind := range []string{"protobuf messages", "func values", "synchronization primitives"} {
		if h, ok := found[kind]; ok {
			out = append(out, h)
		}
	}
	return out
}

func walkEquality(t types.Type, path string, depth int, visited map[string]bool, found map[string]hazard) {
	if depth > maxDepth || t == nil {
		return
	}
	key := types.TypeString(t, nil) + "@" + path
	if visited[key] {
		return
	}
	visited[key] = true

	if path != "" {
		if IsProtoMessage(t) || IsProtoMessage(types.NewPointer(t)) {
			record(found, hazard{path: path, kind: "protobuf messages"})
			return
		}
		if isSyncPrimitive(t) {
			record(found, hazard{path: path, kind: "synchronization primitives"})
			return
		}
	}

	switch u := types.Unalias(t).(type) {
	case *types.Pointer:
		walkEquality(u.Elem(), path, depth+1, visited, found)
		return
	case *types.Named:
		walkEquality(u.Underlying(), path, depth+1, visited, found)
		return
	}

	switch u := t.Underlying().(type) {
	case *types.Struct:
		for i := range u.NumFields() {
			f := u.Field(i)
			child := f.Name()
			if path != "" {
				child = path + "." + f.Name()
			}
			walkEquality(f.Type(), child, depth+1, visited, found)
		}
	case *types.Slice:
		walkEquality(u.Elem(), path+"[]", depth+1, visited, found)
	case *types.Array:
		walkEquality(u.Elem(), path+"[]", depth+1, visited, found)
	case *types.Map:
		walkEquality(u.Key(), path+"[key]", depth+1, visited, found)
		walkEquality(u.Elem(), path+"[value]", depth+1, visited, found)
	case *types.Signature:
		if path != "" {
			record(found, hazard{path: path, kind: "func values"})
		}
	}
	// Interfaces are left alone: their dynamic type is unknown statically.
}

func record(found map[string]hazard, h hazard) {
	if _, ok := found[h.kind]; !ok {
		found[h.kind] = h
	}
}

func isSyncPrimitive(t types.Type) bool {
	n, ok := types.Unalias(t).(*types.Named)
	if !ok || n.Obj().Pkg() == nil {
		return false
	}
	switch n.Obj().Pkg().Path() {
	case "sync":
		switch n.Obj().Name() {
		case "Mutex", "RWMutex", "WaitGroup", "Once", "Map", "Cond", "Pool":
			return true
		}
	case "sync/atomic":
		return true
	}
	return false
}

// embedsProto reports whether t promotes ProtoReflect from an embedded message rather
// than being a protobuf message itself.
func embedsProto(t types.Type) bool {
	fn := Method(t, "ProtoReflect")
	if fn == nil {
		fn = Method(types.NewPointer(t), "ProtoReflect")
	}
	if fn == nil {
		return false
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok || sig.Recv() == nil {
		return false
	}
	return baseName(sig.Recv().Type()) != baseName(t)
}

func baseName(t types.Type) string {
	base := types.Unalias(t)
	if p, ok := base.(*types.Pointer); ok {
		base = types.Unalias(p.Elem())
	}
	n, ok := base.(*types.Named)
	if !ok {
		return types.TypeString(base, nil)
	}
	return types.TypeString(n.Obj().Type(), nil)
}

func signatureOf(fn *types.Func) string {
	sig := types.TypeString(fn.Type(), func(p *types.Package) string { return p.Name() })
	return "Equals" + strings.TrimPrefix(sig, "func")
}
