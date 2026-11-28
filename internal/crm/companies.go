package crm

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/shutthegoatup/prospector/internal/say"
	"github.com/shutthegoatup/prospector/internal/store"
)

var (
	HttpURL = regexp.MustCompile(`^https?://\S+$`)
)

// EnsureCompany finds a company by name (case-insensitive) or creates it.
func EnsureCompany(tx *gorm.DB, org int64, c store.Company) (*store.Company, bool, error) {
	c.Name = strings.TrimSpace(c.Name)
	if c.Name == "" {
		return nil, false, say.New("company name is required")
	}
	if err := store.HoldOrg(tx, org); err != nil {
		return nil, false, err
	}
	var found store.Company
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

type HistoryItem struct {
	At     time.Time
	Actor  string
	Action string
	Value  string
	Person string
}

const HistoryPageSize = 50

// N counts from 1.
type Page struct{ N, Size, Total int }

// PageOf clamps n, so a page that emptied since it was linked shows the last one instead.
func PageOf(n, size, total int) Page {
	p := Page{N: n, Size: size, Total: total}
	p.N = max(1, min(p.N, p.Pages()))
	return p
}

func (p Page) Pages() int { return max(1, (p.Total+p.Size-1)/p.Size) }

func (p Page) Offset() int { return (p.N - 1) * p.Size }

func Fill(tpl, first, company, org string) string {
	return strings.NewReplacer("{first}", first, "{company}", company, "{org}", org).Replace(tpl)
}

type CompanyListRow struct {
	store.Company
	People   int
	Pipeline string
}

const CompaniesPageSize = 50

// A company stands where its furthest contact does. The stages run in this order, so a
// company with one lost conversation and another person still in outreach is in outreach.
var pipelineOrder = []string{"research", "lost", "outreach", "replied", "meeting", "proposal", "won"}

// furthestSQL must agree with furthest; TestPipelineInSQL holds them together.
const furthestSQL = `max(case
	when declined is not null then case when replied is not null or linkedin_added is not null or email_sent is not null or called is not null then 2 else 0 end
	when replied is not null then case stage when 'lost' then 1 when 'meeting' then 4 when 'proposal' then 5 when 'won' then 6 else 3 end
	when linkedin_added is not null or email_sent is not null or called is not null then 2
	else 0 end)`

func PipelineAt(c store.Company, rank int) string {
	if rank == 0 && c.Stage == CompanyRejected {
		return "rejected"
	}
	return pipelineOrder[rank]
}

type Stats struct{ Touches, Replies, Meetings, Researched int64 }

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

func UniqueKey(tx *gorm.DB, org int64, key string) string {
	try := key
	for i := 2; ; i++ {
		var n int64
		_ = store.Logged(tx.Model(&store.Contact{}).Where("org_id = ? and key = ?", org, try).Count(&n))
		if n == 0 {
			return try
		}
		try = fmt.Sprintf("%s (%d)", key, i)
	}
}

// JudgedByRoles says whether a company's verdict for an offering follows its roles there,
// so a person can't set it by hand.
func JudgedByRoles(tx *gorm.DB, org int64, o *store.Offering, company int64) (bool, error) {
	if !o.HasRoles {
		return false, nil
	}
	var n int64
	err := tx.Model(&store.Role{}).Where("org_id = ? and offering_id = ? and company_id = ?", org, o.ID, company).Count(&n).Error
	return n > 0, err
}

const CompanyReview = "review"

const CompanyQualified = "qualified"

const CompanyRejected = "rejected"

var Editable = map[string]bool{"phone": true, "email": true, "linkedin": true, "sources": true,
	"call_notes": true, "tz": true, "person": true, "send_to": true, "message": true, "note": true,
	"body": true, "followup": true, "subject": true,
	"stage": true, "next_step": true, "next_date": true, "lost_reason": true, "title": true}

// loggedValue are the fields whose new value goes into the history: where a conversation
// stands and when it is due. The rest are a person's details or free text about them,
// which their own row already holds, so the history keeps only that they changed.
var loggedValue = map[string]bool{"stage": true, "next_date": true}

// stringFields are the newer non-pointer columns, which store "" rather than NULL.
var stringFields = map[string]bool{"stage": true, "next_step": true, "lost_reason": true, "title": true}

var Marks = map[string]string{"linkedin": "linkedin_added", "email": "email_sent", "followup": "followup_sent",
	"call": "called", "replied": "replied", "declined": "declined", "hold": "hold", "accepted": "accepted"}

const openContacts = "hold is null and replied is null and declined is null and status <> 'suggested' and key not like 'org:%'"

// A rejected company's people leave the queues without being marked, so qualifying
// it again brings them back as they were. Rejected is for the offering each person is
// reached for: the company's own stage for the organisation's, a verdict for another.
const companyNotRejected = "(company_id is null" +
	" or (offering_id = 0 and not exists (select 1 from companies c where c.id = contacts.company_id" +
	" and c.org_id = contacts.org_id and c.stage = '" + CompanyRejected + "'))" +
	" or (offering_id <> 0 and not exists (select 1 from verdicts v where v.org_id = contacts.org_id" +
	" and v.offering_id = contacts.offering_id and v.company_id = contacts.company_id and v.stage = '" + CompanyRejected + "')))"

// MarkDate compares days as text, in the zone today() uses: parsing the day as UTC
// midnight would call today "the future" for the first hours of it east of UTC.
func MarkDate(date string, now time.Time) error {
	if _, err := store.ParseDay(date); err != nil {
		return say.New("date is YYYY-MM-DD")
	}
	if date > now.Format(time.DateOnly) {
		return say.New("date is in the future")
	}
	return nil
}
