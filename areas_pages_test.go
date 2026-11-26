package main

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/shutthegoatup/prospector/api"
)

// The app offers each area's work, and marks the sidebar's pages "view", by the nav's
// sales and support flags. Hiding another area's controls is only worth having if those
// flags say exactly what the API lets each person do.
func TestTheNavOffersOnlyWorkTheAPIAllows(t *testing.T) {
	s, o, srv, owner := signedIn(t)
	c := repliedPerson(t, s, o, "Hannah Pryce")
	task := must(s.CreateTask(o.ID, "Yachts", "Yacht builders with a configurator.", "Ada Lovelace"))
	base := srv.URL + "/o/alpha/"
	pages := append([]string{"companies/" + itoa(*c.CompanyID), "tasks/" + itoa(task.ID), "add?popup=1&name=Hannah", "today?step=linkedin"}, appPages...)

	// The area is checked before any row is looked for, so aiming at none changes nothing.
	sales := []string{"PATCH api/v1/contacts/999999", "PATCH api/v1/deals/999999", "POST api/v1/tasks/999999/requests", "DELETE api/v1/suppressions/999999"}
	support := []string{"PATCH api/v1/enquiries/999999", "POST api/v1/enquiries/999999/answers", "PATCH api/v1/subscriptions/999999", "PATCH api/v1/onboarding-steps/999999"}
	refused := func(cl *http.Client, op string) bool {
		method, path, _ := strings.Cut(op, " ")
		code, body := call(t, cl, method, base+path, "application/json", json.RawMessage(`{}`))
		return code == http.StatusForbidden && strings.Contains(body, "Team list")
	}

	for _, who := range []struct {
		email string
		areas []string
	}{
		{"ada@example.com", nil},
		{"grace@example.com", []string{areaSupport}},
		{"alan@example.com", []string{areaSales}},
		{"carol@example.com", nil},
	} {
		cl := owner
		if who.email != "ada@example.com" {
			u := mustUser(t, s, o, who.email, who.email, "member")
			if err := s.SetAreas(o.ID, u.ID, who.areas); err != nil {
				t.Fatal(err)
			}
			cl = signInAs(t, srv, who.email, "correct horse battery")
		}
		for _, p := range pages {
			if code, page := call(t, cl, "GET", base+p, "", nil); code != http.StatusOK || !strings.Contains(page, `<div id="app"></div>`) {
				t.Fatalf("%s, %s: %d", who.email, p, code)
			}
		}
		code, body := call(t, cl, "GET", base+"api/v1/nav", "", nil)
		var n api.Nav
		if code != http.StatusOK || json.Unmarshal([]byte(body), &n) != nil {
			t.Fatalf("%s's nav: %d %s", who.email, code, body)
		}
		for _, op := range sales {
			if refused(cl, op) == n.Sales {
				t.Errorf("%s: the nav says sales %v, and %s is refused %v", who.email, n.Sales, op, refused(cl, op))
			}
		}
		for _, op := range support {
			if refused(cl, op) == n.Support {
				t.Errorf("%s: the nav says support %v, and %s is refused %v", who.email, n.Support, op, refused(cl, op))
			}
		}
	}

	// The sidebar marks the same pages as the server's pageArea says are each area's work.
	layout, err := os.ReadFile("web/src/components/app/layout.tsx")
	if err != nil {
		t.Fatal(err)
	}
	for page, area := range pageArea {
		if !strings.Contains(string(layout), page+`: "`+area+`",`) {
			t.Errorf("the sidebar doesn't say %s is %s work", page, area)
		}
	}
}

// pageArea is the area whose work a page in the sidebar is.
var pageArea = map[string]string{
	"today": areaSales, "research": areaSales, "conversations": areaSales, "campaigns": areaSales, "tasks": areaSales,
	"enquiries": areaSupport, "onboarding": areaSupport, "subscriptions": areaSupport,
}
