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
)

const (
	// ignoreMarker permanently excludes a field, for values genuinely derived from other
	// compared fields. To exempt a whole Equals method, use the //krtlint:ignore directive.
	ignoreMarker = "+noKrtEquals"
	// todoMarker excludes a field that is known to be missing but not yet fixed. These are
	// reported with -krtequalsfields.todos.
	todoMarker = "+krtEqualsTodo"
)

// reportTodos re-enables diagnostics for fields marked with todoMarker.
var reportTodos bool

// EqualsFieldsAnalyzer reports Equals implementations that silently ignore a field.
var EqualsFieldsAnalyzer = &analysis.Analyzer{
	Name: "krtequalsfields",
	Doc: `check that a krt Equals method compares every field, on both sides

krt uses Equals to decide whether an object changed, and keeps the old object when Equals
returns true. A field left out of Equals therefore does not merely miss an event: it stays
permanently stale in the collection until some compared field happens to change. Fields
that are genuinely derived from a compared field can be marked ` + ignoreMarker + `, and
known gaps can be marked ` + todoMarker + `.

This also reports fields read from only one side of the comparison, which is the shape a
copy-paste error takes: the field is compared against itself and can never differ.

Fields that ResourceName reads are exempt automatically: they are part of the key, so a
change to one produces a delete and an add rather than an update, and Equals is never asked
about it.

The check only runs on packages that import krt, and it backs off on any Equals method whose
receiver or argument is used as a whole value, since it cannot then be attributed to
individual fields.`,
	Run: runEqualsFields,
}

func init() {
	EqualsFieldsAnalyzer.Flags.BoolVar(&reportTodos, "todos", false,
		"also report fields marked "+todoMarker)
}

func runEqualsFields(pass *analysis.Pass) (any, error) {
	if !importsKrt(pass.Pkg) {
		return nil, nil
	}
	exempt := exemptFields(pass.Files)
	namers := methodDecls(pass, "ResourceName")
	for _, file := range pass.Files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name.Name != "Equals" || fn.Recv == nil || fn.Body == nil {
				continue
			}
			checkEqualsFields(pass, fn, exempt, namers)
		}
	}
	return nil, nil
}

// methodDecls indexes the declarations of a named method by the type it is declared on.
func methodDecls(pass *analysis.Pass, name string) map[types.Object]*ast.FuncDecl {
	out := map[types.Object]*ast.FuncDecl{}
	for _, file := range pass.Files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name.Name != name || fn.Recv == nil || fn.Body == nil {
				continue
			}
			obj, ok := pass.TypesInfo.Defs[fn.Name].(*types.Func)
			if !ok {
				continue
			}
			sig, ok := obj.Type().(*types.Signature)
			if !ok || sig.Recv() == nil {
				continue
			}
			if named := namedOf(sig.Recv().Type()); named != nil {
				out[named.Obj()] = fn
			}
		}
	}
	return out
}

// namedOf returns the named type underlying t, dereferencing a single pointer.
func namedOf(t types.Type) *types.Named {
	base := types.Unalias(t)
	if p, ok := base.(*types.Pointer); ok {
		base = types.Unalias(p.Elem())
	}
	n, _ := base.(*types.Named)
	return n
}

// keyFields returns the fields that a type's ResourceName method reads.
//
// A field the key is built from cannot go stale. Changing it changes the key, and krt
// delivers that as a delete of the old object and an add of the new one, never as an update
// that Equals could suppress. Leaving such a field out of Equals is correct by construction,
// so reporting it is noise.
//
// This applies only when ResourceName is in fact how krt keys the type: a Kubernetes object
// or a config.Config is keyed by its metadata, and any ResourceName it also happens to
// declare is not consulted.
func keyFields(info *types.Info, recvType types.Type, decl *ast.FuncDecl) map[string]bool {
	if decl == nil || KeyOf(recvType) != KeyResourceNamer {
		return nil
	}
	recv := onlyNamed(info, decl.Recv)
	if recv == nil {
		return nil
	}
	read, whole := fieldsRead(info, decl.Body, recv)
	if whole {
		// The receiver escaped into something we cannot see through, so we cannot tell
		// which fields reach the key. Assume none do.
		return nil
	}
	return read[recv]
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

func checkEqualsFields(pass *analysis.Pass, fn *ast.FuncDecl, exempt map[string]bool, namers map[types.Object]*ast.FuncDecl) {
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
	// Only check methods krt will actually dispatch to; a mismatched signature is reported
	// by the krtequal analyzer instead.
	if EqualsMethod(recvType) == nil {
		return
	}
	strct, ok := StructOf(recvType)
	if !ok || strct.NumFields() == 0 {
		return
	}

	recv, param := equalsOperands(pass.TypesInfo, fn)
	if recv == nil || param == nil {
		return
	}
	read, whole := fieldsRead(pass.TypesInfo, fn.Body, recv, param)
	if whole {
		return
	}

	var keyed map[string]bool
	if named := namedOf(recvType); named != nil {
		keyed = keyFields(pass.TypesInfo, recvType, namers[named.Obj()])
	}

	typeName := TypeName(recvType)
	var missing, lopsided []string
	for i := range strct.NumFields() {
		name := strct.Field(i).Name()
		if exempt[declaredName(recvType)+"."+name] || keyed[name] {
			continue
		}
		inRecv, inParam := read[recv][name], read[param][name]
		switch {
		case !inRecv && !inParam:
			missing = append(missing, name)
		case inRecv != inParam:
			lopsided = append(lopsided, name)
		}
	}

	if len(missing) > 0 {
		pass.Reportf(fn.Pos(),
			"%s.Equals does not compare %s; krt keeps the old object when Equals returns true, "+
				"so %s will stay stale. Compare the field, or mark it %s if it is derived from another field",
			typeName, strings.Join(missing, ", "),
			plural(len(missing), "this field", "these fields"), ignoreMarker)
	}
	for _, d := range selfLookups(pass.TypesInfo, fn.Body, recv, param) {
		pass.Reportf(d.pos,
			"%s.Equals indexes %s.%s inside a loop over that same field, and never indexes the "+
				"other operand's %s; this comparison can never fail",
			typeName, d.operand, d.field, d.field)
	}
	if len(lopsided) > 0 {
		pass.Reportf(fn.Pos(),
			"%s.Equals reads %s from only one operand (%s); the field is compared against "+
				"itself and can never differ",
			typeName, strings.Join(lopsided, ", "),
			plural(len(lopsided), "this field", "these fields"))
	}
}

// declaredName returns the bare name of a named type, matching how struct declarations are
// keyed in the source.
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

// equalsOperands returns the receiver and the parameter of an Equals method. Either is nil
// when it is unnamed, in which case the body cannot be attributed.
func equalsOperands(info *types.Info, fn *ast.FuncDecl) (recv, param types.Object) {
	return onlyNamed(info, fn.Recv), onlyNamed(info, fn.Type.Params)
}

// onlyNamed returns the object declared by a field list holding exactly one named entry,
// which is the shape of a receiver or a single parameter. It returns nil for a blank or
// omitted name, which cannot be attributed to anything in the body.
func onlyNamed(info *types.Info, fields *ast.FieldList) types.Object {
	if fields == nil || len(fields.List) != 1 || len(fields.List[0].Names) != 1 {
		return nil
	}
	name := fields.List[0].Names[0]
	if name.Name == "_" {
		return nil
	}
	return info.Defs[name]
}

// fieldsRead returns, per operand, the set of top-level struct fields read in the body.
// whole is true when an operand is used as a complete value, in which case the body cannot
// be attributed to individual fields.
func fieldsRead(info *types.Info, body *ast.BlockStmt, operands ...types.Object) (map[types.Object]map[string]bool, bool) {
	isOperand := map[types.Object]bool{}
	read := map[types.Object]map[string]bool{}
	for _, o := range operands {
		isOperand[o] = true
		read[o] = map[string]bool{}
	}
	// Idents appearing as the base of a field selection are accounted for; any other use
	// means the operand escaped into something we cannot reason about.
	accountedFor := map[*ast.Ident]bool{}

	ast.Inspect(body, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		base, ok := ast.Unparen(sel.X).(*ast.Ident)
		if !ok {
			return true
		}
		operand := info.Uses[base]
		if !isOperand[operand] {
			return true
		}
		selection, ok := info.Selections[sel]
		if !ok || selection.Kind() != types.FieldVal {
			// A method call on the operand, which consumes it whole.
			return true
		}
		// A selection through an embedded field has a multi-step index path. Attribute it
		// to the outermost field, which is the one declared on this struct: comparing
		// `a.Labels` where Labels is promoted does compare the embedded field.
		field := selection.Obj()
		if path := selection.Index(); len(path) > 1 {
			if s, ok := StructOf(operand.Type()); ok {
				field = s.Field(path[0])
			}
		}
		accountedFor[base] = true
		read[operand][field.Name()] = true
		return true
	})

	whole := false
	ast.Inspect(body, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok || !isOperand[info.Uses[id]] || accountedFor[id] {
			return true
		}
		whole = true
		return false
	})
	return read, whole
}

type selfLookup struct {
	pos     token.Pos
	operand string
	field   string
}

// selfLookups finds `for k := range a.F { ... a.F[k] ... }` inside an Equals body where the
// other operand's F is never indexed. Ranging over one operand's collection and then
// indexing that same one, rather than its counterpart, is a copy-paste error: the lookup
// can only ever return the value being ranged over.
//
// The common correct form, `for i := range a.F { a.F[i] == b.F[i] }`, indexes both and is
// left alone.
func selfLookups(info *types.Info, body *ast.BlockStmt, recv, param types.Object) []selfLookup {
	// operandField returns the operand and field name for a selector rooted at recv or param.
	operandField := func(e ast.Expr) (types.Object, string, bool) {
		sel, ok := ast.Unparen(e).(*ast.SelectorExpr)
		if !ok {
			return nil, "", false
		}
		base, ok := ast.Unparen(sel.X).(*ast.Ident)
		if !ok {
			return nil, "", false
		}
		obj := info.Uses[base]
		if obj != recv && obj != param {
			return nil, "", false
		}
		if s, ok := info.Selections[sel]; !ok || s.Kind() != types.FieldVal {
			return nil, "", false
		}
		return obj, sel.Sel.Name, true
	}

	var out []selfLookup
	ast.Inspect(body, func(n ast.Node) bool {
		rng, ok := n.(*ast.RangeStmt)
		if !ok {
			return true
		}
		ranged, field, ok := operandField(rng.X)
		if !ok {
			return true
		}
		counterpart := param
		if ranged == param {
			counterpart = recv
		}
		// Collect every indexing of this field within the loop, by either operand.
		var selfIndexed []token.Pos
		counterpartIndexed := false
		ast.Inspect(rng.Body, func(n ast.Node) bool {
			idx, ok := n.(*ast.IndexExpr)
			if !ok {
				return true
			}
			obj, name, ok := operandField(idx.X)
			if !ok || name != field {
				return true
			}
			if obj == counterpart {
				counterpartIndexed = true
			} else {
				selfIndexed = append(selfIndexed, idx.Pos())
			}
			return true
		})
		if counterpartIndexed {
			return true
		}
		for _, pos := range selfIndexed {
			out = append(out, selfLookup{pos: pos, operand: ranged.Name(), field: field})
		}
		return true
	})
	return out
}

// exemptFields collects `Type.Field` keys for fields carrying an opt-out marker.
func exemptFields(files []*ast.File) map[string]bool {
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
				if !isExempt(field) {
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

func isExempt(field *ast.Field) bool {
	for _, group := range []*ast.CommentGroup{field.Doc, field.Comment} {
		if group == nil {
			continue
		}
		text := group.Text()
		if strings.Contains(text, ignoreMarker) {
			return true
		}
		if !reportTodos && strings.Contains(text, todoMarker) {
			return true
		}
	}
	return false
}
