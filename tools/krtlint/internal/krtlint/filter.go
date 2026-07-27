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

// FilterAnalyzer reports Fetch filters applied to types that cannot satisfy them.
var FilterAnalyzer = &analysis.Analyzer{
	Name: "krtfilter",
	Doc: `check that krt Fetch filters are applied to types that support them

Label and selector filters extract fields from the fetched object by type assertion and
reflection. As the krt README notes, a type that does not provide the required accessor
causes a panic rather than a compile error, and only on the code path where a matching
object actually reaches the filter.`,
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      runFilter,
}

// fetchFuncs are the krt functions that accept FetchOptions alongside a collection.
var fetchFuncs = map[string]bool{
	"Fetch":                  true,
	"FetchOne":               true,
	"FetchOrList":            true,
	"PartialFetch":           true,
	"PartialFetchComparable": true,
}

// filterRequirement describes what a filter needs from the objects it inspects.
type filterRequirement struct {
	accessor string
	explain  string
}

var filterRequirements = map[string]filterRequirement{
	"FilterLabel": {
		accessor: "labels",
		explain:  "implement `GetLabels() map[string]string` with a value receiver, or use a Kubernetes object type",
	},
	"FilterSelects": {
		accessor: "selector",
		explain:  "implement `GetLabelSelector() map[string]string` with a value receiver, or expose a `Spec.Selector` field",
	},
	"FilterSelectsNonEmpty": {
		accessor: "selector",
		explain:  "implement `GetLabelSelector() map[string]string` with a value receiver, or expose a `Spec.Selector` field",
	},
}

func runFilter(pass *analysis.Pass) (any, error) {
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	for call := range insp.PreorderSeq((*ast.CallExpr)(nil)) {
		call := call.(*ast.CallExpr)
		elem, args, ok := fetchTarget(pass.TypesInfo, call)
		if !ok {
			continue
		}
		for _, arg := range args {
			opt, ok := ast.Unparen(arg).(*ast.CallExpr)
			if !ok {
				continue
			}
			fn := CalleeFrom(pass.TypesInfo, opt, PkgPath)
			if fn == nil {
				continue
			}
			req, ok := filterRequirements[fn.Name()]
			if !ok {
				continue
			}
			if hasAccessor(elem, req.accessor) {
				continue
			}
			pass.Reportf(opt.Pos(),
				"krt.%s is applied to a collection of %s, which exposes no %s; "+
					"krt panics when a candidate object reaches this filter. To fix, %s",
				fn.Name(), TypeName(elem), req.accessor, req.explain)
		}
	}
	return nil, nil
}

// fetchTarget returns the element type being fetched and the FetchOption arguments.
func fetchTarget(info *types.Info, call *ast.CallExpr) (types.Type, []ast.Expr, bool) {
	// Index.Fetch(ctx, key, opts...) is a method, handled separately from the package
	// level Fetch functions.
	if sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr); ok && sel.Sel.Name == "Fetch" {
		recv := info.TypeOf(sel.X)
		if n, ok := NamedFrom(recv, PkgPath, "Index"); ok {
			if args := n.TypeArgs(); args != nil && args.Len() == 2 && len(call.Args) > 2 {
				return args.At(1), call.Args[2:], true
			}
			return nil, nil, false
		}
	}
	fn := CalleeFrom(info, call, PkgPath)
	if fn == nil || !fetchFuncs[fn.Name()] {
		return nil, nil, false
	}
	// The collection is always the second parameter; options follow the declared ones.
	sig, ok := fn.Type().(*types.Signature)
	if !ok || !sig.Variadic() || len(call.Args) < 2 {
		return nil, nil, false
	}
	elem, ok := CollectionElem(info.TypeOf(call.Args[1]))
	if !ok {
		return nil, nil, false
	}
	fixed := sig.Params().Len() - 1
	if len(call.Args) <= fixed {
		return nil, nil, false
	}
	return elem, call.Args[fixed:], true
}

// hasAccessor mirrors krt's getLabels and getLabelSelector, both of which assert against
// the boxed value and therefore see only the value method set.
func hasAccessor(t types.Type, kind string) bool {
	switch kind {
	case "labels":
		// metav1.Object also provides GetLabels, so this covers Kubernetes types.
		return returnsStringMapMethod(t, "GetLabels") || isConfigConfig(t)
	case "selector":
		if returnsStringMapMethod(t, "GetLabelSelector") {
			return true
		}
		return hasSpecSelector(t)
	}
	return true
}

// hasSpecSelector mirrors the reflection fallback in krt's getLabelSelector, which reads
// the Selector field of the Spec field.
func hasSpecSelector(t types.Type) bool {
	s, ok := StructOf(t)
	if !ok {
		return false
	}
	spec, ok := fieldByName(s, "Spec")
	if !ok {
		return false
	}
	specStruct, ok := StructOf(spec.Type())
	if !ok {
		return false
	}
	_, ok = fieldByName(specStruct, "Selector")
	return ok
}

func fieldByName(s *types.Struct, name string) (*types.Var, bool) {
	for i := range s.NumFields() {
		if s.Field(i).Name() == name {
			return s.Field(i), true
		}
	}
	return nil, false
}
