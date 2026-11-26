package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/shutthegoatup/prospector/api"
)

func verdictFor(t *testing.T, s *Store, org, offering, company int64) Verdict {
	t.Helper()
	var v Verdict
	if err := s.in(org).Where("offering_id = ? and company_id = ?", offering, company).First(&v).Error; err != nil {
		t.Fatal(err)
	}
	return v
}

func TestEachRoleAtACompanyIsPursuedOrJunkedOnItsOwn(t *testing.T) {
	s, o, srv, _ := signedIn(t)
	secret, _, err := s.CreateKey(o.ID, "robot", []string{"read", "research:write", "research:review"}, "Ada Lovelace")
	if err != nil {
		t.Fatal(err)
	}
	roles := must(s.AddOffering(o.ID, Offering{Name: "Contract roles", HasRoles: true}, "Ada"))
	cto := must(s.AddOffering(o.ID, Offering{Name: "Interim CTO"}, "Ada"))
	task := must(s.CreateTaskFor(o.ID, roles.ID, "", "Find contract roles", "Ada"))
	scout := Actor{Name: "agent:scout", KeyID: 3}
	north := must(s.AgentAddCompany(o.ID, scout, AddCompanyIn{Name: "Northwind Payments", Domain: "northwind.example", Task: task.ID}))
	lead := must(s.AgentAddRole(o.ID, scout, AddRoleIn{Company: "Northwind Payments", Task: task.ID, RoleIn: RoleIn{
		Title: "Lead platform engineer (contract, 6 months)", SourceURL: "https://northwind.example/jobs/1", Details: "£700/day, outside IR35"}}))
	plain := must(s.AgentAddRole(o.ID, scout, AddRoleIn{Company: "Northwind Payments", Task: task.ID, RoleIn: RoleIn{
		Title: "Platform engineer (contract)", SourceURL: "https://northwind.example/jobs/2"}}))
	if again := must(s.AgentAddRole(o.ID, scout, AddRoleIn{Company: "Northwind Payments", Task: task.ID, RoleIn: RoleIn{
		Title: "Lead platform engineer", SourceURL: "https://northwind.example/jobs/1/"}})); !again.Existed || again.ID != lead.ID {
		t.Fatalf("the same posting again should be the role already here: %+v", again)
	}
	if err := s.JudgeCompany(o.ID, cto.ID, north.ID, CompanyRejected, "", "CTO hired in September", "Ada"); err != nil {
		t.Fatal(err)
	}

	base := srv.URL + "/o/alpha/api/v1"
	judge := func(id int64, body map[string]any) (int, string) {
		return asKey(t, "PATCH", base+"/roles/"+itoa(id), secret, body)
	}
	if code, body := judge(lead.ID, map[string]any{"stage": "pursued"}); code != http.StatusOK || !strings.Contains(body, `"stage":"pursued"`) {
		t.Fatalf("pursuing a role: %d %s", code, body)
	}
	if code, body := judge(plain.ID, map[string]any{"stage": "junked", "reason": "Inside IR35 after all"}); code != http.StatusOK {
		t.Fatalf("junking a role: %d %s", code, body)
	}
	if v := verdictFor(t, s, o.ID, roles.ID, north.ID); v.Stage != CompanyQualified {
		t.Fatalf("one role pursued and one junked, the company should be qualified for the offering: %+v", v)
	}
	if v := verdictFor(t, s, o.ID, cto.ID, north.ID); v.Stage != CompanyRejected {
		t.Fatalf("a role pursued for one offering changed the company's verdict for another: %+v", v)
	}
	if got := companyRow(t, s, o.ID, north.ID).Stage; got != CompanyReview {
		t.Fatalf("judging roles changed the company's own stage to %s", got)
	}

	code, body := asKey(t, "GET", base+"/companies/"+itoa(north.ID)+"/roles", secret, nil)
	var listed struct{ Items []api.Role }
	if json.Unmarshal([]byte(body), &listed); code != http.StatusOK || len(listed.Items) != 2 ||
		listed.Items[1].Stage != api.RoleStageJunked || listed.Items[1].Reason != "Inside IR35 after all" || listed.Items[1].By != "agent:robot" {
		t.Fatalf("a company's roles, the junked one with who and why: %d %s", code, body)
	}
	_, page := queueAt(t, base+"/research?offering="+itoa(roles.ID)+"&filter=contact", secret)
	if len(page.Items) != 1 || len(page.Items[0].Roles) != 2 || page.Items[0].Roles[0].Details != "£700/day, outside IR35" {
		t.Fatalf("the offering's research should carry each company's roles: %+v", page)
	}
	if _, page := queueAt(t, base+"/research?offering="+itoa(cto.ID)+"&filter=rejected", secret); len(page.Items) != 1 || len(page.Items[0].Roles) != 0 {
		t.Fatalf("another offering's research shows roles that aren't its own: %+v", page)
	}

	// The agent is told what was junked, and can't bring it back.
	detail := must(s.AgentCompany(o.ID, scout, "Northwind Payments", PageIn{}))
	if len(detail.Roles) != 2 || detail.Roles[1].Stage != RoleJunked || detail.Roles[1].Reason != "Inside IR35 after all" {
		t.Fatalf("get_company should list the roles with the junked one's reason: %+v", detail.Roles)
	}
	_, err = s.AgentAddRole(o.ID, scout, AddRoleIn{Company: "Northwind Payments", Task: task.ID, RoleIn: RoleIn{
		Title: "Platform engineer (contract)", SourceURL: "https://northwind.example/jobs/2"}})
	var refused conflict
	if !errors.As(err, &refused) || !strings.Contains(err.Error(), "Inside IR35 after all") {
		t.Fatalf("a junked role added again should be refused with why: %v", err)
	}

	// Putting it back, then junking every role, rules the company out for the offering.
	if code, body := judge(plain.ID, map[string]any{"stage": "review", "reason": "oops"}); code != http.StatusBadRequest {
		t.Fatalf("a reason for a role not junked: %d %s", code, body)
	}
	if code, body := judge(plain.ID, map[string]any{"stage": "review"}); code != http.StatusOK || strings.Contains(body, "Inside IR35") {
		t.Fatalf("putting a junked role back: %d %s", code, body)
	}
	for _, id := range []int64{lead.ID, plain.ID} {
		if code, body := judge(id, map[string]any{"stage": "junked"}); code != http.StatusOK {
			t.Fatalf("junking: %d %s", code, body)
		}
	}
	if v := verdictFor(t, s, o.ID, roles.ID, north.ID); v.Stage != CompanyRejected || v.Reason != "every role junked" {
		t.Fatalf("every role junked, the company should be rejected for the offering: %+v", v)
	}
	code, body = asKey(t, "GET", base+"/companies/"+itoa(north.ID)+"/history", secret, nil)
	if code != http.StatusOK || !strings.Contains(body, "junked a role") || !strings.Contains(body, "added a role") {
		t.Fatalf("the company's history should say what was done to its roles: %d %s", code, body)
	}

	// A new role there is worth having, and brings the company back for review.
	fresh := must(s.AgentAddRole(o.ID, scout, AddRoleIn{Company: itoa(north.ID), Task: task.ID, RoleIn: RoleIn{
		Title: "AWS landing zone engineer", SourceURL: "https://northwind.example/jobs/3"}}))
	if fresh.Existed || fresh.Stage != RoleReview {
		t.Fatalf("a new role: %+v", fresh)
	}
	if v := verdictFor(t, s, o.ID, roles.ID, north.ID); v.Stage != CompanyReview || v.Reason != "" {
		t.Fatalf("a new role at a company whose roles were all junked should bring it back to review: %+v", v)
	}
}

func TestANewRoleAtACompanyRuledOutIsANewFindForTheTask(t *testing.T) {
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	roles := must(s.AddOffering(o.ID, Offering{Name: "Contract roles", HasRoles: true}, "Ada"))
	scout := Actor{Name: "agent:scout"}
	first := must(s.CreateTaskFor(o.ID, roles.ID, "", "Find contract roles", "Ada"))
	north := must(s.AgentAddCompany(o.ID, scout, AddCompanyIn{Name: "Northwind Payments", Task: first.ID}))
	old := must(s.AgentAddRole(o.ID, scout, AddRoleIn{Company: "Northwind Payments", Task: first.ID, RoleIn: RoleIn{
		Title: "Platform engineer", SourceURL: "https://northwind.example/jobs/2"}}))
	must(s.JudgeRole(o.ID, old.ID, RoleJunked, "", "Ada"))
	if err := s.SetTaskClosed(o.ID, first.ID, true, "Ada"); err != nil {
		t.Fatal(err)
	}

	next := must(s.CreateTaskFor(o.ID, roles.ID, "", "Find more contract roles", "Ada"))
	brief := must(s.AgentTask(o.ID, Actor{Name: "agent:scout", ReadOnly: true}, next.ID, PageIn{}))
	if brief.Offering == nil || !brief.Offering.HasRoles || !strings.Contains(brief.Offering.Guidance, "add_role") ||
		strings.Contains(brief.Offering.Guidance, "skip one rejected") {
		t.Fatalf("an agent on a task for an offering with roles should be told to add each role: %+v", brief.Offering)
	}
	must(s.AgentAddRole(o.ID, scout, AddRoleIn{Company: "Northwind Payments", Task: next.ID, RoleIn: RoleIn{
		Title: "AWS landing zone engineer", SourceURL: "https://northwind.example/jobs/3"}}))
	v := must(s.Task(o.ID, next.ID, time.Now()))
	if len(v.Companies) != 1 || v.Companies[0].ID != north.ID || len(v.LeftOut()) != 0 {
		t.Fatalf("a new role at a company whose roles were all junked should count as found, not left out: %+v left out %+v", v.Companies, v.LeftOut())
	}
}

func TestRolesAreKeptOnlyForAnOfferingThatHasThem(t *testing.T) {
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	cto := must(s.AddOffering(o.ID, Offering{Name: "Interim CTO"}, "Ada"))
	north := must(s.AgentAddCompany(o.ID, Actor{Name: "agent:scout"}, AddCompanyIn{Name: "Northwind Payments"}))
	role := RoleIn{Title: "Interim CTO, 6 months"}
	for _, offering := range []int64{0, cto.ID} {
		var refused conflict
		if _, err := s.AddRole(o.ID, offering, north.ID, role, "Ada"); !errors.As(err, &refused) {
			t.Fatalf("a role for offering %d, which has none: %v", offering, err)
		}
	}
	own := must(s.CreateTask(o.ID, "", "Find clients", "Ada"))
	if _, err := s.AgentAddRole(o.ID, Actor{Name: "agent:scout"}, AddRoleIn{Company: "Northwind Payments", Task: own.ID,
		RoleIn: RoleIn{Title: "Platform engineer", SourceURL: "https://northwind.example/jobs/2"}}); err == nil {
		t.Fatal("add_role on a task for the organisation's own")
	}

	must(s.ChangeOffering(o.ID, cto.ID, func(o *Offering) { o.HasRoles = true }, "Ada"))
	if _, err := s.AddRole(o.ID, cto.ID, north.ID, RoleIn{Title: "x", SourceURL: "not a link"}, "Ada"); err == nil {
		t.Fatal("a role whose posting isn't a link")
	}
	added := must(s.AddRole(o.ID, cto.ID, north.ID, role, "Ada"))
	if _, err := s.AddRole(o.ID, cto.ID, north.ID, RoleIn{Title: "interim cto, 6 MONTHS"}, "Ada"); err == nil {
		t.Fatal("the same role twice, by its title")
	}
	if v := verdictFor(t, s, o.ID, cto.ID, north.ID); v.Stage != CompanyReview {
		t.Fatalf("a role added by a person should put the company up for review for the offering: %+v", v)
	}
	var kept conflict
	if _, err := s.ChangeOffering(o.ID, cto.ID, func(o *Offering) { o.HasRoles = false }, "Ada"); !errors.As(err, &kept) {
		t.Fatalf("turning roles off while it has some: %v", err)
	}
	beta := mustOrg(t, s, "beta")
	if _, err := s.JudgeRole(beta.ID, added.ID, RolePursued, "", "Grace"); err == nil {
		t.Fatal("another organisation judged a role")
	}
	if err := s.RemoveOffering(o.ID, cto.ID, "Ada"); err != nil {
		t.Fatal(err)
	}
	if left := must(s.Roles(o.ID, north.ID, nil)); len(left) != 0 {
		t.Fatalf("removing an offering left its roles: %+v", left)
	}
}

func TestOfferingsFromBeforeRolesHaveNone(t *testing.T) {
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	must(s.AddOffering(o.ID, Offering{Name: "Interim CTO"}, "Ada"))
	if err := s.db.Migrator().DropColumn(&Offering{}, "has_roles"); err != nil {
		t.Fatal(err)
	}
	if err := s.db.AutoMigrate(&Offering{}); err != nil {
		t.Fatalf("adding the column to a table with rows: %v", err)
	}
	if got := must(s.Offerings(o.ID)); len(got) != 1 || got[0].HasRoles {
		t.Fatalf("an offering from before roles: %+v", got)
	}
}

func TestARoleAnAgentAddsMustCiteAPageItWasShown(t *testing.T) {
	got := sources("add_role", map[string]any{"title": "Platform engineer", "source_url": "https://northwind.example/jobs/2"})
	if len(got) != 1 || got[0] != "https://northwind.example/jobs/2" {
		t.Fatalf("add_role's posting should be checked against the pages the run has seen: %v", got)
	}
}
