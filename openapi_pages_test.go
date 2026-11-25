package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/shutthegoatup/prospector/api"
)

// Every list in the spec comes a page at a time, the same way: a list operation added
// later that answers a bare array, or forgets limit and cursor, fails here.
func TestEveryListInTheSpecComesInPages(t *testing.T) {
	spec, err := api.Spec()
	if err != nil {
		t.Fatal(err)
	}
	lists := 0
	for path, item := range spec.Paths.Map() {
		op := item.Get
		if op == nil {
			continue
		}
		ok := op.Responses.Status(http.StatusOK)
		if ok == nil || ok.Value.Content.Get("application/json") == nil {
			continue
		}
		schema := ok.Value.Content.Get("application/json").Schema.Value
		if schema.Type.Is("array") {
			t.Errorf("GET %s (%s) answers a bare list: answer {items, next_cursor} and take limit and cursor", path, op.OperationID)
			continue
		}
		items := schema.Properties["items"]
		if items == nil || !items.Value.Type.Is("array") {
			continue
		}
		lists++
		params := map[string]bool{}
		for _, p := range append(item.Parameters, op.Parameters...) {
			params[p.Value.Name] = true
		}
		if !params["limit"] || !params["cursor"] || params["page"] || schema.Properties["next_cursor"] == nil || ok.Value.Headers["Link"] == nil {
			t.Errorf("GET %s (%s) lists without the usual paging: limit and cursor in, next_cursor and a Link header out", path, op.OperationID)
		}
	}
	if lists < 7 {
		t.Fatalf("found only %d paged lists in the spec", lists)
	}
}

func TestAListLeftWholeComesOutAPageAtATime(t *testing.T) {
	all := []int{1, 2, 3, 4, 5}
	var got []int
	cursor, pages := "", 0
	for {
		page, next, err := listPage(all, 2, cursor)
		if err != nil {
			t.Fatal(err)
		}
		got, pages = append(got, page...), pages+1
		if next == "" {
			break
		}
		cursor = next
	}
	if fmt.Sprint(got) != fmt.Sprint(all) || pages != 3 {
		t.Fatalf("walked %v in %d pages", got, pages)
	}
	if page, next, err := listPage(all, 0, ""); err != nil || len(page) != 5 || next != "" {
		t.Fatalf("a list that fits the usual page: %v %q %v", page, next, err)
	}
	if _, _, err := listPage(all, 2, "nonsense"); err != errCursor {
		t.Fatalf("a made-up cursor: %v", err)
	}
	if page, next, _ := listPage(all, 2, cursorOf(99)); len(page) != 0 || next != "" {
		t.Fatalf("a cursor past the end: %v %q", page, next)
	}
}

// Following next_cursor, or the Link header, through a list reaches every row once.
func TestTheAPIsListsWalkToTheirEnd(t *testing.T) {
	s, o, srv, _ := signedIn(t)
	key, _, err := s.CreateKey(o.ID, "reader", []string{"read"}, "test")
	if err != nil {
		t.Fatal(err)
	}
	task := must(s.CreateTask(o.ID, "", "Find makers of garden rooms", "Nia"))
	for i := range 5 {
		if err := s.db.Create(&Agent{OrgID: o.ID, Name: fmt.Sprint("Agent ", i), Kind: kindOpenAI, Models: "[]", Tools: "[]", Allowed: "[]"}).Error; err != nil {
			t.Fatal(err)
		}
		if err := s.db.Create(&AgentRun{OrgID: o.ID, TaskID: task.ID, Status: "done", StartedBy: "Nia"}).Error; err != nil {
			t.Fatal(err)
		}
		must(s.AddEnquiry(o.ID, "form", EnquiryIn{Name: fmt.Sprint("Sender ", i), Email: fmt.Sprintf("sender%d@larkmoor.example", i), Message: "Could you show us?"}))
	}
	base := srv.URL + "/o/alpha/api/v1/"
	for _, list := range []string{"agents", fmt.Sprintf("tasks/%d/runs", task.ID), "enquiries"} {
		seen, pages := map[string]bool{}, 0
		byLink := base + list + "?limit=2"
		for byLink != "" {
			req, _ := http.NewRequest("GET", byLink, nil)
			req.Header.Set("Authorization", "Bearer "+key)
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			var body struct {
				Items      []map[string]any `json:"items"`
				NextCursor string           `json:"next_cursor"`
			}
			err = json.NewDecoder(res.Body).Decode(&body)
			res.Body.Close()
			if err != nil || res.StatusCode != http.StatusOK || len(body.Items) > 2 {
				t.Fatalf("%s: %d, %d items, %v", byLink, res.StatusCode, len(body.Items), err)
			}
			for _, item := range body.Items {
				id := fmt.Sprint(item["id"])
				if seen[id] {
					t.Fatalf("%s listed %s twice", list, id)
				}
				seen[id] = true
			}
			link, present := res.Header["Link"]
			if (body.NextCursor == "") == present {
				t.Fatalf("%s: next_cursor %q but Link %v", list, body.NextCursor, link)
			}
			byLink = ""
			if present {
				target, rel, _ := strings.Cut(link[0], ">; ")
				if rel != `rel="next"` || !strings.Contains(target, "limit=2") || !strings.Contains(target, "cursor="+body.NextCursor) {
					t.Fatalf("%s: a Link that isn't this list's next page: %q", list, link[0])
				}
				byLink = srv.URL + strings.TrimPrefix(target, "<")
			}
			if pages++; pages > 10 {
				t.Fatalf("%s never ended", list)
			}
		}
		if len(seen) != 5 || pages != 3 {
			t.Errorf("%s: %d rows over %d pages, want 5 over 3", list, len(seen), pages)
		}
	}
	for _, bad := range []string{"agents?cursor=nonsense", "enquiries?cursor=nonsense", "agents?limit=lots"} {
		req, _ := http.NewRequest("GET", base+bad, nil)
		req.Header.Set("Authorization", "Bearer "+key)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", bad, res.StatusCode)
		}
	}
}
