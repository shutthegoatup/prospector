package main

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

// fakeDNS answers from tables; a name missing from one is "no such host".
type fakeDNS struct {
	mx    map[string][]*net.MX
	hosts map[string][]string
	fail  map[string]bool
	asked []string
}

func (f *fakeDNS) LookupMX(_ context.Context, name string) ([]*net.MX, error) {
	f.asked = append(f.asked, "mx "+name)
	if f.fail["mx "+name] {
		return nil, &net.DNSError{Err: "server misbehaving", Name: name, IsTemporary: true}
	}
	if mx, ok := f.mx[name]; ok {
		return mx, nil
	}
	return nil, &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
}

func (f *fakeDNS) LookupHost(_ context.Context, name string) ([]string, error) {
	f.asked = append(f.asked, "host "+name)
	if f.fail["host "+name] {
		return nil, &net.DNSError{Err: "i/o timeout", Name: name, IsTimeout: true}
	}
	if h, ok := f.hosts[name]; ok {
		return h, nil
	}
	return nil, &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
}

func TestWhetherADomainTakesMail(t *testing.T) {
	t.Parallel()
	dns := &fakeDNS{
		mx: map[string][]*net.MX{
			"larkmoor.example": {{Host: "mx.larkmoor.example.", Pref: 10}},
			"nomail.example":   {{Host: ".", Pref: 0}},
		},
		hosts: map[string][]string{"bare.example": {"192.0.2.1"}},
		fail:  map[string]bool{"mx flaky.example": true, "host slow.example": true},
	}
	for domain, want := range map[string]mailAnswer{
		"larkmoor.example": mailTaken,
		"nomail.example":   mailRefused,
		"bare.example":     mailTaken,
		"gone.example":     mailRefused,
		"flaky.example":    mailUnknown,
		"slow.example":     mailUnknown,
	} {
		if got := askDNS(context.Background(), dns, domain); got != want {
			t.Errorf("%s: %v, want %v", domain, got, want)
		}
	}
	if got := mailDomainOf("kai@Bücher.Example."); got != "xn--bcher-kva.example" {
		t.Errorf("an international domain is asked about as %q", got)
	}
}

func TestAnAgentsAddressOnADomainThatTakesNoMailIsTurnedAway(t *testing.T) {
	t.Parallel()
	s, o, _, _ := signedIn(t)
	dns := &fakeDNS{mx: map[string][]*net.MX{"corvane.example": {{Host: "mx.corvane.example."}}},
		fail: map[string]bool{"mx flaky.example": true}}
	s.dns = dns
	must(s.AgentAddCompany(o.ID, Actor{Name: "agent:scout"}, AddCompanyIn{Name: "Corvane Boats"}))
	add := func(name, email string) error {
		_, err := s.AgentAddContact(o, Actor{Name: "agent:scout"}, AddContactIn{Company: "Corvane Boats", Name: name,
			Email: email, SourceURL: "https://corvane.example/team"})
		return err
	}
	err := add("Iris Vane", "iris@corvane-boats.example")
	if err == nil || !strings.Contains(err.Error(), "corvane-boats.example takes no mail") {
		t.Fatalf("a made-up domain: %v", err)
	}
	var stored int64
	s.db.Model(&Contact{}).Where("person like 'Iris%'").Count(&stored)
	if stored != 0 {
		t.Errorf("the person was stored though their address was turned away")
	}
	if err := add("Iris Vane", "iris@corvane.example"); err != nil {
		t.Errorf("a domain that takes mail: %v", err)
	}
	// DNS that can't answer says nothing against the address.
	if err := add("Oskar Lund", "oskar@flaky.example"); err != nil {
		t.Errorf("a lookup that failed turned the address away: %v", err)
	}
	var kept []MailDomain
	s.db.Order("domain").Find(&kept)
	if len(kept) != 2 || kept[0].Domain != "corvane-boats.example" || kept[0].TakesMail || kept[1].Domain != "corvane.example" || !kept[1].TakesMail {
		t.Errorf("kept: %+v, want the two definite answers only", kept)
	}
	// A kept answer is used while it is fresh.
	dns.asked = nil
	if err := add("Ada Vane", "ada@corvane-boats.example"); err == nil || len(dns.asked) != 0 {
		t.Errorf("asked DNS again (%v) or let it in (%v)", dns.asked, err)
	}
	s.db.Model(&MailDomain{}).Where("domain = ?", "corvane-boats.example").Update("checked_at", time.Now().Add(-mailRefusedFor-time.Hour))
	dns.mx["corvane-boats.example"] = []*net.MX{{Host: "mx.corvane-boats.example."}}
	if err := add("Ada Vane", "ada@corvane-boats.example"); err != nil {
		t.Errorf("a domain set up since it was last asked about: %v", err)
	}
}

func TestAddressesAlreadyHereAreCheckedAFewAtATime(t *testing.T) {
	t.Parallel()
	s, o, _, _ := signedIn(t)
	person(t, s, o, "Hannah Pryce", "Larkmoor Yachts", "hannah@larkmoor.example", true)
	person(t, s, o, "Jo Marsh", "Larkmoor Yachts", "jo@larkmoor.example, jo.marsh@marsh.example", true)
	person(t, s, o, "Sven Dahl", "Nordkap Boats", "sven@nordkap.example", true)
	s.checkSomeMail(context.Background(), 100)
	var n int64
	if s.db.Model(&MailDomain{}).Count(&n); n != 0 {
		t.Fatalf("without a resolver %d domains were kept", n)
	}
	dns := &fakeDNS{}
	s.dns = dns
	s.checkSomeMail(context.Background(), 2)
	if s.db.Model(&MailDomain{}).Count(&n); n != 2 {
		t.Errorf("%d domains kept after a round of two", n)
	}
	s.checkSomeMail(context.Background(), 2)
	if s.db.Model(&MailDomain{}).Count(&n); n != 3 {
		t.Errorf("%d domains kept, want each of the three once", n)
	}
	dns.asked = nil
	s.checkSomeMail(context.Background(), 100)
	if len(dns.asked) != 0 {
		t.Errorf("fresh answers were asked for again: %v", dns.asked)
	}
}

func TestTheDaysCardWarnsOfADetailThatCantBeRight(t *testing.T) {
	t.Parallel()
	now := time.Now()
	texts := func(c Contact, noMail map[string]bool) string {
		var out []string
		for _, b := range badges(c, "Hannah", now, noMail) {
			out = append(out, b.Text)
		}
		return strings.Join(out, " | ")
	}
	got := texts(Contact{Key: "hannah", Email: str("hannah@larkmoor.example"), Phone: str("+44 1234 (switchboard)")}, map[string]bool{"larkmoor.example": true})
	for _, want := range []string{"larkmoor.example takes no mail", "Phone number isn't one any country uses"} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in %q", want, got)
		}
	}
	got = texts(Contact{Key: "hannah", Email: str("info@larkmoor.example"), Phone: str("+44 20 7946 0958")}, nil)
	if !strings.Contains(got, "info@larkmoor.example is a shared inbox: ask for Hannah's own") || strings.Contains(got, "Phone number") || strings.Contains(got, "takes no mail") {
		t.Errorf("a shared inbox and a good number: %q", got)
	}
	for _, good := range []string{"+44 (0)20 7946 0958", "+44 20 7946 0958 ext. 214", "+1 212 555 0123 / +1 212 555 0199"} {
		if got := texts(Contact{Key: "hannah", Phone: str(good)}, nil); strings.Contains(got, "Phone number") {
			t.Errorf("%s: %q", good, got)
		}
	}
	if got := texts(Contact{Key: "org:larkmoor", Email: str("info@larkmoor.example"), Phone: str("020 7946 0958")}, nil); strings.Contains(got, "shared inbox") || strings.Contains(got, "Phone number") {
		t.Errorf("a company's own inbox, or a number without a country code: %q", got)
	}
}

func TestACampaignLeavesOutAnAddressWhoseDomainTakesNoMail(t *testing.T) {
	t.Parallel()
	s, o, _, _ := signedIn(t)
	person(t, s, o, "Hannah Pryce", "Larkmoor Yachts", "hannah@larkmoor.example", true)
	person(t, s, o, "Sven Dahl", "Nordkap Boats", "sven@nordkap.example", true)
	s.db.Create(&MailDomain{Domain: "nordkap.example", CheckedAt: time.Now()})
	c := must(s.AddCampaign(o.ID, CampaignIn{Name: "Spring price list", Subject: "Our spring prices", Body: "Hello"}, "Ada Lovelace"))
	got := leftOut(must(s.Audience(o, *c)))
	if got["Hannah Pryce"] != "" || got["Sven Dahl"] != "its domain takes no mail" {
		t.Errorf("left out: %+v", got)
	}
	// An answer gone stale no longer counts against them, and asking again replaces it.
	s.db.Model(&MailDomain{}).Where("domain = ?", "nordkap.example").Update("checked_at", time.Now().Add(-mailRefusedFor-time.Hour))
	if got := leftOut(must(s.Audience(o, *c))); got["Sven Dahl"] != "" {
		t.Errorf("a stale answer left them out: %q", got["Sven Dahl"])
	}
	s.dns = &fakeDNS{mx: map[string][]*net.MX{"nordkap.example": {{Host: "mx.nordkap.example."}}}}
	if s.takesMail(context.Background(), "nordkap.example") != mailTaken {
		t.Error("asking again didn't find the mail server")
	}
	var kept MailDomain
	if s.db.Where("domain = ?", "nordkap.example").First(&kept); !kept.TakesMail {
		t.Errorf("the stored answer wasn't replaced: %+v", kept)
	}
}

func TestErasingSomeoneForgetsWhatWasAskedAboutTheirDomain(t *testing.T) {
	t.Parallel()
	s, o, _, _ := signedIn(t)
	p := person(t, s, o, "Hannah Pryce", "Larkmoor Yachts", "hannah@pryce.example", true)
	s.db.Create(&MailDomain{Domain: "pryce.example", TakesMail: true, CheckedAt: time.Now()})
	s.db.Create(&MailDomain{Domain: "larkmoor.example", TakesMail: true, CheckedAt: time.Now()})
	if err := s.ErasePerson(o.ID, p.ID, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	var left []string
	s.db.Model(&MailDomain{}).Pluck("domain", &left)
	if len(left) != 1 || left[0] != "larkmoor.example" {
		t.Errorf("kept after the erasure: %v", left)
	}
}
