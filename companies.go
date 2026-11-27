package main

import (
	"regexp"
	"strings"

	"gorm.io/gorm"
)

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

// NeedsNextStep is a live conversation with no next step, or one that's due.
func NeedsNextStep(c Contact, day string) bool {
	if c.Stage == "won" || c.Stage == "lost" {
		return false
	}
	return c.NextStep == "" || (c.NextDate != nil && *c.NextDate <= day)
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
