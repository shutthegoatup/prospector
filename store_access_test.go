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
// A query made anywhere else, or through an unexported method, is never tried.
func TestOnlyTheStoreTouchesTheDatabase(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	var parsed []*ast.File
	// The store's unexported methods, and every other type's, by name: the check below
	// goes by name alone, so a name both use would fail the other type's calls.
	private, others := map[string]bool{"db": true, "in": true}, map[string]bool{}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		parsed = append(parsed, f)
		for _, decl := range f.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv != nil {
				if !onStore(fn) {
					others[fn.Name.Name] = true
				} else if !fn.Name.IsExported() {
					private[fn.Name.Name] = true
				}
			}
		}
	}
	for name := range private {
		if others[name] {
			t.Errorf("Store.%s shares its name with another type's method, whose calls this test can't tell from the store's: rename one", name)
		}
	}
	for _, f := range parsed {
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			// Open makes the store, so it migrates it.
			if !ok || fn.Body == nil || onStore(fn) || fn.Recv == nil && fn.Name.Name == "Open" {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if sel, ok := n.(*ast.SelectorExpr); ok && private[sel.Sel.Name] {
					t.Errorf("%s: %s reaches the database past the store's exported methods (.%s): give the store one", fset.Position(sel.Pos()), fn.Name.Name, sel.Sel.Name)
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

// TestTheStoreKeepsFilesOfItsOwn keeps an area's queries apart from what the app and
// the API do with them, so the store can be split by area and moved out of main.
func TestTheStoreKeepsFilesOfItsOwn(t *testing.T) {
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
		var store, other string
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil {
				continue
			}
			if onStore(fn) {
				store = fn.Name.Name
			} else if r := receiver(fn); r == "App" || r == "apiServer" {
				other = r + "." + fn.Name.Name
			}
		}
		if store != "" && other != "" {
			t.Errorf("%s holds both Store.%s and %s: keep the store's methods in a file of their own, such as <area>_store.go", name, store, other)
		}
	}
}

func onStore(fn *ast.FuncDecl) bool { return receiver(fn) == "Store" }

func receiver(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) != 1 {
		return ""
	}
	typ := fn.Recv.List[0].Type
	if star, ok := typ.(*ast.StarExpr); ok {
		typ = star.X
	}
	if id, ok := typ.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}
