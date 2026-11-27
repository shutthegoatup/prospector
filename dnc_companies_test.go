package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/shutthegoatup/prospector/api"
)

func TestTheNeverContactListCoversACompanyByItsDomain(t *testing.T) {
	t.Parallel()
	set := OrgSettings{NeverContact: "nordkap.example, ines@larkmoor.example\nhttps://www.linkedin.com/in/li-never, https://halvard.example/"}
	for domain, want := range map[string]bool{
		"nordkap.example": true, "shop.nordkap.example": true, "https://www.nordkap.example/": true, "halvard.example": true,
		"larkmoor.example": false, "notnordkap.example": false, "": false, "linkedin.com": false,
	} {
		if got := set.CoversCompany(domain); got != want {
			t.Errorf("%q: covered %v, want %v", domain, got, want)
		}
	}
}

// A company on a never-contact domain is kept and built on, but nobody is asked to
// review it: a search lists it as left out. The list is read as it is now.
func TestANeverContactCompanyIsKeptButLeftOut(t *testing.T) {
	t.Parallel()
	s, o, srv, ada := signedIn(t)
	o.Settings.NeverContact = "nordkap.example"
	if err := s.SaveOrg(o); err != nil {
		t.Fatal(err)
	}
	task := must(s.CreateTask(o.ID, "", "Boat makers.", "Ada Lovelace"))
	scout := Actor{Name: "agent:scout", KeyID: 1}
	nordkap := must(s.AgentAddCompany(o.ID, scout, AddCompanyIn{Name: "Nordkap Boats", Domain: "nordkap.example", Task: task.ID,
		Facts: []FactIn{{Field: "size", Value: "60 staff", SourceURL: "https://nordkap.example/about"}}}))
	must(s.AgentAddCompany(o.ID, scout, AddCompanyIn{Name: "Halvard Marine", Domain: "halvard.example", Task: task.ID}))

	// Kept, with what was found.
	if _, body := call(t, ada, "GET", fmt.Sprintf("%s/o/alpha/api/v1/companies/%d/overview", srv.URL, nordkap.ID), "", nil); !strings.Contains(body, "60 staff") {
		t.Errorf("Nordkap should be kept with its facts: %s", body)
	}
	review := func() string {
		t.Helper()
		_, body := call(t, ada, "GET", fmt.Sprintf("%s/o/alpha/api/v1/research?task=%d", srv.URL, task.ID), "", nil)
		return body
	}
	queue := review()
	var counted struct {
		Counts map[string]int `json:"counts"`
	}
	if json.Unmarshal([]byte(queue), &counted) != nil || strings.Contains(queue, "Nordkap Boats") || !strings.Contains(queue, "Halvard Marine") || counted.Counts["review"] != 1 {
		t.Errorf("only Halvard should wait for review: %s", queue)
	}
	left := leftOutOf(t, ada, srv.URL, task.ID)
	if left.Body.Listed != 1 || len(left.Body.Items) != 1 || left.Body.Items[0].Held != api.TaskCompanyHeldNeverContact {
		t.Errorf("Nordkap should be left out as never contact: %+v", left)
	}
	_, list := call(t, ada, "GET", srv.URL+"/o/alpha/api/v1/tasks", "", nil)
	var tasks api.ListTasks200JSONResponse
	if json.Unmarshal([]byte(list), &tasks.Body) != nil || tasks.Body.Items[0].ToReview != 1 || tasks.Body.Items[0].LeftOut != 1 {
		t.Errorf("the search should have 1 to review and 1 left out: %s", list)
	}

	// Off the list, it waits for review like any other.
	o.Settings.NeverContact = ""
	if err := s.SaveOrg(o); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(review(), "Nordkap Boats") {
		t.Error("off the list, Nordkap should wait for review")
	}
	if left := leftOutOf(t, ada, srv.URL, task.ID); left.Body.Listed != 0 || len(left.Body.Items) != 0 {
		t.Errorf("off the list, Nordkap isn't left out: %+v", left)
	}
}

// An offering's search leaves a never-contact company out the same way, and a customer
// on such a domain is left out once.
func TestANeverContactCompanyIsLeftOutOfAnOfferingsSearchToo(t *testing.T) {
	t.Parallel()
	s, o, srv, ada := signedIn(t)
	scout := Actor{Name: "agent:scout", KeyID: 1}
	cto := must(s.AddOffering(o.ID, Offering{Name: "Interim CTO"}, "Ada"))
	belair := must(s.AgentAddCompany(o.ID, scout, AddCompanyIn{Name: "Belair Kitchens", Domain: "belair.example"}))
	if err := s.SetCompanyStage(o.ID, belair.ID, CompanyQualified, "", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	o.Settings.NeverContact = "belair.example, nordkap.example"
	if err := s.SaveOrg(o); err != nil {
		t.Fatal(err)
	}
	task := must(s.CreateTaskFor(o.ID, cto.ID, "", "Scale-ups.", "Ada Lovelace"))
	must(s.AgentAddCompany(o.ID, scout, AddCompanyIn{Name: "Nordkap Boats", Domain: "nordkap.example", Task: task.ID}))
	must(s.AgentAddCompany(o.ID, scout, AddCompanyIn{Name: "Belair Kitchens", Task: task.ID}))

	_, queue := call(t, ada, "GET", fmt.Sprintf("%s/o/alpha/api/v1/research?task=%d&offering=%d", srv.URL, task.ID, cto.ID), "", nil)
	if strings.Contains(queue, "Nordkap Boats") || strings.Contains(queue, "Belair Kitchens") {
		t.Errorf("neither should wait in the offering's review: %s", queue)
	}
	if left := leftOutOf(t, ada, srv.URL, task.ID); left.Body.Listed != 2 || len(left.Body.Items) != 2 {
		t.Errorf("Nordkap and Belair should each be left out once, as never contact: %+v", left)
	}
}
