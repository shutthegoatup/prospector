package app

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/shutthegoatup/prospector/internal/store"
)

// TestARoundOfMailChecksReadsOnPastTheFirstBatch: people are read a batch at a time,
// and a round carries on into the next batch while it has lookups to spend, so a domain
// only someone added later holds is still reached.
func TestARoundOfMailChecksReadsOnPastTheFirstBatch(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	people := make([]store.Contact, 1001)
	for i := range people {
		people[i] = store.Contact{OrgID: o.ID, Key: fmt.Sprint("person-", i), File: "x.md", Email: store.Str(fmt.Sprint("p", i, "@larkmoor.example"))}
	}
	people[1000].Email = store.Str("late@windrush.example")
	if err := s.DB.CreateInBatches(people, 500).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Create(&store.MailDomain{Domain: "larkmoor.example", TakesMail: true, CheckedAt: time.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	dns := &fakeDNS{}
	s.DNS = dns
	s.CheckSomeMail(context.Background(), 1)
	asked := strings.Join(dns.asked, " ")
	if !strings.Contains(asked, "windrush.example") {
		t.Fatalf("the one domain past the first batch wasn't checked: asked %v", dns.asked)
	}
	if strings.Contains(asked, "larkmoor.example") {
		t.Errorf("a fresh answer was asked for again: %v", dns.asked)
	}
}
