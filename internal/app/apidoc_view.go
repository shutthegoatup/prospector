package app

import "strings"

// The /api reference is drawn by the React site (web/src/site) when the app is built,
// from this view of the spec: web/src/site/apidoc.json, which a test keeps in step with
// api/openapi.yaml. Everything derived is worked out here, so the page only lays it out.

// docBase stands in for the server's own address in the built page; the server puts
// its address in its place as it answers.
const docBase = "https://prospector.invalid"

type docView struct {
	Title   string      `json:"title"`
	Version string      `json:"version"`
	Scopes  []docScope  `json:"scopes"`
	Groups  []docGroup  `json:"groups"`
	Schemas []docSchema `json:"schemas"`
	// Problems are the problem types, each with the anchor its /api/problems/ link
	// lands on.
	Problems []docProblem `json:"problems"`
}

type docProblem struct {
	Anchor string    `json:"anchor"`
	Type   string    `json:"type"`
	About  []docSpan `json:"about"`
}

type docSpan struct {
	Text string `json:"text"`
	Code bool   `json:"code,omitempty"`
}

type docScope struct {
	Name  string `json:"name"`
	What  string `json:"what"`
	Owner bool   `json:"owner,omitempty"`
}

type docGroup struct {
	Name  string    `json:"name"`
	About []docSpan `json:"about"`
	Ops   []docOp   `json:"ops"`
}

type docOp struct {
	Anchor    string        `json:"anchor"`
	Method    string        `json:"method"`
	Tone      string        `json:"tone"`
	Path      string        `json:"path"`
	Summary   string        `json:"summary"`
	About     []docSpan     `json:"about"`
	Scope     string        `json:"scope,omitempty"`
	OwnerOnly bool          `json:"owner_only,omitempty"`
	Params    []docField    `json:"params"`
	Body      *docBody      `json:"body,omitempty"`
	Responses []docResponse `json:"responses"`
	Curl      string        `json:"curl"`
}

// docType is a type's name, with the anchor of its schema when it names one.
type docType struct {
	Name string `json:"name"`
	Link string `json:"link,omitempty"`
}

type docField struct {
	Name     string    `json:"name"`
	Type     docType   `json:"type"`
	About    []docSpan `json:"about"`
	Required bool      `json:"required,omitempty"`
	Write    bool      `json:"write,omitempty"`
}

type docBody struct {
	Of     *docType   `json:"of,omitempty"`
	Fields []docField `json:"fields"`
}

type docResponse struct {
	Code  string   `json:"code"`
	Bad   bool     `json:"bad,omitempty"`
	About string   `json:"about"`
	Type  *docType `json:"type,omitempty"`
}

type docSchema struct {
	Anchor string     `json:"anchor"`
	Name   string     `json:"name"`
	About  []docSpan  `json:"about"`
	Enum   []string   `json:"enum"`
	Fields []docField `json:"fields"`
}

func docViewOf(doc APIDoc) docView {
	spans := func(s string) []docSpan {
		out := []docSpan{}
		for _, sp := range codeSpans(s) {
			out = append(out, docSpan(sp))
		}
		return out
	}
	typ := func(t string) docType { return docType{t, schemaLink(doc, t)} }
	fields := func(in []APIField) []docField {
		out := []docField{}
		for _, f := range in {
			out = append(out, docField{f.Name, typ(f.Type), spans(f.About), f.Required, f.Write})
		}
		return out
	}
	v := docView{Title: doc.Title, Version: doc.Version, Scopes: []docScope{}, Groups: []docGroup{}, Schemas: []docSchema{}}
	for _, s := range doc.Scopes {
		v.Scopes = append(v.Scopes, docScope(s))
	}
	for _, g := range doc.Groups {
		dg := docGroup{Name: g.Name, About: spans(g.About), Ops: []docOp{}}
		for _, o := range g.Ops {
			op := docOp{Anchor: opAnchor(o), Method: o.Method, Tone: methodTone(o.Method), Path: o.Path, Summary: o.Summary, About: spans(o.About),
				Scope: o.Scope, OwnerOnly: o.OwnerOnly, Params: fields(o.Params), Responses: []docResponse{}, Curl: curlOf(docBase, o)}
			if o.Body != nil {
				op.Body = &docBody{Fields: fields(o.Body.Fields)}
				if o.Body.Of != "" {
					of := typ(o.Body.Of)
					op.Body.Of = &of
				}
			}
			for _, r := range o.Responses {
				dr := docResponse{Code: r.Code, Bad: r.Code >= "400", About: r.About}
				if r.Type != "" {
					t := typ(r.Type)
					dr.Type = &t
				}
				op.Responses = append(op.Responses, dr)
			}
			dg.Ops = append(dg.Ops, op)
		}
		v.Groups = append(v.Groups, dg)
	}
	for _, s := range doc.Schemas {
		v.Schemas = append(v.Schemas, docSchema{schemaAnchor(s.Name), s.Name, spans(s.About), append([]string{}, s.Enum...), fields(s.Fields)})
	}
	v.Problems = []docProblem{}
	for _, p := range doc.Problems {
		v.Problems = append(v.Problems, docProblem{problemAnchor(p.Name), p.Type, spans(p.About)})
	}
	return v
}

// withBase puts the server's address into a built page in docBase's place.
func withBase(page []byte, base string) []byte {
	return []byte(strings.ReplaceAll(string(page), docBase, htmlEscaper.Replace(strings.TrimRight(base, "/"))))
}

var htmlEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&#34;", "'", "&#39;")
