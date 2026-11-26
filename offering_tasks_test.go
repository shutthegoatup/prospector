package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/shutthegoatup/prospector/api"
)

func TestATaskFindsCompaniesForItsOffering(t *testing.T) {
	t.Parallel()
	s, o, srv, _ := signedIn(t)
	cto := must(s.AddOffering(o.ID, Offering{Name: "Interim CTO", LookFor: "trigger: a CTO leaving or a funding round"}, "Ada"))
	secret, _, err := s.CreateKey(o.ID, "robot", []string{"tasks:write", "read"}, "Ada Lovelace")
	if err != nil {
		t.Fatal(err)
	}
	code, body := asKey(t, "POST", srv.URL+"/o/alpha/api/v1/tasks", secret, map[string]any{"title": "Scale-ups", "brief": "Find scale-ups without a CTO", "offering": cto.ID})
	var task api.TaskStarted
	if json.Unmarshal([]byte(body), &task); code != http.StatusCreated || task.OfferingId != cto.ID {
		t.Fatalf("starting a task for an offering: %d %s", code, body)
	}

	beta := mustOrg(t, s, "beta")
	theirs := must(s.AddOffering(beta.ID, Offering{Name: "Theirs"}, "Grace"))
	if code, body := asKey(t, "POST", srv.URL+"/o/alpha/api/v1/tasks", secret, map[string]any{"brief": "x", "offering": theirs.ID}); code != http.StatusNotFound {
		t.Fatalf("a task for another organisation's offering: %d %s", code, body)
	}

	// Rejected for the organisation's own offering, which says nothing about this one.
	scout := Actor{Name: "agent:scout", KeyID: 9}
	known := must(s.AgentAddCompany(o.ID, scout, AddCompanyIn{Name: "Harbour Analytics", Domain: "harbour.example"}))
	if err := s.SetCompanyStage(o.ID, known.ID, CompanyRejected, "", "Ada"); err != nil {
		t.Fatal(err)
	}

	added := must(s.AgentAddCompany(o.ID, scout, AddCompanyIn{Name: "Northwind Payments", Domain: "northwind.example", Task: task.Id}))
	if added.Stage != CompanyReview {
		t.Fatalf("a company found for an offering should wait in its review: %+v", added)
	}
	again := must(s.AgentAddCompany(o.ID, scout, AddCompanyIn{Name: "Harbour Analytics", Task: task.Id}))
	if again.Stage != CompanyReview {
		t.Fatalf("a company rejected only for the organisation's own should be open for this offering: %+v", again)
	}
	if got := companyRow(t, s, o.ID, again.ID).Stage; got != CompanyRejected {
		t.Fatalf("finding it for an offering changed the company's own stage to %s", got)
	}

	if err := s.JudgeCompany(o.ID, cto.ID, added.ID, CompanyRejected, "", "CTO hired in September", "Ada"); err != nil {
		t.Fatal(err)
	}
	if out := must(s.AgentAddCompany(o.ID, scout, AddCompanyIn{Name: "Northwind Payments", Task: task.Id})); out.Stage != CompanyRejected {
		t.Fatalf("adding a company rejected for this offering again should say so: %+v", out)
	}

	view := must(s.AgentTask(o.ID, scout, task.Id, PageIn{}))
	if view.Offering == nil || view.Offering.Name != "Interim CTO" || !strings.Contains(view.Offering.Guidance, "trigger") {
		t.Fatalf("an agent should be told what the task is for and what to look for: %+v", view.Offering)
	}
	stages := map[string]string{}
	for _, c := range view.Companies {
		stages[c.Name] = c.Stage
	}
	if stages["Northwind Payments"] != CompanyRejected || stages["Harbour Analytics"] != CompanyReview {
		t.Fatalf("a task's companies should stand as they do for its offering: %v", stages)
	}

	detail := must(s.AgentCompany(o.ID, scout, "Northwind Payments", PageIn{}))
	if len(detail.Standing) != 1 || detail.Standing[0].Offering != "Interim CTO" || detail.Standing[0].Reason != "CTO hired in September" {
		t.Fatalf("an agent reading the company should see where it stands for each offering: %+v", detail.Standing)
	}

	own := must(s.CreateTask(o.ID, "", "Find more like Larkmoor", "Ada"))
	if v := must(s.AgentTask(o.ID, scout, own.ID, PageIn{})); v.Offering != nil {
		t.Fatalf("a task for the organisation's own offering names none: %+v", v.Offering)
	}

	if code, body := asKey(t, "GET", srv.URL+"/o/alpha/api/v1/offerings/"+itoa(cto.ID)+"/verdicts?stage=junk", secret, nil); code != http.StatusBadRequest {
		t.Fatalf("listing verdicts at a stage there is no such thing as: %d %s", code, body)
	}

	// Agents still on it would be left finding for the wrong thing.
	if err := s.RemoveOffering(o.ID, cto.ID, "Ada"); err == nil || !strings.Contains(err.Error(), "close them first") {
		t.Fatalf("removing an offering an open task is finding for: %v", err)
	}
	if err := s.SetTaskClosed(o.ID, task.Id, true, "Ada"); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveOffering(o.ID, cto.ID, "Ada"); err != nil {
		t.Fatalf("removing it once its tasks are closed: %v", err)
	}
	if v, err := s.AgentTask(o.ID, scout, task.Id, PageIn{}); err != nil || v.Offering != nil {
		t.Fatalf("a closed task whose offering was removed should still read: %+v %v", v, err)
	}
	if err := s.SetTaskClosed(o.ID, task.Id, false, "Ada"); err == nil || !strings.Contains(err.Error(), "removed") {
		t.Fatalf("reopening a task whose offering was removed: %v", err)
	}
}

func TestWhateverAnAgentWorksOnForAnOfferingWaitsInItsReview(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	agencies := must(s.AddOffering(o.ID, Offering{Name: "Agency partners"}, "Ada"))
	task := must(s.CreateTaskFor(o.ID, agencies.ID, "", "Find agencies", "Ada"))
	scout := Actor{Name: "agent:scout", KeyID: 3}
	// Known before the task, and reached through a person and a fact rather than add_company.
	must(s.AgentAddCompany(o.ID, scout, AddCompanyIn{Name: "Quayside Talent", Domain: "quayside.example"}))
	must(s.AgentAddCompany(o.ID, scout, AddCompanyIn{Name: "Fernhill Search", Domain: "fernhill.example"}))
	must(s.AgentAddContact(o, scout, AddContactIn{Company: "Quayside Talent", Name: "Rhea Moss", Title: "Contract lead",
		SourceURL: "https://quayside.example/team", Task: task.ID}))
	must(s.AgentAddFact(o.ID, scout, AddFactIn{Company: "Fernhill Search", Task: task.ID,
		FactIn: FactIn{Field: "agency_focus", Value: "Contract cloud roles", SourceURL: "https://fernhill.example/practice"}}))

	waiting := map[string]bool{}
	for _, v := range must(s.Judged(o.ID, agencies.ID, CompanyReview)) {
		waiting[v.Company] = true
	}
	if !waiting["Quayside Talent"] || !waiting["Fernhill Search"] {
		t.Fatalf("companies an agent worked on for the offering should wait in its review: %v", waiting)
	}
}

func TestExistingTasksAreForTheOrganisationsOwnOffering(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	old := must(s.CreateTask(o.ID, "", "Find makers of kayaks", "Ada"))
	// A database from before the column existed, with tasks already in it.
	if err := s.db.Migrator().DropColumn(&Task{}, "offering_id"); err != nil {
		t.Fatal(err)
	}
	if err := s.db.AutoMigrate(&Task{}); err != nil {
		t.Fatalf("adding the column to a table with rows: %v", err)
	}
	var got Task
	if err := s.in(o.ID).Where("id = ?", old.ID).First(&got).Error; err != nil || got.OfferingID != 0 {
		t.Fatalf("an existing task after the upgrade: %+v %v", got, err)
	}
	if out := must(s.AgentAddCompany(o.ID, Actor{Name: "agent:scout"}, AddCompanyIn{Name: "Larkmoor", Task: old.ID})); out.Stage != CompanyReview {
		t.Fatalf("an existing task still finds companies for the organisation: %+v", out)
	}
	var verdicts int64
	if s.in(o.ID).Model(&Verdict{}).Count(&verdicts); verdicts != 0 {
		t.Fatalf("a task for the organisation's own offering wrote %d verdicts", verdicts)
	}
}
