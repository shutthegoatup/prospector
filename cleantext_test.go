package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
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
	if code, body := call(t, client, "POST", srv.URL+"/o/alpha/ui/add", "application/json",
		json.RawMessage(`{"name":"Hannah\u0000 Pryce","company":"Larkmoor\u0000 Yachts"}`)); code != http.StatusOK {
		t.Fatalf("adding someone whose name holds a NUL: %d %s", code, body)
	}
	c, err := s.FindOne(o.ID, "Hannah Pryce")
	if err != nil || c.Person == nil || *c.Person != "Hannah Pryce" || c.Maker == nil || *c.Maker != "Larkmoor Yachts" {
		t.Fatalf("kept as %+v, %v", c, err)
	}
	if code, body := call(t, client, "GET", srv.URL+"/o/alpha/companies?q=Lark%00moor", "", nil); code != http.StatusOK || !strings.Contains(body, "Larkmoor Yachts") {
		t.Errorf("a search holding a NUL: %d", code)
	}
	if code, _ := call(t, client, "GET", srv.URL+"/o/alpha/companies?q=Lark%FFmoor", "", nil); code != http.StatusOK {
		t.Errorf("a search holding a byte that isn't UTF-8: %d", code)
	}

	// A form post: the sign-in page, with a NUL in the address.
	res, err := http.PostForm(srv.URL+"/login", url.Values{"email": {"ada@example.com\x00"}, "password": {"correct horse battery"}})
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Errorf("signing in with a NUL after the address: %d", res.StatusCode)
	}
}
