package main

import (
	"context"
	"errors"
	"net"
	"strings"
	"time"

	"github.com/nyaruka/phonenumbers/v2"
	"golang.org/x/net/idna"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// MailDomain is what DNS last said about whether a domain takes mail. Only a
// definite answer is kept: a lookup that timed out says nothing either way.
type MailDomain struct {
	Domain    string    `gorm:"primaryKey"`
	TakesMail bool      `gorm:"not null;default:false"`
	CheckedAt time.Time `gorm:"not null"`
}

// mailDNS is the part of a resolver the check needs; *net.Resolver is one.
type mailDNS interface {
	LookupMX(ctx context.Context, name string) ([]*net.MX, error)
	LookupHost(ctx context.Context, host string) ([]string, error)
}

type mailAnswer int

const (
	mailUnknown mailAnswer = iota
	mailTaken
	mailRefused
)

// A domain that takes mail rarely stops; one that doesn't may be set up next week.
const (
	mailTakenFor   = 30 * 24 * time.Hour
	mailRefusedFor = 7 * 24 * time.Hour
	mailLookupFor  = 5 * time.Second
)

func mailDomainOf(address string) string {
	at := strings.LastIndexByte(address, '@')
	if at < 0 {
		return ""
	}
	domain, err := idna.Lookup.ToASCII(strings.Trim(strings.ToLower(address[at+1:]), ". "))
	if err != nil || !strings.Contains(domain, ".") {
		return ""
	}
	return domain
}

func isNotFound(err error) bool {
	var dns *net.DNSError
	return errors.As(err, &dns) && dns.IsNotFound
}

// askDNS follows RFC 5321 and 7505: a null MX refuses mail, and a domain with no MX
// but an address of its own takes it there.
func askDNS(ctx context.Context, r mailDNS, domain string) mailAnswer {
	mx, err := r.LookupMX(ctx, domain)
	if err == nil && len(mx) > 0 {
		if len(mx) == 1 && strings.TrimSuffix(mx[0].Host, ".") == "" {
			return mailRefused
		}
		return mailTaken
	}
	if err != nil && !isNotFound(err) {
		return mailUnknown
	}
	if _, err := r.LookupHost(ctx, domain); err == nil {
		return mailTaken
	} else if isNotFound(err) {
		return mailRefused
	}
	return mailUnknown
}

// takesMail answers from what is kept while it is fresh, and asks DNS otherwise.
// Without a resolver, as in tests that haven't given one, it never knows.
func (s *Store) takesMail(ctx context.Context, domain string) mailAnswer {
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
		logged(s.db.Clauses(clause.OnConflict{UpdateAll: true}).Create(&row))
	}
	return answer
}

func answerOf(takes bool) mailAnswer {
	if takes {
		return mailTaken
	}
	return mailRefused
}

// noMailDomains are the domains known not to take mail, for pages that mustn't wait on DNS.
func (s *Store) noMailDomains() map[string]bool {
	var domains []string
	logged(s.db.Model(&MailDomain{}).Where("takes_mail = ? and checked_at > ?", false, time.Now().Add(-mailRefusedFor)).Pluck("domain", &domains))
	out := make(map[string]bool, len(domains))
	for _, d := range domains {
		out[d] = true
	}
	return out
}

// deadAddress is the first of an email field's addresses whose domain takes no mail.
func deadAddress(email string, noMail map[string]bool) string {
	for _, e := range emailRe.FindAllString(strings.ToLower(email), -1) {
		if noMail[mailDomainOf(e)] {
			return e
		}
	}
	return ""
}

// checkMailDomains asks about each address an agent sends before it is stored, so a
// made-up domain is turned away while the agent can still look again.
func (s *Store) checkMailDomains(email string) error {
	for _, e := range emailRe.FindAllString(strings.ToLower(email), -1) {
		if domain := mailDomainOf(e); s.takesMail(context.Background(), domain) == mailRefused {
			return errors.New("nothing was stored: " + domain + " takes no mail, so " + e + " can't be right. Leave email out, or find the address on a page")
		}
	}
	return nil
}

// keepCheckingMail asks about the domains of addresses already here, a few each
// round, so people and imports get the same check agents do without waiting on DNS.
func (a *App) keepCheckingMail(ctx context.Context, every time.Duration) {
	for {
		a.s.checkSomeMail(ctx, 100)
		select {
		case <-ctx.Done():
			return
		case <-time.After(every):
		}
	}
}

func (s *Store) checkSomeMail(ctx context.Context, most int) {
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
			s.takesMail(ctx, domain)
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
