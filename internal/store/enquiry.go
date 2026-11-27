package store

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Enquiry is something a person sent us: a website form, or a product telling us
// about a sign-up. It is kept as it arrived, and pointed at the contact and company
// it matches when there is one. Its text is the sender's own and is never trusted.
type Enquiry struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	OrgID     int64     `gorm:"index;not null" json:"-"`
	Kind      string    `gorm:"not null;default:'other'" json:"kind"`
	Name      string    `json:"name"`
	Email     string    `gorm:"index" json:"email"`
	Company   string    `json:"company"`
	Phone     string    `json:"phone"`
	Message   string    `json:"message"`
	Site      string    `json:"site"`
	Page      string    `json:"page"`
	Via       string    `gorm:"not null;default:'form'" json:"via"`
	Spam      bool      `gorm:"not null;default:false" json:"spam"`
	ContactID *int64    `gorm:"index" json:"contact_id"`
	CompanyID *int64    `gorm:"index" json:"company_id"`
	CreatedAt time.Time `gorm:"index" json:"created_at"`
	// State is where answering it stands; empty for one that arrived before enquiries
	// were tracked, which nobody is asked to answer now.
	State string `gorm:"not null;default:''" json:"state"`
	// Owner is the teammate answering it.
	Owner string `gorm:"not null;default:''" json:"owner"`
	// Note is ours, about answering it. Nothing sends it, but it is held against whoever
	// wrote in, so it is in their export when they ask what is held on them.
	Note string `gorm:"not null;default:''" json:"note"`
}

// Clip keeps an over-long field's start rather than refusing the enquiry.
func Clip(s string, max int) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > max {
		return string(r[:max])
	}
	return s
}

// EnquiryOrigins reads the setting: the sites whose forms may post, one per line.
func EnquiryOrigins(setting string) ([]string, error) {
	var out []string
	for _, line := range strings.FieldsFunc(setting, func(r rune) bool { return r == '\n' || r == ',' || r == ' ' || r == '\r' }) {
		u, err := url.Parse(line)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return nil, fmt.Errorf("%q isn't a site address: it should look like https://www.example.com", line)
		}
		out = append(out, strings.ToLower(u.Scheme+"://"+u.Host))
	}
	return out, nil
}

func (set OrgSettings) TakesEnquiriesFrom(origin string) bool {
	sites, _ := EnquiryOrigins(set.EnquirySites)
	for _, site := range sites {
		if site == strings.ToLower(origin) {
			return true
		}
	}
	return false
}
