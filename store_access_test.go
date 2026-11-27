package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gorm.io/gorm"
)

// TestOnlyTheStoreTouchesTheDatabase keeps every query on an exported method of the
// store, where TestNoMethodReachesIntoAnotherOrganisation tries it as another organisation.
// A query made anywhere else is never tried.
func TestOnlyTheStoreTouchesTheDatabase(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || onStore(fn) {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "db" {
					t.Errorf("%s: %s reaches the database itself: give the store a method for it", fset.Position(sel.Pos()), fn.Name.Name)
				}
				return true
			})
		}
	}
}

// TestNoStoreMethodHandsOutTheDatabase keeps the rule above from being got round: a
// method that took or gave back a handle on the database would let its caller query.
func TestNoStoreMethodHandsOutTheDatabase(t *testing.T) {
	t.Parallel()
	handle := reflect.TypeOf(&gorm.DB{})
	store := reflect.TypeOf(&Store{})
	for i := range store.NumMethod() {
		m := store.Method(i)
		for j := range m.Type.NumIn() {
			if m.Type.In(j) == handle {
				t.Errorf("Store.%s takes a *gorm.DB", m.Name)
			}
		}
		for j := range m.Type.NumOut() {
			if m.Type.Out(j) == handle {
				t.Errorf("Store.%s gives back a *gorm.DB", m.Name)
			}
		}
	}
}

func onStore(fn *ast.FuncDecl) bool {
	if fn.Recv == nil || len(fn.Recv.List) != 1 {
		return false
	}
	typ := fn.Recv.List[0].Type
	if star, ok := typ.(*ast.StarExpr); ok {
		typ = star.X
	}
	id, ok := typ.(*ast.Ident)
	return ok && id.Name == "Store"
}
