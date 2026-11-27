package outreach

import (
	"context"
	"errors"
	"net"
	"strings"
	"time"

	"golang.org/x/net/idna"

	"github.com/shutthegoatup/prospector/internal/store"
)

type MailAnswer int

const (
	MailAnswerUnknown MailAnswer = iota
	MailTaken
	MailRefused
)

const mailTakenFor = 30 * 24 * time.Hour

const MailRefusedFor = 7 * 24 * time.Hour

const mailLookupFor = 5 * time.Second

func MailDomainOf(address string) string {
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

// AskDNS follows RFC 5321 and 7505: a null MX refuses mail, and a domain with no MX
// but an address of its own takes it there.
func AskDNS(ctx context.Context, r store.MailDNS, domain string) MailAnswer {
	mx, err := r.LookupMX(ctx, domain)
	if err == nil && len(mx) > 0 {
		if len(mx) == 1 && strings.TrimSuffix(mx[0].Host, ".") == "" {
			return MailRefused
		}
		return MailTaken
	}
	if err != nil && !isNotFound(err) {
		return MailAnswerUnknown
	}
	if _, err := r.LookupHost(ctx, domain); err == nil {
		return MailTaken
	} else if isNotFound(err) {
		return MailRefused
	}
	return MailAnswerUnknown
}

func answerOf(takes bool) MailAnswer {
	if takes {
		return MailTaken
	}
	return MailRefused
}
