package main

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/shutthegoatup/prospector/internal/crm"
	"github.com/shutthegoatup/prospector/internal/prospecting"
	"github.com/shutthegoatup/prospector/internal/research"
	"github.com/shutthegoatup/prospector/internal/tickets"
)

// together runs do n times at once and fails the test if any of them does.
func together(t *testing.T, n int, do func(i int) error) {
	t.Helper()
	start := make(chan struct{})
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs[i] = do(i)
		}()
	}
	close(start)
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("caller %d of %d at once: %v", i, n, err)
		}
	}
}

func count(t *testing.T, s *Store, model any, where string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := s.DB.Model(model).Where(where, args...).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

const atOnce = 8

// What an agent's repeated work mustn't double, it mustn't double when two agents do
// it in the same moment either.
func TestAgentsWorkingAtOnceAddEachThingOnce(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	scout := func(i int) Actor { return Actor{Name: fmt.Sprintf("agent:scout-%d", i), KeyID: int64(i + 1)} }
	task := must(s.CreateTask(o.ID, "", "Find makers of garden rooms", "Nia"))

	together(t, atOnce, func(i int) error {
		name := []string{"Larkmoor", "LARKMOOR", "larkmoor"}[i%3]
		_, err := s.AgentAddCompany(o.ID, scout(i), research.AddCompanyIn{Name: name})
		return err
	})
	if n := count(t, s, &Company{}, "org_id = ?", o.ID); n != 1 {
		t.Fatalf("one company added under three spellings at once became %d", n)
	}

	together(t, atOnce, func(i int) error {
		_, err := s.AgentAddCompany(o.ID, scout(i), research.AddCompanyIn{Name: fmt.Sprintf("Fenwick Rooms %d", i), Domain: "fenwick.example"})
		return err
	})
	if n := count(t, s, &Company{}, "org_id = ? and domain = ?", o.ID, "fenwick.example"); n != 1 {
		t.Fatalf("one website added under %d names at once became %d companies", atOnce, n)
	}

	var before Company
	if err := s.InOrg(o.ID).Where("lower(name) = ?", "larkmoor").First(&before).Error; err != nil {
		t.Fatal(err)
	}
	defer func() {
		var after Company
		if s.DB.First(&after, before.ID); !after.UpdatedAt.Equal(before.UpdatedAt) {
			t.Errorf("adding to a company counted as changing it: updated %v, was %v", after.UpdatedAt, before.UpdatedAt)
		}
	}()

	together(t, atOnce, func(i int) error {
		_, err := s.AgentAddFact(o.ID, scout(i), research.AddFactIn{Company: "Larkmoor", Task: task.ID,
			FactIn: research.FactIn{Field: "press", Value: "Opened a second workshop", SourceURL: "https://larkmoor.example/news"}})
		return err
	})
	if n := count(t, s, &Fact{}, "org_id = ?", o.ID); n != 1 {
		t.Fatalf("one fact added %d times at once was stored %d times", atOnce, n)
	}

	together(t, atOnce, func(i int) error {
		_, err := s.AgentAddContact(o, scout(i), prospecting.AddContactIn{Company: "Larkmoor", Name: "Ada Vale", Title: "Head of Digital",
			Email: "ada.vale@larkmoor.example", SourceURL: "https://larkmoor.example/team", Task: task.ID})
		return err
	})
	if n := count(t, s, &Contact{}, "org_id = ? and key not like 'org:%'", o.ID); n != 1 {
		t.Fatalf("one person suggested %d times at once became %d contacts", atOnce, n)
	}

	// Every agent after the first found the fact already here, which links it to the task once.
	if n := count(t, s, &Event{}, "org_id = ? and task_id = ? and action = 'task:match' and contact_id = 0", o.ID, task.ID); n > 1 {
		t.Fatalf("the company was linked to the task %d times", n)
	}
}

func TestTheDatabaseKeepsACompanyNameToOneSpelling(t *testing.T) {
	t.Parallel()
	// It drops the index, so on a database no other test will borrow.
	s := ownStore(t)
	o := mustOrg(t, s, "alpha")
	if err := s.DB.Create(&Company{OrgID: o.ID, Name: "Larkmoor", Stage: prospecting.CompanyReview}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Create(&Company{OrgID: o.ID, Name: "LARKMOOR", Stage: prospecting.CompanyReview}).Error; err == nil {
		t.Fatal("a second company with the same name in other capitals was stored")
	}
	other := mustOrg(t, s, "beta")
	if err := s.DB.Create(&Company{OrgID: other.ID, Name: "LARKMOOR", Stage: prospecting.CompanyReview}).Error; err != nil {
		t.Fatalf("another organisation's company of the same name: %v", err)
	}

	// A database from before the index, which already holds two spellings of one name.
	if err := s.DB.Exec("drop index idx_companies_org_name_folded").Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Create(&Company{OrgID: o.ID, Name: "LARKMOOR", Stage: prospecting.CompanyReview}).Error; err != nil {
		t.Fatal(err)
	}
	s.oneNameWhateverItsCapitals()
	got := must(s.AgentAddCompany(o.ID, Actor{Name: "agent:scout"}, research.AddCompanyIn{Name: "larkmoor"}))
	if !got.Existed || count(t, s, &Company{}, "org_id = ?", o.ID) != 2 {
		t.Fatalf("with the index unbuilt, the look-up should still find the company: %+v", got)
	}
}

func TestAnEnquirySentTwiceAtOnceIsKeptOnce(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	together(t, atOnce, func(int) error {
		_, err := s.AddEnquiry(o.ID, "form", tickets.EnquiryIn{Kind: "demo", Name: "Ada Vale", Email: "ada.vale@larkmoor.example", Message: "Could you show us?"})
		return err
	})
	if n := count(t, s, &Enquiry{}, "org_id = ?", o.ID); n != 1 {
		t.Fatalf("a form that sent one enquiry %d times at once left %d", atOnce, n)
	}
}

// Whichever comes first, the person's words are what is left: an agent that drafts
// before the edit is overwritten by it, and one that drafts after is refused.
func TestAnAgentDraftingDuringAnEditLeavesThePersonsWords(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	agent := Actor{Name: "agent:scout", KeyID: 1}
	for round := range 20 {
		p := must(s.QuickAddPerson(o, crm.QuickAdd{Name: fmt.Sprintf("Person %d", round), Company: "Larkmoor"}, "Nia"))
		together(t, 2, func(i int) error {
			if i == 0 {
				return s.Update(o.ID, p.ID, "body", "Nia's own words", "Nia")
			}
			// Refused when the edit got there first, which is as it should be.
			err := s.AgentDraft(o.ID, agent, research.DraftIn{ContactID: p.ID, Kind: "email", Text: "The agent's draft"})
			if err != nil && !strings.Contains(err.Error(), "edited this text") {
				return err
			}
			return nil
		})
		var got Contact
		if err := s.DB.First(&got, p.ID).Error; err != nil {
			t.Fatal(err)
		}
		if deref(got.Body) != "Nia's own words" {
			t.Fatalf("round %d: an agent's draft replaced what a person had just written: %q", round, deref(got.Body))
		}
	}
}
