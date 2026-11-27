package main

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm/clause"
)

// TakesMail answers from what is kept while it is fresh, and asks DNS otherwise.
// Without a resolver, as in tests that haven't given one, it never knows.
func (s *outreachStore) TakesMail(ctx context.Context, domain string) mailAnswer {
	if domain == "" {
		return mailUnknown
	}
	var kept MailDomain
	if s.db.Where("domain = ?", domain).Limit(1).Find(&kept).RowsAffected > 0 {
		fresh := mailTakenFor
		if !kept.TakesMail {
			fresh = mailRefusedFor
		}
		if time.Since(kept.CheckedAt) < fresh {
			return answerOf(kept.TakesMail)
		}
	}
	if s.dns == nil {
		return mailUnknown
	}
	asking, cancel := context.WithTimeout(ctx, mailLookupFor)
	defer cancel()
	answer := askDNS(asking, s.dns, domain)
	if answer != mailUnknown {
		row := MailDomain{Domain: domain, TakesMail: answer == mailTaken, CheckedAt: time.Now()}
		_ = logged(s.db.Clauses(clause.OnConflict{UpdateAll: true}).Create(&row))
	}
	return answer
}

// NoMailDomains are the domains known not to take mail, for pages that mustn't wait on DNS.
func (s *outreachStore) NoMailDomains() map[string]bool {
	var domains []string
	_ = logged(s.db.Model(&MailDomain{}).Where("takes_mail = ? and checked_at > ?", false, time.Now().Add(-mailRefusedFor)).Pluck("domain", &domains))
	out := make(map[string]bool, len(domains))
	for _, d := range domains {
		out[d] = true
	}
	return out
}

// checkMailDomains asks about each address an agent sends before it is stored, so a
// made-up domain is turned away while the agent can still look again.
func (s *outreachStore) checkMailDomains(email string) error {
	for _, e := range emailRe.FindAllString(strings.ToLower(email), -1) {
		if domain := mailDomainOf(e); s.TakesMail(context.Background(), domain) == mailRefused {
			return errors.New("nothing was stored: " + domain + " takes no mail, so " + e + " can't be right. Leave email out, or find the address on a page")
		}
	}
	return nil
}

func (s *outreachStore) CheckSomeMail(ctx context.Context, most int) {
	if s.dns == nil {
		return
	}
	var emails []string
	if err := logged(s.db.Model(&Contact{}).Where("email is not null and email <> ''").Distinct().Pluck("email", &emails)); err != nil {
		return
	}
	var kept []MailDomain
	if err := logged(s.db.Find(&kept)); err != nil {
		return
	}
	// Fresh answers are passed over without counting, so a round reaches past them.
	asked := map[string]bool{}
	for _, k := range kept {
		fresh := mailTakenFor
		if !k.TakesMail {
			fresh = mailRefusedFor
		}
		asked[k.Domain] = time.Since(k.CheckedAt) < fresh
	}
	lookups := 0
	for _, email := range emails {
		for _, e := range emailRe.FindAllString(strings.ToLower(email), -1) {
			domain := mailDomainOf(e)
			if domain == "" || asked[domain] {
				continue
			}
			if lookups >= most || ctx.Err() != nil {
				return
			}
			asked[domain] = true
			lookups++
			s.TakesMail(ctx, domain)
		}
	}
}
