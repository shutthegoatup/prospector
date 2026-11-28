package app

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
	// The core's handle on the database, its scoping to an organisation and its history
	// log are exported from internal/store, but are the store's alone to use.
	private, others := map[string]bool{"DB": true}, map[string]bool{}
	for name := range withinTheStore {
		private[name] = true
	}
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
			// Open and connect make the store, so they reach its database.
			if !ok || fn.Body == nil || onStore(fn) || fn.Recv == nil && (fn.Name.Name == "Open" || fn.Name.Name == "connect") {
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

// TestOnlyStoresTouchTheDatabaseInsideInternal is the rule above for the packages under
// internal/, where a product's handlers live beside its store: there only the methods of
// a …Store type, and the functions their transactions are handed to, may reach the core.
func TestOnlyStoresTouchTheDatabaseInsideInternal(t *testing.T) {
	t.Parallel()
	// internal/app, this package, has the stricter rule above.
	files, err := filepath.Glob("../*/*.go")
	if err != nil {
		t.Fatal(err)
	}
	reach := map[string]bool{"DB": true}
	for name := range withinTheStore {
		reach[name] = true
	}
	fset := token.NewFileSet()
	for _, name := range files {
		pkg := filepath.Base(filepath.Dir(name))
		if pkg == "app" {
			continue
		}
		// The core itself, and the test fixtures that make databases.
		if strings.HasSuffix(name, "_test.go") || pkg == "store" || pkg == "dbtest" {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || strings.HasSuffix(receiver(fn), "Store") || handedATransaction(fn) {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if sel, ok := n.(*ast.SelectorExpr); ok && reach[sel.Sel.Name] {
					t.Errorf("%s: %s reaches the database (.%s) outside a store: give the store a method", fset.Position(sel.Pos()), fn.Name.Name, sel.Sel.Name)
				}
				return true
			})
		}
	}
}

// handedATransaction is a function a store method calls with its transaction.
func handedATransaction(fn *ast.FuncDecl) bool {
	for _, p := range fn.Type.Params.List {
		if star, ok := p.Type.(*ast.StarExpr); ok {
			if sel, ok := star.X.(*ast.SelectorExpr); ok && sel.Sel.Name == "DB" {
				return true
			}
		}
	}
	return false
}

// withinTheStore are store methods that take or give back a handle on the database, for
// the store's own code: the core's helpers the areas' queries build on, and one area's
// query that another's calls inside its own transaction now the two are in different
// packages. TestOnlyTheStoreTouchesTheDatabase keeps them out of everything else.
var withinTheStore = map[string]bool{
	"InOrg": true, "Log": true, "LogAs": true,
	"SetDeal":             true, // prospecting writes a contact's deal through it
	"SetContactField":     true, // accepting a suggestion writes the contact in its own transaction
	"SuppressIn":          true, // a bounce, and an agent's address, suppress within their own work
	"Gate":                true, // outreach and research ask suppression who may be mailed
	"Marketable":          true,
	"MintUnsubscribeLink": true, // a campaign's mail carries a link made in its own transaction
	// Research holds back a company it finds, and suggests details, inside its own work.
	"HeldWhenFound": true, "HeldWhenReached": true, "SuggestDetails": true,
	// The search's query text, which agents' searches build on.
	"CompanyMatch": true, "ContactMatch": true,
}

// TestNoStoreMethodHandsOutTheDatabase keeps the rule above from being got round: a
// method that took or gave back a handle on the database would let its caller query.
func TestNoStoreMethodHandsOutTheDatabase(t *testing.T) {
	t.Parallel()
	handle := reflect.TypeOf(&gorm.DB{})
	store := reflect.TypeOf(&Store{})
	for i := range store.NumMethod() {
		m := store.Method(i)
		if withinTheStore[m.Name] {
			continue
		}
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

// storeTypes are Store and the types it embeds: the core and each area's queries.
var storeTypes = func() map[string]bool {
	out := map[string]bool{"Store": true}
	typ := reflect.TypeOf(Store{})
	for i := range typ.NumField() {
		if f := typ.Field(i); f.Anonymous {
			t := f.Type
			if t.Kind() == reflect.Pointer {
				t = t.Elem()
			}
			out[t.Name()] = true
		}
	}
	return out
}()

func onStore(fn *ast.FuncDecl) bool { return storeTypes[receiver(fn)] }

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
