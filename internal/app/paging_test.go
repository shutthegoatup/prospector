package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/shutthegoatup/prospector/internal/crm"
	"github.com/shutthegoatup/prospector/internal/paging"
	"github.com/shutthegoatup/prospector/internal/request"
	"github.com/shutthegoatup/prospector/internal/research"
	"github.com/shutthegoatup/prospector/internal/store"
)

// firstOf drops the middle of three results, for must.
func firstOf[T, U any](v T, _ U, err error) (T, error) { return v, err }

// walk follows next cursors to the end of a list, and fails if it never ends.
func walk[T any](t *testing.T, limit int, page func(paging.Page) ([]T, string, error)) (all []T, pages int) {
	t.Helper()
	in := paging.Page{Limit: limit}
	for {
		got, next, err := page(in)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) > limit*2 {
			t.Fatalf("a page of %d held %d", limit, len(got))
		}
		all, pages = append(all, got...), pages+1
		if next == "" {
			return all, pages
		}
		if pages > 100 {
			t.Fatal("the list never ended")
		}
		in.Cursor = next
	}
}

func TestEveryListAnAgentReadsComesInPages(t *testing.T) {
	t.Parallel()
	s, o, srv, _ := signedIn(t)
	scout := store.Actor{Name: "agent:scout", KeyID: 1}
	for i := range 5 {
		co := must(s.AgentAddCompany(o.ID, scout, research.AddCompanyIn{Name: fmt.Sprintf("Larkmoor Rooms %d", i)}))
		must(s.QuickAddPerson(o, crm.QuickAdd{Name: fmt.Sprintf("Quillon V%cle", rune(0x61+i)), Company: co.Name}, "Nia"))
		must(s.CreateTask(o.ID, "", fmt.Sprint("Find makers of garden rooms, part ", i), "Nia"))
	}

	hits, pages := walk(t, 2, func(in paging.Page) ([]research.SearchHit, string, error) {
		return s.AgentSearch(o.ID, scout, "larkmoor", in)
	})
	seen := map[string]int{}
	for _, h := range hits {
		seen[fmt.Sprint(h.Kind, h.ID)]++
	}
	if len(hits) != 10 || len(seen) != 10 || pages != 3 {
		t.Fatalf("5 companies and their 5 people, two of each a page: %d hits, %d different, %d pages", len(hits), len(seen), pages)
	}
	if all, next, err := s.AgentSearch(o.ID, scout, "larkmoor", paging.Page{}); err != nil || len(all) != 10 || next != "" {
		t.Fatalf("a search that fits one page: %d hits, next %q, %v", len(all), next, err)
	}
	if _, _, err := s.AgentSearch(o.ID, scout, "larkmoor", paging.Page{Cursor: "nonsense"}); err != paging.ErrCursor {
		t.Fatalf("a made-up cursor: %v", err)
	}

	tasks, pages := walk(t, 2, func(in paging.Page) ([]research.TaskBrief, string, error) { return s.AgentTasks(o.ID, scout, in) })
	if len(tasks) != 5 || pages != 3 {
		t.Fatalf("5 tasks two a page: %d in %d pages", len(tasks), pages)
	}
	for i := 1; i < len(tasks); i++ {
		if tasks[i].ID >= tasks[i-1].ID {
			t.Fatalf("tasks aren't newest first without repeats: %d after %d", tasks[i].ID, tasks[i-1].ID)
		}
	}
	// One opened between two pages doesn't push an earlier one onto the next page.
	firstPage, next, err := s.AgentTasks(o.ID, scout, paging.Page{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	must(s.CreateTask(o.ID, "", "Opened meanwhile", "Nia"))
	second, _, err := s.AgentTasks(o.ID, scout, paging.Page{Limit: 2, Cursor: next})
	if err != nil || len(second) != 2 || second[0].ID >= firstPage[1].ID {
		t.Fatalf("the page after a task was opened: %+v %v", second, err)
	}

	due, pages := walk(t, 2, func(in paging.Page) ([]research.DueItem, string, error) {
		out, err := s.AgentDue(o, in)
		if err != nil {
			return nil, "", err
		}
		return out.Items, out.NextCursor, nil
	})
	if len(due) != 5 || pages != 3 {
		t.Fatalf("5 people due two a page: %d in %d pages", len(due), pages)
	}

	// Over HTTP the next page is also named in a Link header, and following it ends.
	key, _, err := s.CreateKey(o.ID, "bot", []string{"read"}, "test")
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]int{"search?q=larkmoor&limit=2": 10, "tasks?limit=4": 6, "due?limit=3": 5} {
		url, got, fetched := "/o/alpha/api/v1/"+path, 0, 0
		for url != "" {
			req, _ := http.NewRequest("GET", srv.URL+url, nil)
			req.Header.Set("Authorization", "Bearer "+key)
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			var body struct {
				Hits, Tasks, Items []json.RawMessage
				NextCursor         string `json:"next_cursor"`
			}
			err = json.NewDecoder(res.Body).Decode(&body)
			res.Body.Close()
			if err != nil || res.StatusCode != http.StatusOK {
				t.Fatalf("%s: %d %v", url, res.StatusCode, err)
			}
			got += len(body.Hits) + len(body.Tasks) + len(body.Items)
			link := res.Header.Get("Link")
			if (link == "") != (body.NextCursor == "") {
				t.Fatalf("%s: Link %q and next_cursor %q disagree about whether there is more", url, link, body.NextCursor)
			}
			url = ""
			if link != "" {
				target, rel, _ := strings.Cut(link, ">; ")
				if rel != `rel="next"` || !strings.HasPrefix(target, "</o/alpha/api/v1/") || !strings.Contains(target, "limit=") {
					t.Fatalf("a Link that isn't the next page of this list: %q", link)
				}
				url = strings.TrimPrefix(target, "<")
			}
			if fetched++; fetched > 20 {
				t.Fatalf("%s never ended", path)
			}
		}
		if got != want {
			t.Errorf("%s: %d in all over %d pages, want %d", path, got, fetched, want)
		}
	}
	for path, says := range map[string]string{"tasks?limit=-1": "limit", "tasks?limit=lots": "limit", "tasks?cursor=nonsense": "cursor"} {
		req, _ := http.NewRequest("GET", srv.URL+"/o/alpha/api/v1/"+path, nil)
		req.Header.Set("Authorization", "Bearer "+key)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var p request.Problem
		json.NewDecoder(res.Body).Decode(&p)
		res.Body.Close()
		if res.StatusCode != http.StatusBadRequest || !strings.Contains(p.Detail, says) {
			t.Errorf("%s: %d %q", path, res.StatusCode, p.Detail)
		}
	}
}
