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
