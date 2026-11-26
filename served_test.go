package main

import (
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// shown is what a page the server decided hands the app, read back from its shell.
type shown struct {
	Page    string              `json:"page"`
	State   string              `json:"state"`
	Token   string              `json:"token"`
	Org     string              `json:"org"`
	Email   string              `json:"email"`
	Done    bool                `json:"done"`
	Code    int                 `json:"code"`
	Title   string              `json:"title"`
	Message string              `json:"message"`
	Back    string              `json:"back"`
	Steps   []customerStepShown `json:"steps"`
}

var dataPage = regexp.MustCompile(`<div id="app" data-page="([^"]*)"></div>`)

func served(t *testing.T, body string) shown {
	t.Helper()
	var s shown
	m := dataPage.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no page in the shell: %.300s", body)
	}
	if err := json.Unmarshal([]byte(html.UnescapeString(m[1])), &s); err != nil {
		t.Fatalf("the shell's page: %v in %s", err, m[1])
	}
	return s
}

// progress is the customer's count of their steps done, as the page says it.
func (s shown) progress() string {
	done := 0
	for _, st := range s.Steps {
		if st.DoneOn != nil {
			done++
		}
	}
	return fmt.Sprintf("%d of %d", done, len(s.Steps))
}

func (s shown) toMark() (n int) {
	for _, st := range s.Steps {
		if st.DoneOn == nil {
			n++
		}
	}
	return n
}

func (s shown) step(id int64) *customerStepShown {
	for _, st := range s.Steps {
		if st.ID == id {
			return &st
		}
	}
	return nil
}

// undoable is whether the page offers to take back the mark on step id.
func (s shown) undoable(id int64) bool {
	st := s.step(id)
	return st != nil && st.DoneOn != nil && st.Mine
}

// Whatever the page carries stays inside its attribute, and the app's scripts are the
// only ones on it.
func TestAServedPageStaysInItsAttribute(t *testing.T) {
	w := httptest.NewRecorder()
	odd := `"></div><script>alert(1)</script><div a="&amp;`
	writeShell(w, http.StatusTeapot, problemShown{"problem", 418, odd, "it's <b>odd</b> & \"quoted\"", "/"}, "fallback")
	body := w.Body.String()
	if w.Code != http.StatusTeapot || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("%d %s", w.Code, w.Header().Get("Content-Type"))
	}
	if strings.Contains(body, "<script>alert") || strings.Contains(body, "<b>") || strings.Count(body, "<div") != 1 {
		t.Errorf("the page broke out of its attribute: %s", body)
	}
	if got := served(t, body); got.Title != odd || got.Message != "it's <b>odd</b> & \"quoted\"" || got.Code != 418 {
		t.Errorf("read back as %+v", got)
	}
}

// A customer's page says which steps they marked, never who else marked one.
func TestACustomersPageNamesNoMember(t *testing.T) {
	w := httptest.NewRecorder()
	on := "2026-11-20"
	a := &App{}
	a.customerRender(w, httptest.NewRequest("GET", "/c", nil), http.StatusOK, CustomerView{State: "steps", Org: "Alpha", Steps: []OnboardingStep{
		{ID: 1, Name: "Send files", DoneOn: &on, DoneBy: "Ada Lovelace"},
		{ID: 2, Name: "Approve renders", DoneOn: &on, DoneBy: customerActor},
	}})
	body := w.Body.String()
	if strings.Contains(body, "Ada") || strings.Contains(body, "done_by") {
		t.Errorf("the page names a member: %s", body)
	}
	if s := served(t, body); s.undoable(1) || !s.undoable(2) || s.progress() != "2 of 2" {
		t.Errorf("%+v", s)
	}
}
