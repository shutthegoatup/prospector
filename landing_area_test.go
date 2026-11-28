package main

import (
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/shutthegoatup/prospector/internal/identity"
)

func TestSomeoneInSupportOnlyStartsOnEnquiries(t *testing.T) {
	t.Parallel()
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
		{"grace@example.com", []string{identity.AreaSupport}, "/o/alpha/enquiries"},
		{"alan@example.com", []string{identity.AreaSales}, "/o/alpha/today"},
		{"barbara@example.com", []string{identity.AreaSales, identity.AreaSupport}, "/o/alpha/today"},
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
	// The switcher opens each organisation at /o/{slug}/, which sends its member on as above.
	if _, nav := call(t, ada, "GET", srv.URL+"/o/alpha/api/v1/nav", "", nil); !strings.Contains(nav, `"slug":"alpha"`) {
		t.Errorf("the nav doesn't list the organisations to switch to: %s", nav)
	}
	layout, err := os.ReadFile("web/src/components/app/layout.tsx")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(layout), "href={`/o/${o.slug}/`}") {
		t.Error("the organisation switcher doesn't open an organisation where its member starts")
	}
}
