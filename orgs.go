package main

import (
	"crypto/rand"
	"encoding/base64"
	"regexp"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/shutthegoatup/prospector/internal/say"
)

const sessionLife = 30 * 24 * time.Hour

var (
	slugRe    = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)
	notSlug   = regexp.MustCompile(`[^a-z0-9]+`)
	colorRe   = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	orgColors = []string{"#56c5f4", "#f5c66b", "#7fe0a8", "#ff9a9a", "#c4a7ff"}
)

func randomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func slugify(name string) string {
	s := strings.Trim(notSlug.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(s) > 40 {
		s = strings.Trim(s[:40], "-")
	}
	return s
}

var errWrongCurrent = say.New("the current password is wrong")

// frontOrgSlug is the organisation the front page's contact form sends enquiries to.
const frontOrgSlug = "prospector"

func createOrg(tx *gorm.DB, slug, name string) (*Org, error) {
	if slug == "" {
		slug = slugify(name)
	}
	if !slugRe.MatchString(slug) {
		return nil, say.Errorf("%q can't be used in a URL: use a-z, 0-9 and -", slug)
	}
	inUse := conflict{say.Errorf("another organisation already has the address /o/%s: choose a different name", slug)}
	var same, n int64
	if err := tx.Model(&Org{}).Where("slug = ?", slug).Count(&same).Error; err != nil {
		return nil, err
	}
	if same > 0 {
		return nil, inUse
	}
	if err := tx.Model(&Org{}).Count(&n).Error; err != nil {
		return nil, err
	}
	o := &Org{Slug: slug, Name: strings.TrimSpace(name), Color: orgColors[int(n)%len(orgColors)]}
	// Two asking for the same address at once both find it free; the index stops the
	// second, and the slug is the only thing about a new organisation it guards.
	if err := tx.Create(o).Error; breaksRule(err) {
		return nil, inUse
	} else if err != nil {
		return nil, err
	}
	return o, nil
}

var errLastOwner = say.New("an organisation needs an owner: make someone else an owner first")

func setRole(tx *gorm.DB, org int64, m *Membership, role string) error {
	if m.Role == "owner" && role != "owner" && lastOwner(tx, org) {
		return errLastOwner
	}
	return tx.Model(m).Update("role", role).Error
}

// lockOrg makes changes to one organisation's members take turns, so two owners
// removing each other at the same moment can't both count the other as remaining.
func lockOrg(tx *gorm.DB, org int64) error {
	return tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&Org{}, org).Error
}

func lastOwner(tx *gorm.DB, org int64) bool {
	var owners int64
	// A count that failed is taken as one owner, so nobody is removed on the strength of it.
	if logged(tx.Model(&Membership{}).Where("org_id = ? and role = ?", org, "owner").Count(&owners)) != nil {
		return true
	}
	return owners <= 1
}

type OrgMembership struct {
	Org
	Role string
}

type Member struct {
	Membership
	Email, Name string
}

var errWrongLogin = say.New("wrong email or password")

// Scopes an API key can carry. None can change members or keys or reach another org; only enquiries:reply, customers:invite and campaigns:send send mail, and only import skips review.
var Scopes = []struct{ Name, What string }{
	{"read", "search companies and people, read what's due and the tasks people wrote, read deals, subscriptions, onboardings and enquiries with what was said in answering them, and read the organisation's settings, its never-contact list and agent guidance included, and what it sells with where each company stands for each"},
	{"research:write", "add companies, facts and suggested contacts (they wait in Research), and leave notes on tasks"},
	{"drafts:write", "write draft notes and emails, marked as written by the agent"},
	{"outcomes:write", "record what happened (added, accepted, sent, called, replied, declined, hold), shown as the agent's"},
	{"import", "push the repo's markdown and pull the snapshot"},
	{"enquiries:write", "record an enquiry or sign-up from your own product or site backend"},
	{"enquiries:reply", "email a reply to whoever sent an enquiry, through your mail server, and record what they said back"},
	{"customers:write", "record and change subscriptions, start their renewals, and start, change and remove onboardings"},
	{"pipeline:write", "work deals (start, value, stage, owner, remove) and say where answering an enquiry stands, who has it, and put its sender on the board"},
	{"enquiries:triage", "say where answering an enquiry stands, who has it, and put its sender on the board, without touching deals"},
	{"enquiries:delete", "delete an enquiry for good, with what was said in answering it"},
	{"campaigns:write", "write campaigns, see who they would go to, and keep the list of addresses marketing mail never goes to"},
	{"campaigns:send", "send a campaign to everyone in conversation or at a customer, through your own mail server, and cancel or resume one"},
	{"agreements:write", "start agreements for companies, upload their documents, and set out who signs them and where; nothing is sent"},
	{"customers:invite", "email a customer a link to their onboarding steps, through your mail server, and withdraw it"},
	{"contacts:write", "add people straight to outreach and change their details and where their conversation stands"},
	{"research:review", "qualify and reject companies, for the organisation or for one thing it sells, pick suggested people for outreach and accept or dismiss details agents found: the review a person otherwise does"},
	{"people:privacy", "export everything held on a person, and erase them for good on their request"},
	{"enrich:spend", "ask the organisation's enrichment vendors, at its cost and within each one's monthly limit, for an email, phone or profile someone lacks; what is found waits to be accepted"},
	{"tasks:write", "start tasks, ask them for more, mark their requests done or not, close and reopen them, and delete the notes agents left on them"},
	{"agents:run", "start and stop Prospector's own agents on tasks"},
	{"agents:configure", "add, change and delete Prospector's own agents, their endpoints, keys and tools"},
	{"settings:write", "change the organisation's settings: its name and colour, waits and wording, the guidance sent to every agent, never-contact list, what it looks for, enquiry sites, onboarding steps, currency, postal address and whether to email only incorporated companies"},
	{"team:write", "add people to the team, change their roles and areas, and take them out: as strong as an owner, since it can make one"},
	{"mail:configure", "set the organisation's outgoing mail server, its username and password"},
	{"sso:configure", "set up single sign-on: the provider, its client secret, the email domains it speaks for and whether it is required"},
	{"org:export", "download everything the organisation holds in one file"},
}

func validScope(s string) bool {
	for _, sc := range Scopes {
		if sc.Name == s {
			return true
		}
	}
	return false
}

func checkScopes(scopes []string) error {
	for _, sc := range scopes {
		if !validScope(sc) {
			return say.Errorf("unknown scope %q", sc)
		}
	}
	if len(scopes) == 0 {
		return say.New("pick at least one scope")
	}
	return nil
}

var errBuiltinKey = say.New("this key belongs to one of Prospector's own agents, which change on the agent itself")
