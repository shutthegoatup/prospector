package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shutthegoatup/prospector/api"
)

// Find asks for about so many companies: the searches list shows how far it has got
// and how many wait for review, and agents are told the number.
func TestASearchAsksForAboutSoManyCompanies(t *testing.T) {
	s, o, srv, ada := signedIn(t)
	for _, want := range []int{-1, 501} {
		if code, body := call(t, ada, "POST", srv.URL+"/o/alpha/api/v1/tasks", "application/json",
			map[string]any{"title": "", "brief": "Yacht builders.", "want": want}); code != http.StatusBadRequest {
			t.Errorf("asking for %d companies: %d %s", want, code, body)
		}
	}
	code, body := call(t, ada, "POST", srv.URL+"/o/alpha/api/v1/tasks", "application/json",
		map[string]any{"title": "", "brief": "Yacht builders with a configurator.", "want": 3})
	var made api.TaskStarted
	if code != http.StatusCreated || json.Unmarshal([]byte(body), &made) != nil || made.Want != 3 {
		t.Fatalf("asking for about 3: %d %s", code, body)
	}

	scout := Actor{Name: "agent:scout", KeyID: 1}
	must(s.AgentAddCompany(o.ID, scout, AddCompanyIn{Name: "Larkmoor Yachts", Domain: "larkmoor.example", Task: made.Id}))
	kept := must(s.AgentAddCompany(o.ID, scout, AddCompanyIn{Name: "Halvard Marine", Domain: "halvard.example", Task: made.Id}))
	if err := s.SetCompanyStage(o.ID, kept.ID, CompanyQualified, "A", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}

	_, list := call(t, ada, "GET", srv.URL+"/o/alpha/api/v1/tasks", "", nil)
	var tasks api.ListTasks200JSONResponse
	if err := json.Unmarshal([]byte(list), &tasks.Body); err != nil || len(tasks.Body.Items) != 1 {
		t.Fatalf("the searches: %v %s", err, list)
	}
	got := tasks.Body.Items[0]
	if got.Want != 3 || got.Found.Companies != 2 || got.ToReview != 1 {
		t.Errorf("the search should be 2 of about 3, 1 to review: %s", list)
	}

	brief := must(s.AgentTask(o.ID, scout, made.Id, PageIn{}))
	if brief.Want != 3 || brief.Review != 1 {
		t.Errorf("an agent should be told the number and what waits: want %d, to review %d", brief.Want, brief.Review)
	}

	// Without a number, a search gives agents none.
	code, body = call(t, ada, "POST", srv.URL+"/o/alpha/api/v1/tasks", "application/json", map[string]any{"title": "", "brief": "Caravan makers."})
	var plain api.TaskStarted
	if code != http.StatusCreated || json.Unmarshal([]byte(body), &plain) != nil || plain.Want != 0 {
		t.Fatalf("a search with no number: %d %s", code, body)
	}
	if out, _ := json.Marshal(must(s.AgentTask(o.ID, scout, plain.Id, PageIn{})).TaskBrief); strings.Contains(string(out), `"want"`) {
		t.Errorf("an agent is given a number nobody asked for: %s", out)
	}
}

// A database from before searches asked for a number has tasks, and they ask for none.
func TestTasksFromBeforeANumberAskForNone(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "old.sqlite")
	if pg := os.Getenv("PROSPECTOR_TEST_PG"); pg != "" {
		dsn = pg
	}
	old := must(Open(dsn))
	if err := old.db.Migrator().DropTable(models...); err != nil {
		t.Fatal(err)
	}
	if err := old.db.AutoMigrate(models...); err != nil {
		t.Fatal(err)
	}
	o := mustOrg(t, old, "alpha")
	task := must(old.CreateTask(o.ID, "", "Yacht builders.", "Ada Lovelace"))
	if err := old.db.Migrator().DropColumn(&Task{}, "want"); err != nil {
		t.Fatal(err)
	}

	s := must(Open(dsn))
	if got := must(s.taskRow(o.ID, task.ID)); got.Want != 0 {
		t.Fatalf("a task from before should ask for no number: %+v", got)
	}
}
