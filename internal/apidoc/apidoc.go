package apidoc

import (
	"maps"
	"net/http"
	"slices"
	"sort"
	"strings"

	"github.com/shutthegoatup/prospector/internal/cleantext"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/shutthegoatup/prospector/internal/identity"
	"github.com/shutthegoatup/prospector/internal/store"
)

// The page at /api is the API's contract, drawn from api/openapi.yaml as people read
// it: public, as the spec itself is, and with no script (see apidoc_view.go).

type Doc struct {
	Title, Version string
	Intro          []string
	Base           string
	Scopes         []Scope
	Groups         []Group
	Schemas        []Schema
	Problems       []Problem
}

// Problem is a problem type a client acts on, with its entry on the page.
type Problem struct{ Type, Name, About string }

func ProblemAnchor(name string) string { return "problem-" + name }

type Scope struct {
	Name, What string
	Owner      bool
}

type Group struct {
	Name, About string
	Ops         []Op
}

type Op struct {
	ID, Method, Path, Summary, About, Scope string
	OwnerOnly                               bool
	Params                                  []Field
	Body                                    *Schema
	Responses                               []APIResponse
}

type APIResponse struct{ Code, About, Type string }

type Schema struct {
	Name, About string
	Fields      []Field
	Enum        []string
	// Of is what an inline or array body is made of, when it has no fields of its own.
	Of string
}

type Field struct {
	Name, Type, About string
	Required, Write   bool
}

func Of(spec *openapi3.T) Doc {
	doc := Doc{Title: spec.Info.Title, Version: spec.Info.Version}
	for p := range strings.SplitSeq(strings.TrimSpace(spec.Info.Description), "\n\n") {
		doc.Intro = append(doc.Intro, cleantext.OneLine(p))
	}
	for _, s := range identity.Scopes {
		doc.Scopes = append(doc.Scopes, Scope{Name: s.Name, What: s.What.Error(), Owner: identity.OwnersOnly(s.Name)})
	}
	groups := map[string]*Group{}
	var order []string
	for _, t := range spec.Tags {
		groups[t.Name] = &Group{Name: t.Name, About: t.Description}
		order = append(order, t.Name)
	}
	paths := spec.Paths.InMatchingOrder()
	slices.Sort(paths)
	methods := []string{"GET", "POST", "PUT", "PATCH", "DELETE"}
	for _, path := range paths {
		item := spec.Paths.Value(path)
		for _, method := range methods {
			op := item.GetOperation(method)
			if op == nil {
				continue
			}
			o := Op{ID: op.OperationID, Method: method, Path: path, Summary: op.Summary, About: cleantext.OneLine(op.Description)}
			s := OpScopes(op)
			o.Scope, o.OwnerOnly = strings.Join(s, " or "), slices.ContainsFunc(s, identity.OwnersOnly)
			for _, p := range append(slices.Clone(item.Parameters), op.Parameters...) {
				if p.Value.Name == "org" {
					continue
				}
				o.Params = append(o.Params, Field{Name: p.Value.Name, Type: typeOf(p.Value.Schema), Required: p.Value.Required, About: cleantext.OneLine(p.Value.Description)})
			}
			if op.RequestBody != nil {
				if mt := op.RequestBody.Value.Content.Get("application/json"); mt != nil {
					body := schemaOf("", mt.Schema)
					o.Body = &body
				}
			}
			codes := slices.Sorted(maps.Keys(op.Responses.Map()))
			for _, code := range codes {
				res := op.Responses.Value(code).Value
				r := APIResponse{Code: code, About: cleantext.OneLine(store.Deref(res.Description))}
				for _, mt := range res.Content {
					r.Type = typeOf(mt.Schema)
				}
				o.Responses = append(o.Responses, r)
			}
			tag := "other"
			if len(op.Tags) > 0 {
				tag = op.Tags[0]
			}
			if groups[tag] == nil {
				groups[tag] = &Group{Name: tag}
				order = append(order, tag)
			}
			groups[tag].Ops = append(groups[tag].Ops, o)
		}
	}
	for _, name := range order {
		doc.Groups = append(doc.Groups, *groups[name])
	}
	names := slices.Sorted(maps.Keys(spec.Components.Schemas))
	for _, name := range names {
		doc.Schemas = append(doc.Schemas, schemaOf(name, spec.Components.Schemas[name]))
	}
	doc.Problems = ProblemsOf(spec)
	return doc
}

const problemPrefix = "/api/problems/"

func ProblemsOf(spec *openapi3.T) []Problem {
	kinds := spec.Components.Schemas["ProblemType"].Value
	about, _ := kinds.Extensions["x-enum-descriptions"].([]any)
	var out []Problem
	for i, e := range kinds.Enum {
		t, _ := e.(string)
		p := Problem{Type: t, Name: strings.TrimPrefix(t, problemPrefix)}
		if i < len(about) {
			p.About, _ = about[i].(string)
		}
		out = append(out, p)
	}
	return out
}

func schemaOf(name string, ref *openapi3.SchemaRef) Schema {
	out := Schema{Name: name}
	if ref == nil || ref.Value == nil {
		return out
	}
	// A body that is a named schema shows that schema's fields, under its name.
	if name == "" && ref.Ref != "" {
		out.Of = typeOf(ref)
	}
	s := ref.Value
	out.About = cleantext.OneLine(s.Description)
	for _, e := range s.Enum {
		if v, ok := e.(string); ok {
			out.Enum = append(out.Enum, v)
		}
	}
	if len(s.Properties) == 0 && len(out.Enum) == 0 {
		if out.Of == "" {
			out.Of = typeOf(ref)
		}
		return out
	}
	props := make([]string, 0, len(s.Properties))
	for p := range s.Properties {
		props = append(props, p)
	}
	// Required fields first, then the rest, each by name.
	sort.Slice(props, func(i, j int) bool {
		ri, rj := slices.Contains(s.Required, props[i]), slices.Contains(s.Required, props[j])
		if ri != rj {
			return ri
		}
		return props[i] < props[j]
	})
	for _, p := range props {
		prop := s.Properties[p]
		about := ""
		if prop.Value != nil {
			about = cleantext.OneLine(prop.Value.Description)
		}
		out.Fields = append(out.Fields, Field{Name: p, Type: typeOf(prop), Required: slices.Contains(s.Required, p), About: about,
			Write: prop.Value != nil && prop.Value.WriteOnly})
	}
	return out
}

// typeOf names a schema as the page shows it: a component by name, arrays with [],
// and a nullable value with a trailing ?.
func typeOf(ref *openapi3.SchemaRef) string {
	if ref == nil {
		return ""
	}
	if ref.Ref != "" {
		return ref.Ref[strings.LastIndex(ref.Ref, "/")+1:]
	}
	s := ref.Value
	if s == nil {
		return ""
	}
	var t string
	switch {
	case s.Type.Is("array"):
		t = typeOf(s.Items) + "[]"
	case s.Type.Is("object") && len(s.Properties) > 0:
		t = "object"
	case s.Format != "" && s.Format != "int64":
		t = s.Format
	case s.Type != nil && len(*s.Type) > 0:
		t = (*s.Type)[0]
	}
	if s.Nullable {
		t += "?"
	}
	return t
}

// codeSpans splits text on backticks, so `quoted` parts can be set as code.
type textSpan struct {
	Text string
	Code bool
}

func codeSpans(s string) []textSpan {
	var out []textSpan
	for i, part := range strings.Split(s, "`") {
		if part != "" {
			out = append(out, textSpan{Text: part, Code: i%2 == 1})
		}
	}
	return out
}

func OpAnchor(o Op) string { return "op-" + o.ID }

func schemaAnchor(name string) string { return "schema-" + name }

// schemaLink is the anchor for a type that names a component, or "" for a plain type.
func schemaLink(doc Doc, t string) string {
	name := strings.TrimSuffix(strings.TrimSuffix(t, "?"), "[]")
	for _, s := range doc.Schemas {
		if s.Name == name {
			return "#" + schemaAnchor(name)
		}
	}
	return ""
}

func methodTone(m string) string {
	switch m {
	case "GET":
		return "is-green"
	case "POST":
		return "is-gold"
	case "PUT", "PATCH":
		return "is-violet"
	}
	return "is-coral"
}

// curlOf is the operation as a shell command, for the reader's own organisation.
func curlOf(base string, o Op) string {
	path := strings.ReplaceAll(o.Path, "{org}", "your-org")
	var b strings.Builder
	b.WriteString("curl")
	if o.Method != http.MethodGet {
		b.WriteString(" -X " + o.Method)
	}
	b.WriteString(" " + strings.TrimRight(base, "/") + path + " \\\n  -H \"Authorization: Bearer $PROSPECTOR_KEY\"")
	if o.Body != nil {
		b.WriteString(" \\\n  -H \"Content-Type: application/json\" \\\n  -d '" + exampleOf(o.Body) + "'")
	}
	return b.String()
}

// exampleOf is a body with each required field, and the first few others, filled in
// with a placeholder of its type.
func exampleOf(s *Schema) string {
	var parts []string
	for i, f := range s.Fields {
		if !f.Required && i > 3 {
			continue
		}
		v := `"…"`
		switch {
		case strings.HasSuffix(f.Type, "[]"):
			v = "[]"
		case strings.TrimSuffix(f.Type, "?") == "integer":
			v = "1"
		case strings.TrimSuffix(f.Type, "?") == "boolean":
			v = "true"
		}
		parts = append(parts, `"`+f.Name+`": `+v)
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

// OpScopes are the scopes a key may make an operation with, any one of them.
func OpScopes(op *openapi3.Operation) []string {
	var out []string
	if op.Security != nil {
		for _, req := range *op.Security {
			if s := req["bearer"]; len(s) > 0 {
				out = append(out, s[0])
			}
		}
	}
	return out
}
