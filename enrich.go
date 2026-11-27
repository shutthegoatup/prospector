package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"gorm.io/gorm"
)

// Enricher is a data vendor an organisation pays, under its own key, to find a detail
// its people and agents couldn't. Vendors are asked in Position order, each no more
// than MonthlyLimit times a calendar month.
type Enricher struct {
	ID    int64  `gorm:"primaryKey"`
	OrgID int64  `gorm:"uniqueIndex:idx_enrichers_org_kind;not null"`
	Kind  string `gorm:"uniqueIndex:idx_enrichers_org_kind;not null"`
	// APIKey is sealed under the server's key, never kept or shown in clear.
	APIKey       string `gorm:"not null;default:''"`
	Position     int    `gorm:"not null;default:0"`
	MonthlyLimit int    `gorm:"not null;default:0"`
	UpdatedBy    string `gorm:"not null;default:''"`
	UpdatedAt    time.Time
}

// EnrichUsage counts the lookups asked of one vendor in one month, so the limit holds
// across servers and restarts.
type EnrichUsage struct {
	ID    int64  `gorm:"primaryKey"`
	OrgID int64  `gorm:"uniqueIndex:idx_enrich_usage;not null"`
	Kind  string `gorm:"uniqueIndex:idx_enrich_usage;not null"`
	Month string `gorm:"uniqueIndex:idx_enrich_usage;not null"`
	Calls int    `gorm:"not null;default:0"`
}

// enrichPerson is what a vendor is told about someone: no more than it needs to find them.
type enrichPerson struct {
	Name, Domain, Company, Linkedin string
}

// vendorFind is what a vendor found for one field; an empty Value is a miss.
type vendorFind struct {
	Value       string
	EmailStatus string
}

type vendor interface {
	Name() string
	Fields() []string
	Find(ctx context.Context, c *http.Client, base, key string, p enrichPerson, field string) (vendorFind, error)
}

var vendors = map[string]vendor{}

func vendorKinds() []string {
	var out []string
	for k := range vendors {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

const (
	enrichAgain     = 30 * 24 * time.Hour
	enrichLookupFor = 20 * time.Second
	maxMonthlyLimit = 100_000
)

type EnricherIn struct {
	Kind         string
	APIKey       string
	Position     int
	MonthlyLimit int
}

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

// unlookable says why nothing about someone may be sent to a vendor, or "" if it may:
// they declined, or they or their company's domain is on the never-contact list.
func (set OrgSettings) unlookable(c Contact, domain string) string {
	if c.Declined != nil {
		return "they declined: nothing about them is looked up"
	}
	if err := set.listed(deref(c.Email), deref(c.Linkedin)); err != nil {
		return err.Error()
	}
	if domain != "" {
		if err := set.listed("someone@"+domain, ""); err != nil {
			return err.Error()
		}
	}
	return ""
}

// Enrich asks the organisation's vendors, in its order, for the details someone lacks.
// What is found waits as a suggestion for a person to accept, like any agent's find.
func (a *App) Enrich(ctx context.Context, o *Org, who Actor, in EnrichIn) ([]EnrichAsked, error) {
	s := a.s
	if in.Task != 0 {
		var err error
		if who, err = s.OnTask(o.ID, who, in.Task); err != nil {
			return nil, err
		}
	}
	if len(in.Fields) == 0 {
		return nil, errors.New("fields is needed: which of email, phone and linkedin to look for")
	}
	found, err := s.ContactByID(o.ID, in.ContactID)
	if err != nil {
		return nil, err
	}
	c := *found
	if strings.HasPrefix(c.Key, "org:") {
		return nil, errors.New("that is a company's own row, not a person")
	}
	co := &Company{}
	if c.CompanyID != nil {
		if co, err = s.CompanyByID(o.ID, *c.CompanyID); err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}
	domain := strings.ToLower(strings.TrimSpace(co.Domain))
	if why := o.Settings.unlookable(c, domain); why != "" {
		return nil, conflict(why)
	}
	person := enrichPerson{Name: displayName(c), Domain: domain, Company: co.Name, Linkedin: deref(c.Linkedin)}
	enrichers, err := s.Enrichers(o.ID)
	if err != nil {
		return nil, err
	}
	var out []EnrichAsked
	for _, field := range in.Fields {
		field = strings.ToLower(strings.TrimSpace(field))
		if !slices.Contains(suggestable, field) {
			return nil, fmt.Errorf("unknown field %q: email, phone or linkedin", field)
		}
		if detailOf(c, field) != "" {
			out = append(out, EnrichAsked{Field: field, Outcome: "has"})
			continue
		}
		out = append(out, a.enrichField(ctx, o, who, c, person, enrichers, field))
	}
	return out, nil
}

func (a *App) enrichField(ctx context.Context, o *Org, who Actor, c Contact, p enrichPerson, enrichers []Enricher, field string) EnrichAsked {
	s := a.s
	asked := EnrichAsked{Field: field, Outcome: "skipped", Detail: "no vendor here looks for " + field}
	tried := false
	for _, e := range enrichers {
		v := vendors[e.Kind]
		if v == nil || !slices.Contains(v.Fields(), field) || e.APIKey == "" {
			continue
		}
		if when := s.AskedLately(o.ID, c.ID, field, e.Kind); when != nil {
			if !tried {
				asked.Detail = v.Name() + " was asked on " + when.Format("2 Jan 2006") + "; it is asked again after " + fmt.Sprint(enrichAgain.Hours()/24) + " days"
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
		key, err := s.keys.open(e.APIKey)
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
			a.logEnrich(o.ID, who, c, field, e.Kind+": "+lookupNone)
			continue
		}
		kept := a.keepFind(o, who, c, field, e.Kind, value, found.EmailStatus)
		if kept.Outcome == "skipped" {
			// Asking again would only spend another lookup on the same answer.
			a.logEnrich(o.ID, who, c, field, e.Kind+": "+lookupNotKept)
		}
		return kept
	}
	if tried && asked.Outcome == "skipped" {
		asked.Outcome = "none"
	}
	return asked
}

// keepFind stores what a vendor found as a suggestion, unless it may not be kept.
func (a *App) keepFind(o *Org, who Actor, c Contact, field, kind, value, status string) EnrichAsked {
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
	if field == "email" && s.TakesMail(context.Background(), mailDomainOf(value)) == mailRefused {
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

const (
	lookupFound   = "found"
	lookupNone    = "none found"
	lookupNotKept = "found, not kept"
)

// Lookup is a vendor's latest answer about one detail of one person.
type Lookup struct {
	Field, Vendor, Outcome string
	At                     time.Time
}

func (a *App) logEnrich(org int64, who Actor, c Contact, field, value string) {
	noted(a.s.LogEnrich(org, who, c.ID, field, value))
}

func (a *App) vendorHTTP() *http.Client {
	if a.vendorClient != nil {
		return a.vendorClient
	}
	return publicClient(false)
}

// EnrichOut is what came of each field asked for.
type EnrichOut struct {
	Results []EnrichAsked `json:"results"`
}
