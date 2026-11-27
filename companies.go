package main

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/shutthegoatup/prospector/internal/say"
)

var (
	httpURL = regexp.MustCompile(`^https?://\S+$`)
)

// ensureCompany finds a company by name (case-insensitive) or creates it.
func ensureCompany(tx *gorm.DB, org int64, c Company) (*Company, bool, error) {
	c.Name = strings.TrimSpace(c.Name)
	if c.Name == "" {
		return nil, false, say.New("company name is required")
	}
	if err := holdOrg(tx, org); err != nil {
		return nil, false, err
	}
	var found Company
	err := tx.Where("org_id = ? and lower(name) = ?", org, strings.ToLower(c.Name)).First(&found).Error
	if err == nil {
		return &found, false, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, err
	}
	c.OrgID = org
	if c.Stage == "" {
		c.Stage = CompanyReview
	}
	res := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&c)
	if res.Error != nil {
		return nil, false, res.Error
	}
	if res.RowsAffected == 0 {
		// The index found one the look-up didn't: the two fold capitals differently outside ASCII.
		err := tx.Where("org_id = ? and (name = ? or lower(name) = lower(?))", org, c.Name, c.Name).First(&found).Error
		return &found, false, err
	}
	return &c, true, nil
}

// companyAt finds the company an agent means by its website when the name is new,
// so one company found under two spellings stays one row. A site that several
// companies share (a group's brands, a marketplace) identifies none of them.
func companyAt(tx *gorm.DB, org int64, domain string) (*Company, error) {
	if domain == "" {
		return nil, nil
	}
	var found []Company
	if err := tx.Where("org_id = ? and lower(domain) = ?", org, domain).Limit(2).Find(&found).Error; err != nil || len(found) != 1 {
		return nil, err
	}
	return &found[0], nil
}

type HistoryItem struct {
	At     time.Time
	Actor  string
	Action string
	Value  string
	Person string
}

const historyPageSize = 50

// ResearchRow is one company in the Research tab with what's known about it.
type ResearchRow struct {
	Company
	Facts      []Fact
	People     []Contact
	Pending    map[int64][]PendingDetail
	Lookups    map[int64][]Lookup
	Incomplete bool
}

var ResearchFilters = []struct{ Key, Label string }{
	{"review", "To review"}, {"contact", "Needs a contact"}, {"verify", "Needs verifying"}, {"details", "Details to check"}, {"rejected", "Rejected"},
}

// N counts from 1.
type Page struct{ N, Size, Total int }

// pageOf clamps n, so a page that emptied since it was linked shows the last one instead.
func pageOf(n, size, total int) Page {
	p := Page{N: n, Size: size, Total: total}
	p.N = max(1, min(p.N, p.Pages()))
	return p
}

func (p Page) Pages() int  { return max(1, (p.Total+p.Size-1)/p.Size) }
func (p Page) offset() int { return (p.N - 1) * p.Size }

const researchPageSize = 30

// searchFitCap is how many companies to review one search may have and still be ranked
// by fit; past it, they come newest first as everywhere else.
const searchFitCap = 500

func fill(tpl, first, company, org string) string {
	return strings.NewReplacer("{first}", first, "{company}", company, "{org}", org).Replace(tpl)
}

// NeedsNextStep is a live conversation with no next step, or one that's due.
func NeedsNextStep(c Contact, day string) bool {
	if c.Stage == "won" || c.Stage == "lost" {
		return false
	}
	return c.NextStep == "" || (c.NextDate != nil && *c.NextDate <= day)
}

type CompanyListRow struct {
	Company
	People   int
	Pipeline string
}

const companiesPageSize = 50

// A company stands where its furthest contact does. The stages run in this order, so a
// company with one lost conversation and another person still in outreach is in outreach.
var pipelineOrder = []string{"research", "lost", "outreach", "replied", "meeting", "proposal", "won"}

// furthestSQL must agree with furthest; TestPipelineInSQL holds them together.
const furthestSQL = `max(case
	when declined is not null then case when replied is not null or linkedin_added is not null or email_sent is not null or called is not null then 2 else 0 end
	when replied is not null then case stage when 'lost' then 1 when 'meeting' then 4 when 'proposal' then 5 when 'won' then 6 else 3 end
	when linkedin_added is not null or email_sent is not null or called is not null then 2
	else 0 end)`

func furthest(p Contact) int {
	switch {
	case p.Declined != nil:
		// Someone who declined before anyone reached them was never in the pipeline.
		if p.Touched() || p.Replied != nil {
			return 2
		}
		return 0
	case p.Replied != nil:
		switch p.Stage {
		case "lost":
			return 1
		case "meeting":
			return 4
		case "proposal":
			return 5
		case "won":
			return 6
		}
		return 3
	case p.Touched():
		return 2
	}
	return 0
}

func pipelineAt(c Company, rank int) string {
	if rank == 0 && c.Stage == CompanyRejected {
		return "rejected"
	}
	return pipelineOrder[rank]
}

// pipeline is where a company stands, from its own stage and its furthest contact.
func pipeline(c Company, people []Contact) string {
	best := 0
	for _, p := range people {
		best = max(best, furthest(p))
	}
	return pipelineAt(c, best)
}

type Stats struct{ Touches, Replies, Meetings, Researched int64 }

type AgentEvent struct {
	At     time.Time
	Actor  string
	Action string
	Value  string
}

const activityPageSize = 20

// QuickAdd is the "Add person" form and the LinkedIn bookmarklet: one person straight into the LinkedIn queue.
type QuickAdd struct {
	Linkedin string `json:"linkedin"`
	Name     string `json:"name"`
	Title    string `json:"title"`
	Company  string `json:"company"`
	Email    string `json:"email"`
	Note     string `json:"note"`
	Added    bool   `json:"added"`
}

func uniqueKey(tx *gorm.DB, org int64, key string) string {
	try := key
	for i := 2; ; i++ {
		var n int64
		_ = logged(tx.Model(&Contact{}).Where("org_id = ? and key = ?", org, try).Count(&n))
		if n == 0 {
			return try
		}
		try = fmt.Sprintf("%s (%d)", key, i)
	}
}

// notListed keeps the companies the list covers out of a query, as coversCompany does.
func notListed(q *gorm.DB, set OrgSettings) *gorm.DB {
	for _, host := range set.NeverDomains() {
		q = q.Where("not (lower(domain) = ? or lower(domain) like ?)", host, "%."+host)
	}
	return q
}

// LinkedIn page titles look like "(3) Hannah Pryce | LinkedIn"; headlines "Head of Marketing at Larkmoor Yachts".
var (
	unreadPrefix = regexp.MustCompile(`^\(\d+\)\s*`)
	linkedinTail = regexp.MustCompile(`\s*[|–-]\s*LinkedIn.*$`)
)

func ParseLinkedIn(title, headline string) (name, role, company string) {
	name = strings.TrimSpace(linkedinTail.ReplaceAllString(unreadPrefix.ReplaceAllString(title, ""), ""))
	headline = strings.TrimSpace(strings.SplitN(headline, "|", 2)[0])
	if i := strings.LastIndex(headline, " at "); i > 0 {
		return name, strings.TrimSpace(headline[:i]), strings.TrimSpace(headline[i+4:])
	}
	if i := strings.LastIndex(headline, " @ "); i > 0 {
		return name, strings.TrimSpace(headline[:i]), strings.TrimSpace(headline[i+3:])
	}
	return name, headline, ""
}
