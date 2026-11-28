package app

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/shutthegoatup/prospector/api"
	"github.com/shutthegoatup/prospector/internal/crm"
	"github.com/shutthegoatup/prospector/internal/identity"
	"github.com/shutthegoatup/prospector/internal/research"
	"github.com/shutthegoatup/prospector/internal/store"
)

type todayPage struct {
	Items      []api.DueCard  `json:"items"`
	Counts     map[string]int `json:"counts"`
	Total      int            `json:"total"`
	Shown      int            `json:"shown"`
	LaterCalls int            `json:"later_calls"`
	NextCursor string         `json:"next_cursor"`
}

func readToday(t *testing.T, client *http.Client, u string) todayPage {
	t.Helper()
	code, body := call(t, client, "GET", u, "", nil)
	var page todayPage
	if err := json.Unmarshal([]byte(body), &page); code != http.StatusOK || err != nil {
		t.Fatalf("GET %s: %d %s", u, code, body)
	}
	return page
}

func cardOf(t *testing.T, page todayPage, step string) api.DueCard {
	t.Helper()
	for _, c := range page.Items {
		if string(c.Step) == step {
			return c
		}
	}
	t.Fatalf("no %s card in %+v", step, page.Items)
	return api.DueCard{}
}

func TestTodaysQueueIsInTheAPI(t *testing.T) {
	t.Parallel()
	s, o, srv, client := signedIn(t)
	ada := must(s.UserByEmail("ada@example.com"))
	if err := s.SaveVoice(o.ID, ada.ID, "Ada Lovelace", "Ada\nAlpha Ltd"); err != nil {
		t.Fatal(err)
	}
	hannah := must(s.QuickAddPerson(o, crm.QuickAdd{Name: "Hannah Pryce", Company: "Larkmoor Yachts", Linkedin: "https://www.linkedin.com/in/hannah-pryce"}, "Ada Lovelace"))
	tobias := must(s.QuickAddPerson(o, crm.QuickAdd{Name: "Tobias Wren", Company: "Fjordline Saunas"}, "Ada Lovelace"))
	if err := s.Update(o.ID, tobias.ID, "body", "Hi Tobias, a short note about your configurator.", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	added := time.Now().AddDate(0, 0, -10).Format("2006-01-02")
	if err := s.AgentMark(o.ID, store.Actor{Name: "agent:scout"}, research.MarkIn{ContactID: tobias.ID, Step: "linkedin", Date: added}); err != nil {
		t.Fatal(err)
	}
	iris := must(s.QuickAddPerson(o, crm.QuickAdd{Name: "Iris Vane", Company: "Corvane Boats"}, "Ada Lovelace"))
	if err := s.Update(o.ID, iris.ID, "phone", "+41 44 668 1800", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	repliedPerson(t, s, o, "Nia Brook")
	base := srv.URL + "/o/alpha/api/v1/"

	all := readToday(t, client, base+"today")
	if all.Total != 5 || all.Shown != 5 || all.Counts["linkedin"] != 2 || all.Counts["email"] != 1 || all.Counts["call"] != 1 || all.Counts["next"] != 1 {
		t.Fatalf("what's due: total %d shown %d %v", all.Total, all.Shown, all.Counts)
	}
	if all.Items[0].Step != "next" {
		t.Errorf("a next step should come first, got %s", all.Items[0].Step)
	}
	add := cardOf(t, all, "linkedin")
	if !strings.Contains(add.Text, strings.Fields(add.Name)[0]) || add.OpenLabel != "Open + copy" && add.OpenLabel != "Search + copy" {
		t.Errorf("a LinkedIn add should carry the templated note and how to open them: %+v", add)
	}
	email := cardOf(t, all, "email")
	if email.Copy != email.Text+"\n\nAda\nAlpha Ltd" || !strings.Contains(email.Meta, "added "+added+" (agent · scout)") {
		t.Errorf("an email is copied signed, and says the agent marked the add: %q / %q", email.Copy, email.Meta)
	}
	ring := cardOf(t, all, "call")
	if ring.Number == "" || ring.Tel != "+41446681800" || len(ring.Script) == 0 || !strings.Contains(ring.Script[0].Say, "it's Ada from Alpha") {
		t.Errorf("a call carries its number and a script in the caller's name: %+v", ring)
	}
	if next := cardOf(t, all, "next"); !strings.Contains(next.Meta, "no next step yet") {
		t.Errorf("a next step says where the conversation stands: %q", next.Meta)
	}

	adds := readToday(t, client, base+"today?step=linkedin")
	if adds.Shown != 2 || adds.Total != 5 || len(adds.Items) != 2 || adds.Items[0].Next != adds.Items[1].Id {
		t.Fatalf("the LinkedIn filter: %+v", adds)
	}
	first := readToday(t, client, base+"today?limit=3")
	rest := readToday(t, client, base+"today?limit=3&cursor="+first.NextCursor)
	if len(first.Items) != 3 || first.NextCursor == "" || len(rest.Items) != 2 || rest.NextCursor != "" {
		t.Fatalf("paging: %d then %d", len(first.Items), len(rest.Items))
	}
	if code, _ := call(t, client, "GET", base+"today?step=nonsense", "", nil); code != http.StatusBadRequest {
		t.Errorf("an unknown step: %d", code)
	}

	code, body := call(t, client, "GET", base+"today/summary", "", nil)
	var summary api.TodaySummary
	if err := json.Unmarshal([]byte(body), &summary); code != http.StatusOK || err != nil || summary.Week.Replies != 1 {
		t.Fatalf("the summary: %d %s", code, body)
	}

	// A key reads it too, though it has no name to call in.
	reader, _ := apiKey(t, s, o, "reader", "read")
	code, body = asKey(t, "GET", base+"today?step=call", reader, nil)
	if code != http.StatusOK || !strings.Contains(body, "[YOUR NAME]") {
		t.Fatalf("a key reading the queue: %d %s", code, body)
	}
	if code, body := asKey(t, "GET", base+"due", reader, nil); code != http.StatusOK || !strings.Contains(body, "Tobias Wren") {
		t.Fatalf("the older due list should still answer: %d %s", code, body)
	}

	// A member marks the add done, and it leaves the queue.
	if code, body := call(t, client, "POST", base+"marks", "application/json", map[string]any{"contact_id": hannah.ID, "step": "linkedin"}); code != http.StatusOK || !strings.Contains(body, `"ok":true`) {
		t.Fatalf("marking: %d %s", code, body)
	}
	for _, c := range readToday(t, client, base+"today?step=linkedin").Items {
		if c.Id == hannah.ID {
			t.Fatal("a marked add is still due")
		}
	}
	var e store.Event
	if err := s.DB.Where("contact_id = ? and action = ?", hannah.ID, "mark:linkedin").First(&e).Error; err != nil || e.User != "Ada Lovelace" || e.KeyID != 0 {
		t.Fatalf("the mark's event: %+v %v", e, err)
	}
}

func TestMarkingIsRefusedToWhoeverMayNot(t *testing.T) {
	t.Parallel()
	s, o, srv, client := signedIn(t)
	c := must(s.QuickAddPerson(o, crm.QuickAdd{Name: "Hannah Pryce", Company: "Larkmoor Yachts"}, "Ada Lovelace"))
	marks := srv.URL + "/o/alpha/api/v1/marks"
	mark := map[string]any{"contact_id": c.ID, "step": "linkedin"}

	if code, _ := call(t, client, "POST", marks, "text/plain", mark); code != http.StatusUnsupportedMediaType {
		t.Errorf("a non-JSON mark: %d", code)
	}
	if code, _ := call(t, http.DefaultClient, "POST", marks, "application/json", mark); code != http.StatusUnauthorized {
		t.Errorf("signed out: %d", code)
	}
	if code, _ := call(t, http.DefaultClient, "GET", srv.URL+"/o/alpha/api/v1/today", "", nil); code != http.StatusUnauthorized {
		t.Errorf("reading signed out: %d", code)
	}
	if code, _ := call(t, client, "POST", marks, "application/json", map[string]any{"contact_id": c.ID, "step": "waved"}); code == http.StatusOK {
		t.Error("a step that isn't one was marked")
	}
	if code, _ := call(t, client, "POST", marks, "application/json", map[string]any{"contact_id": c.ID, "step": "linkedin", "date": "2999-01-01"}); code == http.StatusOK {
		t.Error("a mark in the future was taken")
	}

	u := mustUser(t, s, o, "grace@example.com", "Grace Hopper", "member")
	if err := s.SetAreas(o.ID, u.ID, []string{identity.AreaSupport}); err != nil {
		t.Fatal(err)
	}
	grace := signInAs(t, srv, "grace@example.com", "correct horse battery")
	if code, body := call(t, grace, "POST", marks, "application/json", mark); code != http.StatusForbidden || !strings.Contains(body, "Team list") {
		t.Errorf("someone without sales marking: %d %s", code, body)
	}
	if page := readToday(t, grace, srv.URL+"/o/alpha/api/v1/today"); page.Total != 1 {
		t.Errorf("someone without sales still reads Today: %+v", page)
	}

	reader, _ := apiKey(t, s, o, "reader", "read")
	if code, _ := asKey(t, "POST", marks, reader, mark); code != http.StatusForbidden {
		t.Errorf("a key without outcomes:write marking: %d", code)
	}
	writer, k := apiKey(t, s, o, "scout", "outcomes:write")
	day := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	if code, body := asKey(t, "POST", marks, writer, map[string]any{"contact_id": c.ID, "step": "linkedin", "date": day, "note": "Ada said she sent it"}); code != http.StatusOK {
		t.Fatalf("a key marking: %d %s", code, body)
	}
	var e store.Event
	if err := s.DB.Where("contact_id = ? and action = ?", c.ID, "mark:linkedin").First(&e).Error; err != nil || e.User != "agent:scout" || e.KeyID != k.ID || store.Deref(e.Value) != day+" · Ada said she sent it" {
		t.Fatalf("a key's mark should be the agent's, with its date and note: %+v %v", e, err)
	}
}

func apiKey(t *testing.T, s *Store, o *store.Org, name string, scopes ...string) (string, *store.APIKey) {
	t.Helper()
	secret, k, err := s.CreateKey(o.ID, name, scopes, "test")
	if err != nil {
		t.Fatal(err)
	}
	return secret, k
}
