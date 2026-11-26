package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/iotest"
)

func TestNULEscapesAreDroppedFromJSON(t *testing.T) {
	for in, want := range map[string]string{
		`{"name":"Acme\u0000 Boats"}`:      `{"name":"Acme Boats"}`,
		`{"name":"\u0000\u0000"}`:          `{"name":""}`,
		`{"path":"C:\\u0000"}`:             `{"path":"C:\\u0000"}`,
		`{"note":"say \"\u0000\" \u00e9"}`: `{"note":"say \"\" \u00e9"}`,
		`{"name":"Acme\u0001"}`:            `{"name":"Acme\u0001"}`,
		`{"cut":"\u00`:                     `{"cut":"\u00`,
		`{"a":"x\\","b":"\u0000y"}`:        `{"a":"x\\","b":"y"}`,
	} {
		// One byte at a time, so every escape is split across reads somewhere.
		r := &withoutNUL{src: io.NopCloser(iotest.OneByteReader(strings.NewReader(in)))}
		got, err := io.ReadAll(r)
		if err != nil || string(got) != want {
			t.Errorf("%s: %s, %v; want %s", in, got, err, want)
		}
	}
}

// Postgres won't keep a NUL or bytes that aren't UTF-8, so neither reaches it: what a
// person sends is kept without them, on both databases alike.
func TestTextArrivesWithoutWhatPostgresWontKeep(t *testing.T) {
	s, o, srv, client := signedIn(t)
	if code, body := call(t, client, "POST", srv.URL+"/o/alpha/api/v1/people", "application/json",
		json.RawMessage(`{"name":"Hannah\u0000 Pryce","company":"Larkmoor\u0000 Yachts"}`)); code != http.StatusCreated {
		t.Fatalf("adding someone whose name holds a NUL: %d %s", code, body)
	}
	c, err := s.FindOne(o.ID, "Hannah Pryce")
	if err != nil || c.Person == nil || *c.Person != "Hannah Pryce" || c.Maker == nil || *c.Maker != "Larkmoor Yachts" {
		t.Fatalf("kept as %+v, %v", c, err)
	}
	if code, body := call(t, client, "GET", srv.URL+"/o/alpha/api/v1/companies?q=Lark%00moor", "", nil); code != http.StatusOK || !strings.Contains(body, "Larkmoor Yachts") {
		t.Errorf("a search holding a NUL: %d", code)
	}
	if code, _ := call(t, client, "GET", srv.URL+"/o/alpha/api/v1/companies?q=Lark%FFmoor", "", nil); code != http.StatusOK {
		t.Errorf("a search holding a byte that isn't UTF-8: %d", code)
	}

	// A key holder's body is read as JSON whatever it says it is, so it is cleaned too.
	key, _, _ := s.CreateKey(o.ID, "bot", []string{"read", "research:write"}, "test")
	req, _ := http.NewRequest("POST", srv.URL+"/o/alpha/api/v1/companies", strings.NewReader(`{"name":"Quay\u0000 Marine"}`))
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/x-json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	var named int64
	s.in(o.ID).Model(&Company{}).Where("name = ?", "Quay Marine").Count(&named)
	if res.StatusCode >= 300 || named != 1 {
		t.Errorf("a key holder's company with a NUL, sent as application/x-json: %d, kept as named %d", res.StatusCode, named)
	}

	// A path holding a NUL names nothing here.
	if code, _ := call(t, client, "GET", srv.URL+"/o/alp%00ha/today", "", nil); code != http.StatusNotFound {
		t.Errorf("a path holding a NUL: %d", code)
	}

	// A form post: the sign-in page, with a NUL in the address.
	res, err = signInWith(http.DefaultClient, srv.URL, "ada@example.com\x00", "correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Errorf("signing in with a NUL after the address: %d", res.StatusCode)
	}
}
