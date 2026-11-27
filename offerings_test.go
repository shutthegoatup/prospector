package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/shutthegoatup/prospector/api"
	"github.com/shutthegoatup/prospector/internal/paging"
)

func dueNames(t *testing.T, s *Store, o *Org) map[string]bool {
	t.Helper()
	rows := must(s.Due(o.ID, "linkedin", time.Now(), o.Settings))
	out := map[string]bool{}
	for _, r := range rows {
		out[deref(r.First)] = true
	}
	return out
}

func TestACompanyIsJudgedForEachOfferingOnItsOwn(t *testing.T) {
	t.Parallel()
	s, o, srv, _ := signedIn(t)
	key := func(scopes ...string) string {
		secret, _, err := s.CreateKey(o.ID, "robot", scopes, "Ada Lovelace")
		if err != nil {
			t.Fatal(err)
		}
		return secret
	}
	owner, reviewer, reader := key("settings:write"), key("research:review"), key("read")
	base := srv.URL + "/o/alpha/api/v1"

	priya := must(s.QuickAddPerson(o, QuickAdd{Name: "Priya Shah", Company: "Northwind Payments"}, "Ada"))
	company := *priya.CompanyID

	code, body := asKey(t, "POST", base+"/offerings", owner, map[string]any{"name": "Interim CTO", "look_for": "trigger: a CTO leaving"})
	var cto api.Offering
	if json.Unmarshal([]byte(body), &cto); code != http.StatusCreated || cto.Id == 0 || cto.LookFor != "trigger: a CTO leaving" {
		t.Fatalf("adding an offering: %d %s", code, body)
	}
	if code, body := asKey(t, "POST", base+"/offerings", reviewer, map[string]any{"name": "Agency partners"}); code != http.StatusForbidden {
		t.Fatalf("a key that can't change settings added an offering: %d %s", code, body)
	}
	if code, body := asKey(t, "POST", base+"/offerings", owner, map[string]any{"name": "interim cto"}); code != http.StatusConflict {
		t.Fatalf("a second offering by the same name: %d %s", code, body)
	}
	if code, body := asKey(t, "POST", base+"/offerings", owner, map[string]any{"name": "Roles", "look_for": ": no name"}); code != http.StatusBadRequest {
		t.Fatalf("look-for fields that can't be read were saved: %d %s", code, body)
	}

	review := base + "/companies/" + itoa(company)
	if code, body := asKey(t, "PATCH", review, reviewer, map[string]any{"stage": "rejected", "offering": cto.Id, "reason": "CTO hired in September"}); code != http.StatusNoContent {
		t.Fatalf("rejecting for an offering: %d %s", code, body)
	}
	c := companyRow(t, s, o.ID, company)
	if c.Stage == CompanyRejected {
		t.Fatal("rejecting for one offering rejected the company for the organisation's own")
	}
	if !dueNames(t, s, o)["Priya"] {
		t.Fatal("rejecting for one offering took its people out of outreach for another")
	}
	// Agents read a company's history to learn what a person rejected, so a verdict for
	// one offering mustn't look like the company's own.
	var own int64
	if err := s.in(o.ID).Model(&Event{}).Where("company_id = ? and action like 'company:%'", company).Count(&own).Error; err != nil || own != 0 {
		t.Fatalf("a verdict for an offering was logged as the company's own: %d %v", own, err)
	}
	detail := must(s.AgentCompany(o.ID, Actor{Name: "agent:robot"}, "Northwind Payments", paging.Page{}))
	raw, _ := json.Marshal(detail)
	if said := string(raw); strings.Contains(said, `"company:rejected"`) || !strings.Contains(said, `"verdict:rejected"`) {
		t.Fatalf("what an agent is told about the company: %s", said)
	}

	code, body = asKey(t, "GET", review+"/verdicts", reader, nil)
	var stands struct{ Items []api.Verdict }
	if json.Unmarshal([]byte(body), &stands); code != http.StatusOK || len(stands.Items) != 1 || stands.Items[0].Reason != "CTO hired in September" ||
		stands.Items[0].Company != "Northwind Payments" || stands.Items[0].By != "agent:robot" {
		t.Fatalf("where the company stands: %d %s", code, body)
	}

	// What was ruled out can be looked at, and put back.
	code, body = asKey(t, "GET", base+"/offerings/"+itoa(cto.Id)+"/verdicts?stage=rejected", reader, nil)
	if code != http.StatusOK || !strings.Contains(body, "Northwind Payments") {
		t.Fatalf("listing what was rejected: %d %s", code, body)
	}
	if code, body := asKey(t, "PATCH", review, reviewer, map[string]any{"stage": "review", "offering": cto.Id}); code != http.StatusNoContent {
		t.Fatalf("putting it back: %d %s", code, body)
	}
	if _, body := asKey(t, "GET", base+"/offerings/"+itoa(cto.Id)+"/verdicts?stage=rejected", reader, nil); strings.Contains(body, "Northwind") {
		t.Fatalf("a company put back is still listed as rejected: %s", body)
	}
	if v := must(s.Verdicts(o.ID, company)); len(v) != 1 || v[0].Stage != CompanyReview || v[0].Reason != "" {
		t.Fatalf("putting it back should clear the reason: %+v", v)
	}

	if code, body := asKey(t, "PATCH", review, reviewer, map[string]any{"stage": "rejected", "reason": "why"}); code != http.StatusBadRequest {
		t.Fatalf("a reason for the organisation's own offering: %d %s", code, body)
	}
	if code, _ := asKey(t, "PATCH", review, reviewer, map[string]any{"stage": "qualified"}); code != http.StatusNoContent || companyRow(t, s, o.ID, company).Stage != CompanyQualified {
		t.Fatal("reviewing without an offering should still set the company's stage")
	}

	beta := mustOrg(t, s, "beta")
	theirs := must(s.AddOffering(beta.ID, Offering{Name: "Theirs"}, "Grace"))
	if code, body := asKey(t, "PATCH", review, reviewer, map[string]any{"stage": "rejected", "offering": theirs.ID}); code != http.StatusNotFound {
		t.Fatalf("a verdict for another organisation's offering: %d %s", code, body)
	}
	if code, _ := asKey(t, "GET", base+"/offerings/"+itoa(theirs.ID)+"/verdicts", reader, nil); code != http.StatusNotFound {
		t.Fatal("another organisation's verdicts were listed")
	}

	code, body = asKey(t, "PATCH", base+"/offerings/"+itoa(cto.Id), owner, map[string]any{"pitch": "Interim CTO for {company}."})
	if json.Unmarshal([]byte(body), &cto); code != http.StatusOK || cto.Pitch != "Interim CTO for {company}." || cto.Name != "Interim CTO" || cto.LookFor != "trigger: a CTO leaving" {
		t.Fatalf("changing only the pitch: %d %s", code, body)
	}

	ex := must(s.ExportOrg(o.ID, "Ada"))
	if len(ex.Offerings) != 1 || len(ex.Verdicts) != 1 {
		t.Fatalf("the export should carry offerings and verdicts: %d %d", len(ex.Offerings), len(ex.Verdicts))
	}

	if code, _ := asKey(t, "DELETE", base+"/offerings/"+itoa(cto.Id), owner, nil); code != http.StatusNoContent {
		t.Fatal("removing an offering")
	}
	if v := must(s.Verdicts(o.ID, company)); len(v) != 0 {
		t.Fatalf("removing an offering left its verdicts: %+v", v)
	}
	if code, body := asKey(t, "GET", base+"/offerings", reader, nil); code != http.StatusOK || strings.Contains(body, "Interim CTO") {
		t.Fatalf("a removed offering is still listed: %d %s", code, body)
	}
}

func companyRow(t *testing.T, s *Store, org, id int64) Company {
	t.Helper()
	var c Company
	if err := s.in(org).Where("id = ?", id).First(&c).Error; err != nil {
		t.Fatal(err)
	}
	return c
}
