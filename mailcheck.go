package main

import (
	"context"
	"strings"
	"time"

	"github.com/nyaruka/phonenumbers/v2"
	"gorm.io/gorm"
)

// A domain that takes mail rarely stops; one that doesn't may be set up next week.

// deadAddress is the first of an email field's addresses whose domain takes no mail.
func deadAddress(email string, noMail map[string]bool) string {
	for _, e := range emailRe.FindAllString(strings.ToLower(email), -1) {
		if noMail[mailDomainOf(e)] {
			return e
		}
	}
	return ""
}

// keepCheckingMail asks about the domains of addresses already here, a few each
// round, so people and imports get the same check agents do without waiting on DNS.
func (a *App) keepCheckingMail(ctx context.Context, every time.Duration) {
	for {
		a.s.CheckSomeMail(ctx, 100)
		select {
		case <-ctx.Done():
			return
		case <-time.After(every):
		}
	}
}

// forgetMailDomains drops what is kept about the domains of an erased person's
// addresses: a domain can be their own name. It is asked about again if anyone else uses it.
func forgetMailDomains(tx *gorm.DB, emails []string) error {
	var domains []string
	for _, e := range emails {
		if d := mailDomainOf(e); d != "" {
			domains = append(domains, d)
		}
	}
	if len(domains) == 0 {
		return nil
	}
	return tx.Where("domain in ?", domains).Delete(&MailDomain{}).Error
}

// roleBoxes are local parts that reach whoever reads a shared inbox, not one person.
var roleBoxes = map[string]bool{"info": true, "hello": true, "contact": true, "sales": true, "enquiries": true,
	"inquiries": true, "office": true, "admin": true, "support": true, "help": true, "team": true, "mail": true,
	"marketing": true, "press": true, "media": true, "hr": true, "jobs": true, "careers": true, "accounts": true,
	"billing": true, "reception": true, "noreply": true, "no-reply": true, "webmaster": true, "general": true}

func roleAddress(email string) string {
	for _, e := range emailRe.FindAllString(strings.ToLower(email), -1) {
		if local, _, _ := strings.Cut(e, "@"); roleBoxes[local] {
			return e
		}
	}
	return ""
}

// phoneLooksWrong is a first number written in full that no country uses. One without
// a country code can't be judged, so it isn't.
func phoneLooksWrong(phone string) bool {
	first := strings.TrimSpace(strings.SplitN(phone, "/", 2)[0])
	if !strings.HasPrefix(first, "+") {
		return false
	}
	n, err := phonenumbers.Parse(first, "")
	return err != nil || !phonenumbers.IsValidNumber(n)
}
