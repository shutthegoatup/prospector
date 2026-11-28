package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/shutthegoatup/prospector/internal/egress"
	"github.com/shutthegoatup/prospector/internal/marketing"
	"github.com/shutthegoatup/prospector/internal/prospecting"
	"github.com/shutthegoatup/prospector/internal/research"
	"github.com/shutthegoatup/prospector/internal/say"
	"github.com/shutthegoatup/prospector/internal/store"
)

const (
	enrichLookupFor = 20 * time.Second
)

// EnrichAsked is what came of asking for one field.
type EnrichAsked struct {
	Field   string `json:"field"`
	Vendor  string `json:"vendor,omitempty"`
	Outcome string `json:"outcome" jsonschema:"suggested, none (no vendor found it), has (they have one already), skipped or failed"`
	Detail  string `json:"detail,omitempty"`
}

type EnrichIn struct {
	ContactID int64    `json:"contact_id"`
	Fields    []string `json:"fields" jsonschema:"which of email, phone and linkedin to look for"`
	Task      int64    `json:"task,omitempty" jsonschema:"the id of the task this is for, if any"`
}

// Enrich asks the organisation's vendors, in its order, for the details someone lacks.
// What is found waits as a suggestion for a person to accept, like any agent's find.
func (a *App) Enrich(ctx context.Context, o *store.Org, who store.Actor, in EnrichIn) ([]EnrichAsked, error) {
	s := a.s
	if in.Task != 0 {
		var err error
		if who, err = s.OnTask(o.ID, who, in.Task); err != nil {
			return nil, err
		}
	}
	if len(in.Fields) == 0 {
		return nil, say.New("fields is needed: which of email, phone and linkedin to look for")
	}
	found, err := s.ContactByID(o.ID, in.ContactID)
	if err != nil {
		return nil, err
	}
	c := *found
	if strings.HasPrefix(c.Key, "org:") {
		return nil, say.New("that is a company's own row, not a person")
	}
	co := &store.Company{}
	if c.CompanyID != nil {
		if co, err = s.CompanyByID(o.ID, *c.CompanyID); err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}
	domain := strings.ToLower(strings.TrimSpace(co.Domain))
	if why := o.Settings.Unlookable(c, domain); why != "" {
		return nil, store.Conflict{Text: say.New(why)}
	}
	person := prospecting.EnrichPerson{Name: store.DisplayName(c), Domain: domain, Company: co.Name, Linkedin: store.Deref(c.Linkedin)}
	enrichers, err := s.Enrichers(o.ID)
	if err != nil {
		return nil, err
	}
	var out []EnrichAsked
	for _, field := range in.Fields {
		field = strings.ToLower(strings.TrimSpace(field))
		if !slices.Contains(prospecting.Suggestable, field) {
			return nil, say.Errorf("unknown field %q: email, phone or linkedin", field)
		}
		if prospecting.DetailOf(c, field) != "" {
			out = append(out, EnrichAsked{Field: field, Outcome: "has"})
			continue
		}
		out = append(out, a.enrichField(ctx, o, who, c, person, enrichers, field))
	}
	return out, nil
}

func (a *App) enrichField(ctx context.Context, o *store.Org, who store.Actor, c store.Contact, p prospecting.EnrichPerson, enrichers []store.Enricher, field string) EnrichAsked {
	s := a.s
	asked := EnrichAsked{Field: field, Outcome: "skipped", Detail: "no vendor here looks for " + field}
	tried := false
	for _, e := range enrichers {
		v := prospecting.Vendors[e.Kind]
		if v == nil || !slices.Contains(v.Fields(), field) || e.APIKey == "" {
			continue
		}
		if when := s.AskedLately(o.ID, c.ID, field, e.Kind); when != nil {
			if !tried {
				asked.Detail = v.Name() + " was asked on " + when.Format("2 Jan 2006") + "; it is asked again after " + fmt.Sprint(prospecting.EnrichAgain.Hours()/24) + " days"
			}
			continue
		}
		if !tried {
			asked.Detail = "every vendor that looks for " + field + " has used this month's lookups"
		}
		took, err := s.Spend(o.ID, e, time.Now())
		if err != nil {
			return EnrichAsked{Field: field, Vendor: e.Kind, Outcome: "failed", Detail: err.Error()}
		}
		if !took {
			continue
		}
		tried = true
		key, err := s.Keyring.Open(e.APIKey)
		if err != nil {
			asked = EnrichAsked{Field: field, Vendor: e.Kind, Outcome: "failed", Detail: err.Error()}
			continue
		}
		looking, cancel := context.WithTimeout(ctx, enrichLookupFor)
		found, err := v.Find(looking, a.vendorHTTP(), a.vendorBase, key, p, field)
		cancel()
		if err != nil {
			asked = EnrichAsked{Field: field, Vendor: e.Kind, Outcome: "failed", Detail: v.Name() + ": " + err.Error()}
			continue
		}
		value := strings.TrimSpace(found.Value)
		if value == "" {
			asked = EnrichAsked{Field: field, Vendor: e.Kind, Outcome: "none"}
			a.logEnrich(o.ID, who, c, field, e.Kind+": "+research.LookupNone)
			continue
		}
		kept := a.keepFind(o, who, c, field, e.Kind, value, found.EmailStatus)
		if kept.Outcome == "skipped" {
			// Asking again would only spend another lookup on the same answer.
			a.logEnrich(o.ID, who, c, field, e.Kind+": "+research.LookupNotKept)
		}
		return kept
	}
	if tried && asked.Outcome == "skipped" {
		asked.Outcome = "none"
	}
	return asked
}

// keepFind stores what a vendor found as a suggestion, unless it may not be kept.
func (a *App) keepFind(o *store.Org, who store.Actor, c store.Contact, field, kind, value, status string) EnrichAsked {
	s := a.s
	asked := EnrichAsked{Field: field, Vendor: kind, Outcome: "suggested"}
	email, link := "", ""
	switch field {
	case "email":
		email = value
	case "linkedin":
		link = value
	}
	if err := s.Allowed(o, email, link); err != nil {
		return EnrichAsked{Field: field, Vendor: kind, Outcome: "skipped", Detail: err.Error()}
	}
	if err := s.Erased(o.ID, email, link); err != nil {
		return EnrichAsked{Field: field, Vendor: kind, Outcome: "skipped", Detail: "it belongs to someone erased on request"}
	}
	if field == "phone" && phoneLooksWrong(value) {
		return EnrichAsked{Field: field, Vendor: kind, Outcome: "skipped", Detail: "it isn't a number that can be called"}
	}
	if field == "email" && s.TakesMail(context.Background(), store.AddressDomain(value)) == marketing.MailRefused {
		return EnrichAsked{Field: field, Vendor: kind, Outcome: "skipped", Detail: "its domain takes no mail"}
	}
	made, err := s.KeepFind(o.ID, who, c.ID, field, kind, value, status)
	if err != nil {
		return EnrichAsked{Field: field, Vendor: kind, Outcome: "failed", Detail: err.Error()}
	}
	if !made {
		asked.Outcome, asked.Detail = "has", "it was already suggested, or they have it"
	}
	return asked
}

func (a *App) logEnrich(org int64, who store.Actor, c store.Contact, field, value string) {
	noted(a.s.LogEnrich(org, who, c.ID, field, value))
}

func (a *App) vendorHTTP() *http.Client {
	if a.vendorClient != nil {
		return a.vendorClient
	}
	return egress.Client(egress.Options{})
}

// EnrichOut is what came of each field asked for.
type EnrichOut struct {
	Results []EnrichAsked `json:"results"`
}
