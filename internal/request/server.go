package request

import (
	"strings"

	"github.com/shutthegoatup/prospector/internal/identity"
	"github.com/shutthegoatup/prospector/internal/onboarding"
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
	if name == "" || strings.HasPrefix(name, "agent:") || name == "cli" || name == "import" || name == onboarding.CustomerActor {
		return q.User.Email
	}
	return name
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
