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
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

// KeyAnalyzer reports collection element types that krt cannot derive a key for.
var KeyAnalyzer = &analysis.Analyzer{
	Name: "krtkey",
	Doc: `check that krt collection element types have a derivable key

krt.GetKey panics when it cannot derive a key for an object, which happens the first
time an object flows through the collection. Because the requirement cannot be
expressed as a generic constraint it is invisible to the compiler. This reports
collection constructors whose element type would panic.`,
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      runKey,
}

// elementConstructors are the krt constructors whose element type is chosen by the caller
// rather than inherited from an existing collection, and whose result is stored in a keyed,
// change-detecting collection.
//
// Constructors such as JoinCollection are excluded because their element type is already
// checked where it was introduced. NewStatic and NewRecomputeProtected are excluded because
// they hold a single value directly, never deriving a key or comparing it.
var elementConstructors = map[string]bool{
	"NewCollection":           true,
	"NewManyCollection":       true,
	"NewSingleton":            true,
	"NewManyFromNothing":      true,
	"NewStaticCollection":     true,
	"MapCollection":           true,
	"NewStatusCollection":     true,
	"NewStatusManyCollection": true,
}

// constructedElems returns the element types produced by a krt collection constructor call.
// The status constructors return both a status and an output collection, so a single call
// can produce more than one.
func constructedElems(info *types.Info, call *ast.CallExpr) []types.Type {
	fn := CalleeFrom(info, call, PkgPath)
	if fn == nil || !elementConstructors[fn.Name()] {
		return nil
	}
	tv, ok := info.Types[call]
	if !ok {
		return nil
	}
	var out []types.Type
	tuple, ok := tv.Type.(*types.Tuple)
	if !ok {
		if elem, ok := CollectionElem(tv.Type); ok {
			out = append(out, elem)
		}
		return out
	}
	for i := range tuple.Len() {
		if elem, ok := CollectionElem(tuple.At(i).Type()); ok {
			out = append(out, elem)
		}
	}
	return out
}

func runKey(pass *analysis.Pass) (any, error) {
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	for call := range insp.PreorderSeq((*ast.CallExpr)(nil)) {
		call := call.(*ast.CallExpr)
		for _, elem := range constructedElems(pass.TypesInfo, call) {
			if IsTypeParam(elem) || KeyOf(elem) != KeyNone {
				continue
			}
			// A defined string type boxes as itself, so krt's `any(a).(string)` never matches
			// and suggesting ResourceName would miss the simpler fix.
			if basic, ok := elem.Underlying().(*types.Basic); ok && basic.Kind() == types.String {
				pass.Reportf(call.Pos(),
					"krt collection element type %s has no key: krt.GetKey accepts only the builtin "+
						"string, not a defined string type, and will panic. Convert to string, or "+
						"implement `ResourceName() string` on %s",
					TypeName(elem), TypeName(elem))
				continue
			}
			// A very common mistake: the accessor is declared on the pointer receiver, but the
			// collection holds values. krt asserts against the boxed value, so it never matches.
			if ptrOnlyKeyAccessor(elem) {
				pass.Reportf(call.Pos(),
					"krt collection element type %s has no key: ResourceName is declared on *%s, "+
						"but krt.GetKey inspects the value method set and will panic. "+
						"Use a value receiver, or make this a collection of *%s",
					TypeName(elem), TypeName(elem), TypeName(elem))
				continue
			}
			pass.Reportf(call.Pos(),
				"krt collection element type %s has no key: krt.GetKey will panic at runtime. "+
					"Implement `ResourceName() string` with a value receiver, or embed krt.Named",
				TypeName(elem))
		}
	}
	return nil, nil
}

// ptrOnlyKeyAccessor reports whether *t would be keyable while t is not.
func ptrOnlyKeyAccessor(t types.Type) bool {
	if _, isPtr := types.Unalias(t).(*types.Pointer); isPtr {
		return false
	}
	return KeyOf(types.NewPointer(t)) != KeyNone
}
