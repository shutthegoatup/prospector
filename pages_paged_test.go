package main

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestTheEnquiriesPageHasPages(t *testing.T) {
	s, o, srv, client := signedIn(t)
	rows := make([]Enquiry, enquiryPageSize+1)
	for i := range rows {
		rows[i] = Enquiry{OrgID: o.ID, Kind: "demo", Name: fmt.Sprint("Sender ", i), Email: fmt.Sprintf("sender%d@larkmoor.example", i),
			Message: "Could you show us?", CreatedAt: time.Now().Add(time.Duration(i) * time.Second)}
	}
	if err := s.db.CreateInBatches(rows, 50).Error; err != nil {
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

	code, body := call(t, client, "GET", srv.URL+"/o/alpha/enquiries?kind=demo", "", nil)
	if code != http.StatusOK || !strings.Contains(body, "Page 1 of 2") || !strings.Contains(body, "enquiries?kind=demo&amp;page=2") {
		t.Fatalf("the page doesn't offer the next one with its filter kept: %d", code)
	}
	if strings.Contains(body, "sender0@larkmoor.example") {
		t.Fatal("the first page shows an enquiry that belongs on the second")
	}
	if _, body := call(t, client, "GET", srv.URL+"/o/alpha/enquiries?kind=demo&page=2", "", nil); !strings.Contains(body, "sender0@larkmoor.example") || !strings.Contains(body, "Page 2 of 2") {
		t.Fatal("the second page doesn't show the oldest enquiry")
	}
}
