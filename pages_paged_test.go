package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestTheTasksPageHasPages(t *testing.T) {
	t.Parallel()
	s, o, srv, client := signedIn(t)
	rows := make([]Task, taskPageSize+2)
	for i := range rows {
		rows[i] = Task{OrgID: o.ID, Title: fmt.Sprint("Brief number ", i), CreatedBy: "Nia"}
	}
	if err := s.DB.CreateInBatches(rows, 50).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.SetTaskClosed(o.ID, rows[0].ID, true, "Nia"); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	open, pg, err := s.Tasks(o.ID, false, 1, now)
	if err != nil || len(open) != taskPageSize || pg.Total != taskPageSize+1 || pg.Pages() != 2 {
		t.Fatalf("the first page of open tasks: %d rows, %+v, %v", len(open), pg, err)
	}
	// With closed ones shown they come last, on the last page.
	last, pg, err := s.Tasks(o.ID, true, 2, now)
	if err != nil || len(last) != 2 || pg.Total != taskPageSize+2 || last[1].ID != rows[0].ID {
		t.Fatalf("the last page with closed tasks: %d rows, %+v, %v", len(last), pg, err)
	}

	// The app walks the same list through the API, with closed tasks still last.
	pages := apiPages(t, client, srv.URL+"/o/alpha/api/v1/tasks?closed=true&limit="+fmt.Sprint(taskPageSize))
	if len(pages) != 2 || len(pages[0].Items) != taskPageSize || len(pages[1].Items) != 2 || pages[1].Items[1]["title"] != "Brief number 0" {
		t.Fatalf("the tasks, a page at a time with closed ones: %d pages", len(pages))
	}
	// The counts on the chips are of every task, not of the page.
	var counts struct{ Open, All int }
	if _, body := call(t, client, "GET", srv.URL+"/o/alpha/api/v1/tasks?limit=1", "", nil); json.Unmarshal([]byte(body), &counts) != nil || counts.Open != taskPageSize+1 || counts.All != taskPageSize+2 {
		t.Fatalf("the list doesn't count every open task and every task: %+v", counts)
	}
}

// apiPage is one page of an API list, as the app reads it.
type apiPage struct {
	Items      []map[string]any `json:"items"`
	NextCursor string           `json:"next_cursor"`
}

// apiPages follows a list's next_cursor from its first page to its last.
func apiPages(t *testing.T, client *http.Client, u string) []apiPage {
	t.Helper()
	var out []apiPage
	next := ""
	for {
		at := u
		if next != "" {
			at += "&cursor=" + url.QueryEscape(next)
		}
		code, body := call(t, client, "GET", at, "", nil)
		var pg apiPage
		if code != http.StatusOK || json.Unmarshal([]byte(body), &pg) != nil {
			t.Fatalf("GET %s: %d %s", at, code, body)
		}
		out = append(out, pg)
		if pg.NextCursor == "" || len(out) > 50 {
			return out
		}
		next = pg.NextCursor
	}
}

func TestTheEnquiriesPageHasPages(t *testing.T) {
	t.Parallel()
	s, o, srv, client := signedIn(t)
	rows := make([]Enquiry, enquiryPageSize+1)
	for i := range rows {
		rows[i] = Enquiry{OrgID: o.ID, Kind: "demo", Name: fmt.Sprint("Sender ", i), Email: fmt.Sprintf("sender%d@larkmoor.example", i),
			Message: "Could you show us?", CreatedAt: time.Now().Add(time.Duration(i) * time.Second)}
	}
	if err := s.DB.CreateInBatches(rows, 50).Error; err != nil {
		t.Fatal(err)
	}
	first, pg, err := s.Enquiries(o.ID, EnquiryFilter{}, 1)
	if err != nil || len(first) != enquiryPageSize || pg.Pages() != 2 || pg.Total != len(rows) || first[0].Name != fmt.Sprint("Sender ", enquiryPageSize) {
		t.Fatalf("the first page: %d rows, %+v, newest %q, %v", len(first), pg, first[0].Name, err)
	}
	second, pg, err := s.Enquiries(o.ID, EnquiryFilter{}, 2)
	if err != nil || len(second) != 1 || pg.N != 2 || second[0].Name != "Sender 0" {
		t.Fatalf("the second page: %d rows, %+v, %v", len(second), pg, err)
	}
	// A page that is no longer there shows the last one, not nothing.
	if beyond, pg, err := s.Enquiries(o.ID, EnquiryFilter{}, 9); err != nil || len(beyond) != 1 || pg.N != 2 {
		t.Fatalf("a page past the end: %d rows, %+v, %v", len(beyond), pg, err)
	}
	if none, pg, err := s.Enquiries(o.ID, EnquiryFilter{Kind: "support"}, 1); err != nil || len(none) != 0 || pg.Pages() != 1 {
		t.Fatalf("a filter that matches nothing: %d rows, %+v, %v", len(none), pg, err)
	}

	// The app walks the same list through the API, its filter kept from page to page.
	pages := apiPages(t, client, srv.URL+"/o/alpha/api/v1/enquiries?kind=demo&limit="+fmt.Sprint(enquiryPageSize))
	if len(pages) != 2 || len(pages[0].Items) != enquiryPageSize || len(pages[1].Items) != 1 || pages[1].Items[0]["email"] != "sender0@larkmoor.example" {
		t.Fatalf("the enquiries, a page at a time: %d pages", len(pages))
	}
	for _, e := range pages[0].Items {
		if e["email"] == "sender0@larkmoor.example" {
			t.Fatal("the first page shows an enquiry that belongs on the second")
		}
	}
	if pages := apiPages(t, client, srv.URL+"/o/alpha/api/v1/enquiries?kind=support"); len(pages) != 1 || len(pages[0].Items) != 0 {
		t.Fatal("a filter that matches nothing should be one empty page")
	}
}
