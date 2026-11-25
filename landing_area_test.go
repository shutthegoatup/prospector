package main

import (
	"net/http"
	"strings"
	"testing"
)

func TestSomeoneInSupportOnlyStartsOnEnquiries(t *testing.T) {
	s, o, srv, ada := signedIn(t)
	starts := func(c *http.Client, path string) string {
		t.Helper()
		stay := &http.Client{Jar: c.Jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		res, err := stay.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.Header.Get("Location")
	}
	for _, who := range []struct {
		email string
		areas []string
		want  string
	}{
		{"grace@example.com", []string{areaSupport}, "/o/alpha/enquiries"},
		{"alan@example.com", []string{areaSales}, "/o/alpha/today"},
		{"barbara@example.com", []string{areaSales, areaSupport}, "/o/alpha/today"},
		{"carol@example.com", nil, "/o/alpha/today"},
	} {
		u := mustUser(t, s, o, who.email, who.email, "member")
		if err := s.SetAreas(o.ID, u.ID, who.areas); err != nil {
			t.Fatal(err)
		}
		c := signInAs(t, srv, who.email, "correct horse battery")
		for _, path := range []string{"/", "/o/alpha/"} {
			if got := starts(c, path); got != who.want {
				t.Errorf("%s with %v opening %s starts on %q, want %q", who.email, who.areas, path, got, who.want)
			}
		}
	}
	if got := starts(ada, "/"); got != "/o/alpha/today" {
		t.Errorf("an owner starts on %q", got)
	}
	if _, page := call(t, ada, "GET", srv.URL+"/o/alpha/companies", "", nil); !strings.Contains(page, `href="/o/alpha/"`) || strings.Contains(page, `href="/o/alpha/today" class="flex items-center gap-3`) {
		t.Error("the organisation switcher doesn't open an organisation where its member starts")
	}
}
