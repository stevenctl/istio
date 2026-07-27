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

// FetchAnalyzer reports transformation functions that read state without registering a
// dependency, or that depend on state outside the krt graph.
var FetchAnalyzer = &analysis.Analyzer{
	Name: "krtfetch",
	Doc: `check that krt transformations only read other collections through Fetch

krt tracks which collections a transformation depended on by intercepting krt.Fetch, and
uses that to decide what to recompute. Reading a collection directly with List, GetKey or
an index Lookup returns data without registering the dependency, so the transformation is
never re-run when that data changes and its output silently goes stale. For the same
reason transformations must be deterministic: they may be re-run at any time, and any
result derived from wall-clock time, randomness or the environment cannot be reproduced.`,
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      runFetch,
}

// impureCall names functions whose results a transformation cannot legally depend on,
// keyed by package path then function or method name.
var impureCall = map[string]map[string]string{
	"time": {"Now": "wall-clock time"},
	"math/rand": {
		"Int": "randomness", "Intn": "randomness", "Int31": "randomness", "Int31n": "randomness",
		"Int63": "randomness", "Int63n": "randomness", "Float64": "randomness", "Float32": "randomness",
		"Perm": "randomness", "Shuffle": "randomness",
	},
	"math/rand/v2": {
		"Int": "randomness", "IntN": "randomness", "Int32": "randomness", "Int32N": "randomness",
		"Int64": "randomness", "Int64N": "randomness", "Float64": "randomness", "Float32": "randomness",
		"Perm": "randomness", "Shuffle": "randomness",
	},
	"os": {"Getenv": "the process environment", "LookupEnv": "the process environment"},
}

func runFetch(pass *analysis.Pass) (any, error) {
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	filter := []ast.Node{(*ast.FuncDecl)(nil), (*ast.FuncLit)(nil)}
	insp.Preorder(filter, func(n ast.Node) {
		var body *ast.BlockStmt
		var sig *types.Signature
		switch fn := n.(type) {
		case *ast.FuncDecl:
			body = fn.Body
			if obj, ok := pass.TypesInfo.Defs[fn.Name].(*types.Func); ok {
				sig, _ = obj.Type().(*types.Signature)
			}
		case *ast.FuncLit:
			body = fn.Body
			sig, _ = pass.TypesInfo.TypeOf(fn).(*types.Signature)
		}
		if body == nil || sig == nil || !takesHandlerContext(sig) {
			return
		}
		checkTransformationBody(pass, body)
	})
	return nil, nil
}

func takesHandlerContext(sig *types.Signature) bool {
	params := sig.Params()
	for i := range params.Len() {
		if IsHandlerContext(params.At(i).Type()) {
			return true
		}
	}
	return false
}

func checkTransformationBody(pass *analysis.Pass, body *ast.BlockStmt) {
	ast.Inspect(body, func(n ast.Node) bool {
		// A nested function literal that takes its own HandlerContext is a separate
		// transformation and is visited on its own.
		if lit, ok := n.(*ast.FuncLit); ok {
			if sig, ok := pass.TypesInfo.TypeOf(lit).(*types.Signature); ok && takesHandlerContext(sig) {
				return false
			}
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		// A value that only reaches a log message does not affect the transformation's
		// output, so it needs no dependency.
		if isLoggingCall(pass.TypesInfo, call) {
			return false
		}
		checkUntrackedRead(pass, call)
		checkNondeterminism(pass, call)
		return true
	})
}

// logPkgPath is istio's logging package; both its package-level functions and the methods
// of its Scope type are treated as logging calls.
const logPkgPath = "istio.io/istio/pkg/log"

// isLoggingCall reports whether call emits a log message.
func isLoggingCall(info *types.Info, call *ast.CallExpr) bool {
	var id *ast.Ident
	switch fn := ast.Unparen(call.Fun).(type) {
	case *ast.SelectorExpr:
		id = fn.Sel
	case *ast.Ident:
		id = fn
	default:
		return false
	}
	obj, ok := info.Uses[id].(*types.Func)
	if !ok || obj.Pkg() == nil {
		return false
	}
	return obj.Pkg().Path() == logPkgPath
}

// checkUntrackedRead reports reads of a collection that bypass Fetch and so do not
// register a dependency.
func checkUntrackedRead(pass *analysis.Pass, call *ast.CallExpr) {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return
	}
	recv := pass.TypesInfo.TypeOf(sel.X)
	if recv == nil {
		return
	}
	name := sel.Sel.Name
	switch {
	case IsIndex(recv) && name == "Lookup":
		pass.Reportf(call.Pos(),
			"krt transformation calls Index.Lookup, which does not register a dependency; "+
				"the result will go stale. Use idx.Fetch(ctx, key) or krt.Fetch with krt.FilterIndex")
	case IsSingleton(recv) && name == "Get":
		pass.Reportf(call.Pos(),
			"krt transformation calls Singleton.Get, which does not register a dependency; "+
				"the result will go stale. Use krt.FetchOne(ctx, singleton.AsCollection())")
	case IsCollection(recv):
		switch name {
		case "List":
			pass.Reportf(call.Pos(),
				"krt transformation calls Collection.List, which does not register a dependency; "+
					"the result will go stale. Use krt.Fetch(ctx, collection)")
		case "GetKey":
			pass.Reportf(call.Pos(),
				"krt transformation calls Collection.GetKey, which does not register a dependency; "+
					"the result will go stale. Use krt.FetchOne(ctx, collection, krt.FilterKey(key))")
		case "Register", "RegisterBatch":
			pass.Reportf(call.Pos(),
				"krt transformation registers an event handler; transformations may run many times, "+
					"so this leaks a handler per invocation. Register outside the transformation")
		}
	}
}

// checkNondeterminism reports calls that make a transformation unreproducible.
func checkNondeterminism(pass *analysis.Pass, call *ast.CallExpr) {
	var id *ast.Ident
	switch fn := ast.Unparen(call.Fun).(type) {
	case *ast.SelectorExpr:
		id = fn.Sel
	case *ast.Ident:
		id = fn
	default:
		return
	}
	obj, ok := pass.TypesInfo.Uses[id].(*types.Func)
	if !ok || obj.Pkg() == nil {
		return
	}
	// Only package-level functions; a method named Now on some other type is unrelated.
	if sig, ok := obj.Type().(*types.Signature); ok && sig.Recv() != nil {
		return
	}
	source, ok := impureCall[obj.Pkg().Path()][obj.Name()]
	if !ok {
		return
	}
	pass.Reportf(call.Pos(),
		"krt transformation depends on %s via %s.%s; transformations must be deterministic "+
			"because krt may re-run them at any time with the same inputs",
		source, obj.Pkg().Name(), obj.Name())
}
