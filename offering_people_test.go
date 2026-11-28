package main

import (
	"strings"
	"testing"

	"github.com/shutthegoatup/prospector/internal/prospecting"
	"github.com/shutthegoatup/prospector/internal/request"
	"github.com/shutthegoatup/prospector/internal/research"
	"github.com/shutthegoatup/prospector/internal/store"
)

func TestPeopleAreReachedForTheOfferingTheyWerePickedFor(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	cto := must(s.AddOffering(o.ID, store.Offering{Name: "Interim CTO", NoteTemplate: "Hi {first}, about technology leadership at {company}.",
		Pitch: "I step in as interim CTO at companies like {company}."}, "Ada"))
	task := must(s.CreateTaskFor(o.ID, cto.ID, "", "Find scale-ups without a CTO", "Ada"))
	scout := store.Actor{Name: "agent:scout", KeyID: 2}

	co := must(s.AgentAddCompany(o.ID, scout, research.AddCompanyIn{Name: "Northwind Payments", Domain: "northwind.example"}))
	ownPerson := must(s.AgentAddContact(o, scout, prospecting.AddContactIn{Company: co.Name, Name: "Ada Vale", Title: "Head of Platform",
		SourceURL: "https://northwind.example/team"}))
	ctoPerson := must(s.AgentAddContact(o, scout, prospecting.AddContactIn{Company: co.Name, Name: "Bram Osei", Title: "CEO",
		SourceURL: "https://northwind.example/about", Task: task.ID}))
	for _, id := range []int64{ownPerson.ID, ctoPerson.ID} {
		if err := s.PickContact(o.ID, o, id, "Ada"); err != nil {
			t.Fatal(err)
		}
	}

	var picked store.Contact
	if err := s.InOrg(o.ID).Where("id = ?", ctoPerson.ID).First(&picked).Error; err != nil {
		t.Fatal(err)
	}
	if picked.OfferingID != cto.ID || !strings.HasPrefix(store.Deref(picked.Note), "Hi Bram, about technology leadership at Northwind Payments") {
		t.Fatalf("someone suggested for an offering should get its note: %d %q", picked.OfferingID, store.Deref(picked.Note))
	}
	if v := must(s.Verdicts(o.ID, co.ID)); len(v) != 1 || v[0].Stage != prospecting.CompanyQualified {
		t.Fatalf("picking someone for an offering should qualify the company for it: %+v", v)
	}
	if got := companyRow(t, s, o.ID, co.ID).Stage; got != prospecting.CompanyQualified {
		t.Fatalf("picking someone for the organisation's own should still qualify the company: %s", got)
	}

	script := callScript(picked, "Bram Osei", request.Voice{Caller: "Ada", Org: "Alpha", Pitch: "We help {company}.", Pitches: map[int64]string{cto.ID: cto.Pitch}})
	if !strings.Contains(scriptText(script), "I step in as interim CTO at companies like Northwind Payments") {
		t.Fatalf("the call script should pitch the offering they're reached for: %+v", script)
	}

	// Rejected for one offering, only its people leave the queue.
	if err := s.JudgeCompany(o.ID, cto.ID, co.ID, prospecting.CompanyRejected, "", "CTO hired", "Ada"); err != nil {
		t.Fatal(err)
	}
	if due := dueNames(t, s, o); due["Bram"] || !due["Ada"] {
		t.Fatalf("rejecting for the interim CTO offering should take only its people out of the queue: %v", due)
	}
	if err := s.JudgeCompany(o.ID, cto.ID, co.ID, prospecting.CompanyQualified, "", "", "Ada"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetCompanyStage(o.ID, co.ID, prospecting.CompanyRejected, "", "Ada"); err != nil {
		t.Fatal(err)
	}
	if due := dueNames(t, s, o); !due["Bram"] || due["Ada"] {
		t.Fatalf("rejecting for the organisation's own should take only its people out of the queue: %v", due)
	}

	snapshot := must(s.SnapshotText(o.ID))
	if _, err := s.Restore(o.ID, snapshot); err != nil {
		t.Fatal(err)
	}
	if err := s.InOrg(o.ID).Where("id = ?", ctoPerson.ID).First(&picked).Error; err != nil || picked.OfferingID != cto.ID {
		t.Fatalf("a snapshot and restore should keep what someone is reached for: %d %v", picked.OfferingID, err)
	}

	// With the offering gone, its people are the organisation's own again.
	if err := s.SetTaskClosed(o.ID, task.ID, true, "Ada"); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveOffering(o.ID, cto.ID, "Ada"); err != nil {
		t.Fatal(err)
	}
	if err := s.InOrg(o.ID).Where("id = ?", ctoPerson.ID).First(&picked).Error; err != nil || picked.OfferingID != 0 {
		t.Fatalf("removing an offering should hand its people to the organisation's own: %d %v", picked.OfferingID, err)
	}
	// A snapshot from before the removal names an offering that is no longer there.
	if _, err := s.Restore(o.ID, snapshot); err != nil {
		t.Fatal(err)
	}
	if err := s.InOrg(o.ID).Where("id = ?", ctoPerson.ID).First(&picked).Error; err != nil || picked.OfferingID != 0 {
		t.Fatalf("a restore naming a removed offering should leave them the organisation's own: %d %v", picked.OfferingID, err)
	}
}

func TestPickingForAnOfferingLeavesTheCompanysOwnStage(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	o.Settings.NoteTemplate, o.Settings.Pitch = "Hello {first} from {org}.", "Our own pitch for {company}."
	if err := s.SaveOrg(o); err != nil {
		t.Fatal(err)
	}
	plain := must(s.AddOffering(o.ID, store.Offering{Name: "Agency partners"}, "Ada"))
	task := must(s.CreateTaskFor(o.ID, plain.ID, "", "Find agencies", "Ada"))
	scout := store.Actor{Name: "agent:scout", KeyID: 2}
	co := must(s.AgentAddCompany(o.ID, scout, research.AddCompanyIn{Name: "Quayside Talent", Domain: "quayside.example", Task: task.ID}))
	p := must(s.AgentAddContact(o, scout, prospecting.AddContactIn{Company: co.Name, Name: "Rhea Moss", Title: "Contract lead",
		SourceURL: "https://quayside.example/team", Task: task.ID}))
	if err := s.PickContact(o.ID, o, p.ID, "Ada"); err != nil {
		t.Fatal(err)
	}
	if got := companyRow(t, s, o.ID, co.ID).Stage; got != prospecting.CompanyReview {
		t.Fatalf("picking someone only for an offering changed the company's own stage to %s", got)
	}
	var c store.Contact
	if err := s.InOrg(o.ID).Where("id = ?", p.ID).First(&c).Error; err != nil || store.Deref(c.Note) != "Hello Rhea from "+o.Name+"." {
		t.Fatalf("an offering without a note of its own should use the organisation's: %q %v", store.Deref(c.Note), err)
	}
	script := scriptText(callScript(c, "Rhea Moss", request.Voice{Org: "alpha", Pitch: o.Settings.Pitch, Pitches: map[int64]string{plain.ID: ""}}))
	if !strings.Contains(script, "Our own pitch for Quayside Talent.") {
		t.Fatalf("an offering without a pitch of its own should use the organisation's: %s", script)
	}
}

func scriptText(lines []Line) string {
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l.Say.Error() + "\n")
	}
	return b.String()
}
