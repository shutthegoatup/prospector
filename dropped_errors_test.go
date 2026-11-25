package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// GORM reports a failed query in the Error field of what it returns, which nothing
// makes a caller read. A query written as a statement by itself has dropped it, and a
// database that is down then looks like an empty list.
func TestNoQueryDropsItsError(t *testing.T) {
	ends := map[string]bool{"Find": true, "First": true, "Take": true, "Scan": true, "Count": true, "Pluck": true,
		"Create": true, "CreateInBatches": true, "Save": true, "Update": true, "Updates": true, "UpdateColumn": true,
		"Delete": true, "Exec": true, "AutoMigrate": true}
	files, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	checked := 0
	for _, f := range files {
		name := f.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || strings.HasSuffix(name, "_templ.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		checked++
		ast.Inspect(file, func(n ast.Node) bool {
			stmt, ok := n.(*ast.ExprStmt)
			if !ok {
				return true
			}
			call, ok := stmt.X.(*ast.CallExpr)
			if !ok {
				return true
			}
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && ends[sel.Sel.Name] && onDatabase(sel.X) {
				t.Errorf("%s: .%s(…) is a statement by itself, so its error is dropped: read .Error", fset.Position(stmt.Pos()), sel.Sel.Name)
			}
			return true
		})
	}
	if checked < 20 {
		t.Fatalf("only %d source files were read: is the test running in the package directory?", checked)
	}
}

func TestAFailedQueryIsntTakenForNothingThere(t *testing.T) {
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	co := must(s.AgentAddCompany(o.ID, Actor{Name: "agent:scout"}, AddCompanyIn{Name: "Larkmoor"}))
	if err := s.db.Migrator().DropTable(&Contact{}, &Membership{}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.History(o.ID, co.ID, 10); err == nil {
		t.Error("a company's history came back as if complete when its people couldn't be read")
	}
	if !lastOwner(s.db, o.ID) {
		t.Error("with the owners uncountable, the one being removed was taken not to be the last")
	}
}

// onDatabase follows a chain of calls back to where it starts, and reports whether
// that is a store's database, a scoped query or a transaction.
func onDatabase(x ast.Expr) bool {
	for {
		switch v := x.(type) {
		case *ast.CallExpr:
			x = v.Fun
		case *ast.SelectorExpr:
			if v.Sel.Name == "db" || v.Sel.Name == "in" {
				return true
			}
			x = v.X
		case *ast.Ident:
			return v.Name == "tx" || v.Name == "db" || v.Name == "q" || v.Name == "old"
		default:
			return false
		}
	}
}
