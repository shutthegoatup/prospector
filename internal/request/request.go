package request

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/shutthegoatup/prospector/internal/identity"
	"github.com/shutthegoatup/prospector/internal/store"
)

// Req is who is asking and in which organisation: a signed-in member, or an API key.
type Req struct {
	User    *store.User
	Session *store.Session
	Org     *store.Org
	Member  *store.Membership
	Orgs    []identity.OrgMembership
	Key     *store.APIKey
	// due and convs are worked out once a request, for the page and the navigation both.
	DueByStep         map[string][]store.Contact
	OpenConversations map[string][]store.Contact
}

type ReqKey struct{}

// Actor is the name recorded against what this request does. A person is their full
// name, so two with the same first name stay distinct in the history, or their email
// when the name is missing or would read as an agent, the CLI or an import.
func (q *Req) Actor() string {
	if q.Key != nil {
		return "agent:" + q.Key.Name
	}
	name := strings.TrimSpace(q.User.Name)
	if name == "" || strings.HasPrefix(name, "agent:") || name == "cli" || name == "import" || name == store.CustomerActor {
		return q.User.Email
	}
	return name
}

// Who is the request's actor as the history records it, with the key behind an agent.
func (q *Req) Who() store.Actor {
	who := store.Actor{Name: q.Actor()}
	if q.Key != nil {
		who.KeyID = q.Key.ID
	}
	return who
}

func (q *Req) Agent() store.Actor {
	return store.Actor{Name: q.Actor(), KeyID: q.Key.ID, ReadOnly: !q.Key.Has("research:write")}
}

func (q *Req) Voice() Voice {
	caller := q.Member.CallerName
	if caller == "" {
		caller = q.User.Name
	}
	return Voice{Caller: caller, Org: q.Org.Name, Pitch: q.Org.Settings.WithDefaults().Pitch, Signature: q.Member.Signature}
}

func (q *Req) Owner() bool { return q.Member != nil && q.Member.Role == "owner" }

func (q *Req) Base() string { return "/o/" + q.Org.Slug + "/" }

// Voice is who is calling and signing, from the org's settings and the member's own.
type Voice struct {
	Caller, Org, Pitch, Signature string
	// Pitches are the offerings' own pitch lines, by offering, where one was written.
	Pitches map[int64]string
}

func (v Voice) PitchFor(offering int64) string {
	if p := v.Pitches[offering]; p != "" {
		return p
	}
	return v.Pitch
}

func (v Voice) CallerFirst() string {
	if f := store.FirstField(v.Caller); f != "" {
		return f
	}
	return "[YOUR NAME]"
}

func (v Voice) CallerFull() string {
	if v.Caller != "" {
		return v.Caller
	}
	return "[YOUR NAME]"
}

func (v Voice) Signed() string {
	if v.Signature == "" {
		return ""
	}
	return "\n\n" + v.Signature
}

func ReqFrom(ctx context.Context) *Req { return ctx.Value(ReqKey{}).(*Req) }

type ListURLKey struct{}

// RequestKey holds the request an operation came in, for the few that need it.
type RequestKey struct{}

func RequestFrom(ctx context.Context) *http.Request { return ctx.Value(RequestKey{}).(*http.Request) }

// LanguageFrom is the language to answer the operation in ctx in.
func LanguageFrom(ctx context.Context) string { return LanguageOf(RequestFrom(ctx)) }

// NextLink is the Link header for the page after this one: empty when this one is the last.
func NextLink(ctx context.Context, next string) string {
	at, _ := ctx.Value(ListURLKey{}).(*url.URL)
	if next == "" || at == nil {
		return ""
	}
	q := at.Query()
	q.Set("cursor", next)
	return "<" + at.Path + "?" + q.Encode() + `>; rel="next"`
}

func Put(to, v *string) {
	if v != nil {
		*to = *v
	}
}

// Worker is who reads or works a task through the API: a key as the agent it is, or a
// person signed in, who claims nothing by reading.
func Worker(q *Req) store.Actor {
	if q.Key != nil {
		return q.Agent()
	}
	return store.Actor{Name: q.Actor(), ReadOnly: true}
}
