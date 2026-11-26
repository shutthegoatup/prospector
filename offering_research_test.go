package main

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/shutthegoatup/prospector/api"
)

type offeringQueue struct {
	Items  []api.ResearchCompany
	Counts map[string]int
}

func queueAt(t *testing.T, url, secret string) (int, offeringQueue) {
	t.Helper()
	code, body := asKey(t, "GET", url, secret, nil)
	var out offeringQueue
	if code == http.StatusOK {
		if err := json.Unmarshal([]byte(body), &out); err != nil {
			t.Fatal(err)
		}
	}
	return code, out
}

func researched(p offeringQueue) map[string]api.ResearchCompany {
	out := map[string]api.ResearchCompany{}
	for _, c := range p.Items {
		out[c.Name] = c
	}
	return out
}

func TestEachOfferingHasAResearchQueueOfItsOwn(t *testing.T) {
	s, o, srv, _ := signedIn(t)
	secret, _, err := s.CreateKey(o.ID, "robot", []string{"read", "research:review"}, "Ada Lovelace")
	if err != nil {
		t.Fatal(err)
	}
	roles := must(s.AddOffering(o.ID, Offering{Name: "Contract roles", LookFor: "ir35: outside IR35 only"}, "Ada"))
	task := must(s.CreateTaskFor(o.ID, roles.ID, "", "Find contract roles", "Ada"))
	scout := Actor{Name: "agent:scout", KeyID: 3}
	must(s.AgentAddCompany(o.ID, scout, AddCompanyIn{Name: "Northwind Payments", Domain: "northwind.example", Task: task.ID,
		Facts: []FactIn{{Field: "ir35", Answer: "yes", Value: "Outside IR35", SourceURL: "https://northwind.example/jobs/1"}}}))
	must(s.AgentAddCompany(o.ID, scout, AddCompanyIn{Name: "Larkmoor", Domain: "larkmoor.example"}))

	base := srv.URL + "/o/alpha/api/v1/research"
	code, own := queueAt(t, base, secret)
	if code != http.StatusOK || len(own.Items) != 1 || own.Items[0].Name != "Larkmoor" || own.Counts["review"] != 1 {
		t.Fatalf("a company found only for another offering should stay out of the organisation's own review: %d %+v", code, own)
	}
	code, theirs := queueAt(t, base+"?offering="+itoa(roles.ID), secret)
	north, ok := researched(theirs)["Northwind Payments"]
	if code != http.StatusOK || len(theirs.Items) != 1 || !ok || theirs.Counts["review"] != 1 {
		t.Fatalf("the offering's review: %d %+v", code, theirs)
	}
	code, body := asKey(t, "GET", srv.URL+"/o/alpha/api/v1/companies/"+itoa(north.Id)+"/overview", secret, nil)
	var overview api.CompanyOverview
	if json.Unmarshal([]byte(body), &overview); code != http.StatusOK || overview.FoundFor != roles.ID {
		t.Fatalf("the company's page should say what it was found for: %d %s", code, body)
	}
	code, body = asKey(t, "GET", srv.URL+"/o/alpha/api/v1/tasks", secret, nil)
	var tasks struct{ Items []api.TaskBrief }
	if json.Unmarshal([]byte(body), &tasks); code != http.StatusOK || len(tasks.Items) != 1 || tasks.Items[0].OfferingId != roles.ID {
		t.Fatalf("the task list should say what each task is for: %d %s", code, body)
	}
	for offering, want := range map[int64]int{roles.ID: 1, 0: 0} {
		code, body = asKey(t, "GET", srv.URL+"/o/alpha/api/v1/tasks?offering="+itoa(offering), secret, nil)
		if json.Unmarshal([]byte(body), &tasks); code != http.StatusOK || len(tasks.Items) != want {
			t.Fatalf("the tasks for offering %d: %d %s", offering, code, body)
		}
	}
	code, body = asKey(t, "GET", base+"?task="+itoa(task.ID), secret, nil)
	var queue struct{ OfferingId int64 `json:"offering_id"` }
	if json.Unmarshal([]byte(body), &queue); code != http.StatusOK || queue.OfferingId != roles.ID {
		t.Fatalf("a task's queue should say whose it is, so what is done on it goes there: %d %s", code, body)
	}
	code, body = asKey(t, "GET", srv.URL+"/o/alpha/api/v1/offerings", secret, nil)
	var offerings struct{ Items []api.Offering }
	if json.Unmarshal([]byte(body), &offerings); code != http.StatusOK || len(offerings.Items) != 1 || len(offerings.Items[0].LookingFor) != 1 || offerings.Items[0].LookingFor[0].Field != "ir35" {
		t.Fatalf("an offering's look-for fields, read: %d %s", code, body)
	}
	if north.Fit.Of != 1 || north.Fit.Met != 1 || len(north.Criteria) != 1 || north.Criteria[0].Field != "ir35" {
		t.Fatalf("fit for an offering should be judged against its own look-for fields: %+v", north)
	}
	if code, page := queueAt(t, base+"?task="+itoa(task.ID), secret); code != http.StatusOK || len(page.Items) != 1 || page.Items[0].Name != "Northwind Payments" {
		t.Fatalf("a task's search should be shown for the task's offering: %d %+v", code, page)
	}
	if code, _ := queueAt(t, base+"?task="+itoa(task.ID)+"&offering="+itoa(roles.ID+1), secret); code != http.StatusBadRequest {
		t.Fatalf("asking for a task's search under another offering: %d", code)
	}
	beta := mustOrg(t, s, "beta")
	other := must(s.AddOffering(beta.ID, Offering{Name: "Theirs"}, "Grace"))
	if code, _ := queueAt(t, base+"?offering="+itoa(other.ID), secret); code != http.StatusNotFound {
		t.Fatalf("another organisation's offering: %d", code)
	}
	if n := s.ToReview(o.ID); n != 2 {
		t.Fatalf("the count waiting for review should take in every offering's: %d", n)
	}

	// Qualified for the offering, someone at the company is still to be picked for it.
	review := srv.URL + "/o/alpha/api/v1/companies/" + itoa(north.Id)
	if code, body := asKey(t, "PATCH", review, secret, map[string]any{"stage": "qualified", "offering": roles.ID}); code != http.StatusNoContent {
		t.Fatalf("qualifying for the offering: %d %s", code, body)
	}
	if _, page := queueAt(t, base+"?offering="+itoa(roles.ID)+"&filter=contact", secret); len(page.Items) != 1 || page.Items[0].Stage != api.ResearchCompanyStageQualified {
		t.Fatalf("qualified for the offering with nobody picked: %+v", page)
	}
	if _, page := queueAt(t, base+"?filter=contact", secret); len(page.Items) != 0 {
		t.Fatalf("qualifying for an offering put the company in the organisation's own contact list: %+v", page)
	}

	// Suggested by an agent working for the organisation's own, so in its review now too,
	// and picked from the offering's list for the offering.
	priya := must(s.AgentAddContact(o, scout, AddContactIn{Company: "Northwind Payments", Name: "Priya Shah", Title: "Head of Platform",
		SourceURL: "https://northwind.example/team"}))
	if _, page := queueAt(t, base, secret); researched(page)["Northwind Payments"].Id == 0 {
		t.Fatalf("work for the organisation's own should bring the company into its review: %+v", page)
	}
	pick := srv.URL + "/o/alpha/api/v1/contacts/" + itoa(priya.ID) + "/pick?offering=" + itoa(roles.ID)
	if code, body := asKey(t, "POST", pick, secret, nil); code != http.StatusNoContent {
		t.Fatalf("picking for the offering: %d %s", code, body)
	}
	var got Contact
	if err := s.in(o.ID).Where("id = ?", priya.ID).First(&got).Error; err != nil || got.OfferingID != roles.ID {
		t.Fatalf("someone picked for an offering should be reached for it: %d %v", got.OfferingID, err)
	}
	if _, page := queueAt(t, base+"?offering="+itoa(roles.ID)+"&filter=contact", secret); len(page.Items) != 0 {
		t.Fatalf("picking someone for the offering should take the company off its contact list: %+v", page)
	}
	if got := companyRow(t, s, o.ID, north.Id).Stage; got != CompanyReview {
		t.Fatalf("picking for an offering changed the company's own stage to %s", got)
	}
	if code, body := asKey(t, "POST", srv.URL+"/o/alpha/api/v1/contacts/"+itoa(priya.ID)+"/pick?offering=0", secret, nil); code != http.StatusConflict {
		t.Fatalf("picking someone already being reached for one offering for another: %d %s", code, body)
	}

	if err := s.JudgeCompany(o.ID, roles.ID, north.Id, CompanyRejected, "", "Inside IR35 after all", "Ada"); err != nil {
		t.Fatal(err)
	}
	if _, page := queueAt(t, base+"?offering="+itoa(roles.ID)+"&filter=rejected", secret); len(page.Items) != 1 || page.Items[0].Stage != api.ResearchCompanyStageRejected {
		t.Fatalf("rejected for the offering: %+v", page)
	}
}

func TestRemovingAnOfferingBringsWhatItFoundIntoTheOrganisationsOwnReview(t *testing.T) {
	s, o, srv, _ := signedIn(t)
	cto := must(s.AddOffering(o.ID, Offering{Name: "Interim CTO"}, "Ada"))
	task := must(s.CreateTaskFor(o.ID, cto.ID, "", "Find scale-ups", "Ada"))
	must(s.AgentAddCompany(o.ID, Actor{Name: "agent:scout"}, AddCompanyIn{Name: "Harbour Analytics", Task: task.ID}))
	looked := must(s.AgentAddCompany(o.ID, Actor{Name: "agent:scout"}, AddCompanyIn{Name: "Fernhill Search", Task: task.ID}))
	if n := s.ResearchCount(o.ID, "review"); n != 0 {
		t.Fatalf("found for an offering, in the organisation's own review: %d", n)
	}
	// Someone asking to review it for the organisation's own, from the company's page.
	if err := s.JudgeCompany(o.ID, 0, looked.ID, CompanyReview, "", "", "Ada"); err != nil {
		t.Fatal(err)
	}
	if n := s.ResearchCount(o.ID, "review"); n != 1 {
		t.Fatalf("put up for the organisation's own review, it should be in it: %d", n)
	}
	if err := s.SetTaskClosed(o.ID, task.ID, true, "Ada"); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveOffering(o.ID, cto.ID, "Ada"); err != nil {
		t.Fatal(err)
	}
	if n := s.ResearchCount(o.ID, "review"); n != 2 {
		t.Fatalf("a company found for a removed offering should come up in the organisation's own review: %d", n)
	}
	secret, _, err := s.CreateKey(o.ID, "robot", []string{"read"}, "Ada Lovelace")
	if err != nil {
		t.Fatal(err)
	}
	if code, page := queueAt(t, srv.URL+"/o/alpha/api/v1/research?task="+itoa(task.ID), secret); code != http.StatusOK || len(page.Items) != 2 {
		t.Fatalf("a closed task's search, after its offering was removed: %d %+v", code, page)
	}
}

func TestExistingCompaniesAreInTheOrganisationsOwnReview(t *testing.T) {
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	must(s.AgentAddCompany(o.ID, Actor{Name: "agent:scout"}, AddCompanyIn{Name: "Larkmoor"}))
	// A database from before the column existed, with companies already in it.
	if err := s.db.Migrator().DropColumn(&Company{}, "found_for"); err != nil {
		t.Fatal(err)
	}
	if err := s.db.AutoMigrate(&Company{}); err != nil {
		t.Fatalf("adding the column to a table with rows: %v", err)
	}
	if n := s.ResearchCount(o.ID, "review"); n != 1 {
		t.Fatalf("an existing company after the upgrade should still wait in review: %d", n)
	}
}
