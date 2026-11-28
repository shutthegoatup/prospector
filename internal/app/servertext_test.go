package app

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/shutthegoatup/prospector/internal/request"
	"github.com/shutthegoatup/prospector/internal/say"
)

// wordsFor are the calls whose format a person reads, and which of their arguments it is.
var wordsFor = map[string]int{"say.New": 0, "say.Errorf": 0, "noSuch": 0, "missing": 1, "store.NoSuch": 0, "store.Missing": 1, "NoSuch": 0, "Missing": 1}

// serverFormats are the formats the server's code hands those calls, as written: string
// literals, or constants declared alongside.
func serverFormats(t *testing.T) []string {
	t.Helper()
	fset := token.NewFileSet()
	var files []*ast.File
	err := filepath.WalkDir("../..", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (strings.HasPrefix(d.Name(), ".") || d.Name() == "web" || d.Name() == "node_modules") && p != "../.." {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			return err
		}
		files = append(files, f)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	consts := map[string]string{}
	for _, f := range files {
		for _, d := range f.Decls {
			if g, ok := d.(*ast.GenDecl); ok && g.Tok == token.CONST {
				for _, s := range g.Specs {
					v := s.(*ast.ValueSpec)
					for i, name := range v.Names {
						if i < len(v.Values) {
							if lit, ok := v.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
								consts[name.Name], _ = strconv.Unquote(lit.Value)
							}
						}
					}
				}
			}
		}
	}
	found := map[string]bool{}
	for _, f := range files {
		for _, d := range f.Decls {
			fn, _ := d.(*ast.FuncDecl)
			ast.Inspect(d, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				name := ""
				switch fn := call.Fun.(type) {
				case *ast.Ident:
					name = fn.Name
				case *ast.SelectorExpr:
					if x, ok := fn.X.(*ast.Ident); ok {
						name = x.Name + "." + fn.Sel.Name
					}
				}
				i, ok := wordsFor[name]
				if !ok || len(call.Args) <= i {
					return true
				}
				if s, ok := constant(call.Args[i], consts); ok {
					found[s] = true
					return true
				}
				// The wrappers pass on a format they were handed, which is gathered where
				// they are called; say.New also passes on words that aren't the server's.
				wrapper := false
				if fn != nil {
					_, wrapper = wordsFor[fn.Name.Name]
				}
				if name != "say.New" && !wrapper {
					t.Errorf("%s: %s's format isn't written out, so it can't be translated: write it whole, with verbs for what varies",
						fset.Position(call.Pos()), name)
				}
				return true
			})
		}
	}
	return slices.Sorted(maps.Keys(found))
}

// constant is the string e always is: a literal, a constant, or those joined with +.
func constant(e ast.Expr, consts map[string]string) (string, bool) {
	switch e := e.(type) {
	case *ast.BasicLit:
		if e.Kind == token.STRING {
			s, err := strconv.Unquote(e.Value)
			return s, err == nil
		}
	case *ast.Ident:
		s, ok := consts[e.Name]
		return s, ok
	case *ast.ParenExpr:
		return constant(e.X, consts)
	case *ast.BinaryExpr:
		if e.Op == token.ADD {
			x, ok := constant(e.X, consts)
			y, ok2 := constant(e.Y, consts)
			return x + y, ok && ok2
		}
	}
	return "", false
}

func verbs(format string) []string {
	var v []string
	for _, p := range say.Split(format) {
		if strings.HasPrefix(p, "%") && p != "%%" {
			v = append(v, p)
		}
	}
	slices.Sort(v)
	return v
}

// TestTheServersWordsAreCatalogued gathers what the server says into the source's
// catalogue, internal/say/lang/en-GB.json, which translators work from: run with
// PROSPECTOR_WRITE_LANG=1 to write it after changing what the server says. A
// translation may leave a format out, which then reads in English, but has none the
// source lacks, and fills the same verbs.
func TestTheServersWordsAreCatalogued(t *testing.T) {
	if say.Source != request.Locales.Source {
		t.Fatalf("the server is written in %s and the app in %s", say.Source, request.Locales.Source)
	}
	formats := serverFormats(t)
	source := say.Catalogue(say.Source)
	path := filepath.Join("../..", "internal", "say", "lang", say.Source+".json")
	if os.Getenv("PROSPECTOR_WRITE_LANG") != "" {
		c := map[string]say.Message{}
		for _, f := range formats {
			m, ok := source[f]
			if !ok {
				m = say.Message{Text: f}
			}
			c[f] = m
		}
		var b bytes.Buffer
		enc := json.NewEncoder(&b)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		if err := enc.Encode(c); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	for _, f := range formats {
		if _, ok := source[f]; !ok {
			t.Errorf("%s lacks %q: run PROSPECTOR_WRITE_LANG=1 go test -run TestTheServersWordsAreCatalogued .", path, f)
		}
	}
	for f, m := range source {
		if !slices.Contains(formats, f) {
			t.Errorf("%s has %q, which the server no longer says: run PROSPECTOR_WRITE_LANG=1 go test -run TestTheServersWordsAreCatalogued .", path, f)
		}
		if m.Forms != nil && m.Forms["other"] == "" {
			t.Errorf("%s: %q has plural forms but no \"other\"", path, f)
		}
	}
	for _, lang := range say.Languages() {
		if !slices.Contains(request.Locales.Translated, lang) && lang != say.Source {
			t.Errorf("internal/say/lang/%s.json is for a language web/locales.json doesn't list as translated", lang)
		}
		for f, m := range say.Catalogue(lang) {
			if _, ok := source[f]; !ok {
				t.Errorf("%s: %q isn't something the server says", lang, f)
				continue
			}
			texts := []string{m.Text}
			if m.Forms != nil {
				texts = slices.Collect(maps.Values(m.Forms))
			}
			for _, text := range texts {
				if !slices.Equal(verbs(text), verbs(f)) {
					t.Errorf("%s: %q fills %v, not %v", lang, text, verbs(text), verbs(f))
				}
			}
		}
	}
}
