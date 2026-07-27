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
	"strings"

	"golang.org/x/tools/go/analysis"
)

// ignoreMarker opts a field out of the completeness check, for fields that are genuinely
// derived from others or are pure caches.
const ignoreMarker = "+noKrtEquals"

// EqualsFieldsAnalyzer reports Equals implementations that silently ignore a field.
var EqualsFieldsAnalyzer = &analysis.Analyzer{
	Name: "krtequalsfields",
	Doc: `check that a krt Equals method compares every field

krt uses Equals to decide whether an object changed. A field left out of Equals is a field
whose changes are invisible to every downstream collection, which produces stale output
that is very hard to trace back. Fields that are intentionally excluded (caches, derived
values) can be marked with a ` + ignoreMarker + ` comment.

This check only runs on packages that import krt, and it bails out on any Equals method
whose receiver or argument is used as a whole value, since such an implementation cannot
be attributed to individual fields.`,
	Run: runEqualsFields,
}

func runEqualsFields(pass *analysis.Pass) (any, error) {
	if !importsKrt(pass.Pkg) {
		return nil, nil
	}
	ignored := ignoredFields(pass.Files)
	for _, file := range pass.Files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name.Name != "Equals" || fn.Recv == nil || fn.Body == nil {
				continue
			}
			checkEqualsFields(pass, fn, ignored)
		}
	}
	return nil, nil
}

func importsKrt(pkg *types.Package) bool {
	if pkg.Path() == PkgPath {
		return true
	}
	for _, imp := range pkg.Imports() {
		if imp.Path() == PkgPath {
			return true
		}
	}
	return false
}

func checkEqualsFields(pass *analysis.Pass, fn *ast.FuncDecl, ignored map[string]bool) {
	obj, ok := pass.TypesInfo.Defs[fn.Name].(*types.Func)
	if !ok {
		return
	}
	sig, ok := obj.Type().(*types.Signature)
	if !ok || sig.Recv() == nil {
		return
	}
	recvType := sig.Recv().Type()
	if p, ok := types.Unalias(recvType).(*types.Pointer); ok {
		recvType = p.Elem()
	}
	// Only check methods krt will actually dispatch to; a mismatched signature is
	// reported by the krtequal analyzer instead.
	if EqualsMethod(recvType) == nil {
		return
	}
	strct, ok := StructOf(recvType)
	if !ok || strct.NumFields() == 0 {
		return
	}

	vars := equalsOperands(pass.TypesInfo, fn)
	if len(vars) == 0 {
		return
	}
	compared, whole := comparedFields(pass.TypesInfo, fn.Body, vars)
	if whole {
		return
	}

	typeName := TypeName(recvType)
	var missing []string
	for i := range strct.NumFields() {
		f := strct.Field(i)
		if compared[f.Name()] || ignored[declaredName(recvType)+"."+f.Name()] {
			continue
		}
		missing = append(missing, f.Name())
	}
	if len(missing) == 0 {
		return
	}
	pass.Reportf(fn.Pos(),
		"%s.Equals does not compare %s; krt will not see changes to %s. "+
			"Compare the field, or mark it %s if it is derived from another field",
		typeName, strings.Join(missing, ", "), plural(len(missing), "this field", "these fields"), ignoreMarker)
}

// declaredName returns the bare (unqualified) name of a named type, matching how struct
// declarations are keyed in the source.
func declaredName(t types.Type) string {
	if n, ok := types.Unalias(t).(*types.Named); ok {
		return n.Obj().Name()
	}
	return ""
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// equalsOperands returns the receiver and the single parameter of an Equals method.
func equalsOperands(info *types.Info, fn *ast.FuncDecl) map[types.Object]bool {
	out := map[types.Object]bool{}
	add := func(fields *ast.FieldList) {
		if fields == nil {
			return
		}
		for _, f := range fields.List {
			for _, name := range f.Names {
				if name.Name == "_" {
					continue
				}
				if obj, ok := info.Defs[name]; ok && obj != nil {
					out[obj] = true
				}
			}
		}
	}
	add(fn.Recv)
	add(fn.Type.Params)
	// An unnamed receiver or parameter cannot be attributed, and neither can a body that
	// does not name both operands.
	if len(out) != 2 {
		return nil
	}
	return out
}

// comparedFields returns the set of receiver fields read in the body. whole is true when
// an operand is used as a complete value, in which case the body cannot be attributed to
// individual fields and no diagnostic should be produced.
func comparedFields(info *types.Info, body *ast.BlockStmt, vars map[types.Object]bool) (compared map[string]bool, whole bool) {
	compared = map[string]bool{}
	// Idents that appear as the base of a field selection are accounted for; any other
	// use means the operand escaped into something we cannot reason about.
	accountedFor := map[*ast.Ident]bool{}

	ast.Inspect(body, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		base, ok := ast.Unparen(sel.X).(*ast.Ident)
		if !ok || !vars[info.Uses[base]] {
			return true
		}
		selection, ok := info.Selections[sel]
		if !ok || selection.Kind() != types.FieldVal {
			// A method call on the operand; the operand is fully consumed by it.
			return true
		}
		accountedFor[base] = true
		compared[sel.Sel.Name] = true
		return true
	})

	ast.Inspect(body, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok || !vars[info.Uses[id]] || accountedFor[id] {
			return true
		}
		whole = true
		return false
	})
	return compared, whole
}

// ignoredFields collects `Type.Field` keys for fields carrying the opt-out marker.
func ignoredFields(files []*ast.File) map[string]bool {
	out := map[string]bool{}
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			spec, ok := n.(*ast.TypeSpec)
			if !ok {
				return true
			}
			strct, ok := spec.Type.(*ast.StructType)
			if !ok || strct.Fields == nil {
				return true
			}
			for _, field := range strct.Fields.List {
				if !hasIgnoreMarker(field) {
					continue
				}
				for _, name := range field.Names {
					out[spec.Name.Name+"."+name.Name] = true
				}
			}
			return true
		})
	}
	return out
}

func hasIgnoreMarker(field *ast.Field) bool {
	for _, group := range []*ast.CommentGroup{field.Doc, field.Comment} {
		if group != nil && strings.Contains(group.Text(), ignoreMarker) {
			return true
		}
	}
	return false
}
