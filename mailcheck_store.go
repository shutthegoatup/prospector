package main

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm/clause"

	"github.com/shutthegoatup/prospector/internal/say"
)

// TakesMail answers from what is kept while it is fresh, and asks DNS otherwise.
// Without a resolver, as in tests that haven't given one, it never knows.
func (s *outreachStore) TakesMail(ctx context.Context, domain string) mailAnswer {
	if domain == "" {
		return mailUnknown
	}
	var kept MailDomain
	if s.DB.Where("domain = ?", domain).Limit(1).Find(&kept).RowsAffected > 0 {
		fresh := mailTakenFor
		if !kept.TakesMail {
			fresh = mailRefusedFor
		}
		if time.Since(kept.CheckedAt) < fresh {
			return answerOf(kept.TakesMail)
		}
	}
	if s.DNS == nil {
		return mailUnknown
	}
	asking, cancel := context.WithTimeout(ctx, mailLookupFor)
	defer cancel()
	answer := askDNS(asking, s.DNS, domain)
	if answer != mailUnknown {
		row := MailDomain{Domain: domain, TakesMail: answer == mailTaken, CheckedAt: time.Now()}
		_ = logged(s.DB.Clauses(clause.OnConflict{UpdateAll: true}).Create(&row))
	}
	return answer
}

// NoMailDomains are the domains known not to take mail, for pages that mustn't wait on DNS.
func (s *outreachStore) NoMailDomains() map[string]bool {
	var domains []string
	_ = logged(s.DB.Model(&MailDomain{}).Where("takes_mail = ? and checked_at > ?", false, time.Now().Add(-mailRefusedFor)).Pluck("domain", &domains))
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
			return say.Errorf("nothing was stored: %s takes no mail, so %s can't be right. Leave email out, or find the address on a page", domain, e)
		}
	}
	return nil
}

func (s *outreachStore) CheckSomeMail(ctx context.Context, most int) {
	if s.DNS == nil {
		return
	}
	// A batch of people at a time, in the order they were added, until the round's
	// lookups are spent: a round reads only as far as it needs to.
	const batch = 1000
	asked := map[string]bool{}
	lookups := 0
	for after := int64(0); ; {
		var rows []struct {
			ID    int64
			Email string
		}
		if err := logged(s.DB.Model(&Contact{}).Select("id, email").Where("id > ? and email is not null and email <> ''", after).
			Order("id").Limit(batch).Scan(&rows)); err != nil || len(rows) == 0 {
			return
		}
		after = rows[len(rows)-1].ID
		domainsOf := map[int64][]string{}
		var fresh []string
		for _, r := range rows {
			for _, e := range emailRe.FindAllString(strings.ToLower(r.Email), -1) {
				if d := mailDomainOf(e); d != "" {
					domainsOf[r.ID] = append(domainsOf[r.ID], d)
					if _, seen := asked[d]; !seen {
						fresh = append(fresh, d)
					}
				}
			}
		}
		// Fresh answers are passed over without counting, so a round reaches past them.
		if len(fresh) > 0 {
			var kept []MailDomain
			if err := logged(s.DB.Where("domain in ?", fresh).Find(&kept)); err != nil {
				return
			}
			for _, k := range kept {
				good := mailTakenFor
				if !k.TakesMail {
					good = mailRefusedFor
				}
				asked[k.Domain] = time.Since(k.CheckedAt) < good
			}
		}
		for _, r := range rows {
			for _, domain := range domainsOf[r.ID] {
				if asked[domain] {
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
		if len(rows) < batch {
			return
		}
	}
}
