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
reflect.DeepEqual is wrong for protobuf messages (which carry unexported internal state)
and for func fields (never equal unless both are nil), so a type that reaches the
fallback with such fields either panics or silently reports every object as changed,
producing an endless recomputation loop.`,
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      runEqual,
}

// maxDepth bounds the search for equality hazards in deeply nested types.
const maxDepth = 8

func runEqual(pass *analysis.Pass) (any, error) {
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	// A type may back many collections; report each distinct problem once per package.
	seen := map[string]bool{}
	for call := range insp.PreorderSeq((*ast.CallExpr)(nil)) {
		call := call.(*ast.CallExpr)
		elem, ok := constructedElem(pass.TypesInfo, call)
		if !ok || IsTypeParam(elem) {
			continue
		}
		if seen[TypeName(elem)] {
			continue
		}
		seen[TypeName(elem)] = true
		checkEquality(pass, call.Pos(), elem)
	}
	return nil, nil
}

func checkEquality(pass *analysis.Pass, pos token.Pos, elem types.Type) {
	name := TypeName(elem)

	// An Equals method that krt cannot dispatch to is worse than none at all: the author
	// believes comparison is handled, but krt silently uses reflect.DeepEqual.
	if EqualsMethod(elem) == nil {
		if declared := DeclaredEquals(elem); declared != nil {
			pass.Reportf(pos,
				"krt collection element type %s declares %s, but krt.Equal cannot dispatch to it: "+
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
		pass.Reportf(pos,
			"krt collection element type %s embeds a protobuf message; krt.Equal panics on this "+
				"rather than compare it incorrectly. Implement `Equals(%s) bool`",
			name, name)
		return
	}
	if IsProtoMessage(elem) {
		// A genuine protobuf message; krt.Equal uses proto.Equal.
		return
	}

	for _, h := range equalityHazards(elem) {
		pass.Reportf(pos,
			"krt collection element type %s has no Equals method, so krt compares it with "+
				"reflect.DeepEqual, which is unreliable for %s (field %s). Implement `Equals(%s) bool`",
			name, h.kind, h.path, name)
	}
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
