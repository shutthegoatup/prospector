package app

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/shutthegoatup/prospector/internal/crm"
	"github.com/shutthegoatup/prospector/internal/store"
)

// The writes an agent makes with a key: every field it sends lands, the answer says what
// it always has, any content type is taken, and whom they refuse.
func TestAnAgentsWritesAnswerAsTheyAlwaysHave(t *testing.T) {
	t.Parallel()
	s, o, srv, client := signedIn(t)
	writer, _, err := s.CreateKey(o.ID, "writer", []string{"read", "research:write", "drafts:write", "enquiries:write"}, "test")
	if err != nil {
		t.Fatal(err)
	}
	reader, _, err := s.CreateKey(o.ID, "reader", []string{"read"}, "test")
	if err != nil {
		t.Fatal(err)
	}
	post := func(key, path, contentType, body string) (int, string) {
		t.Helper()
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/o/alpha/api/v1/"+path, strings.NewReader(body))
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		if key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(b)
	}
	keys := func(body string) map[string]any {
		t.Helper()
		var m map[string]any
		if err := json.Unmarshal([]byte(body), &m); err != nil {
			t.Fatalf("not JSON: %s", body)
		}
		return m
	}

	code, body := post(writer, "companies", "application/json", `{"name":"Larkmoor Yachts","domain":"larkmoor.example","country":"GB","segment":"day cruisers","fit":"A","why":"Builds to order.","facts":[{"field":"configurator","value":"Day cruiser builder","source_url":"https://larkmoor.example/build","answer":"no","inferred":true}]}`)
	added := keys(body)
	if code != http.StatusOK || added["name"] != "Larkmoor Yachts" || added["existed"] != false || added["stage"] == nil || added["id"] == nil {
		t.Fatalf("adding a company: %d %s", code, body)
	}
	lark := must(s.CompanyRef(o.ID, "Larkmoor Yachts"))
	if lark.Domain != "larkmoor.example" || lark.Country != "GB" || lark.Segment != "day cruisers" || lark.Fit != "A" || lark.Why != "Builds to order." {
		t.Errorf("a company's fields didn't all land: %+v", lark)
	}
	facts := must(s.Facts(o.ID, lark.ID))
	if len(facts) != 1 || facts[0].Answer != "no" || !facts[0].Inferred || facts[0].SourceURL != "https://larkmoor.example/build" {
		t.Errorf("a company's fact didn't land whole: %+v", facts)
	}

	// No content type at all, as some agents send.
	code, body = post(writer, "facts", "", `{"company":"Larkmoor Yachts","field":"press","value":"Won a boat show prize.","source_url":"https://larkmoor.example/news"}`)
	if fact := keys(body); code != http.StatusOK || fact["existed"] != false || fact["field"] != "press" || fact["company_id"] == nil {
		t.Fatalf("adding a fact: %d %s", code, body)
	}
	if code, body = post(writer, "facts", "text/plain", `{"company":"Larkmoor Yachts","field":"press","value":"Won a boat show prize.","source_url":"https://larkmoor.example/news"}`); code != http.StatusOK || keys(body)["existed"] != true {
		t.Errorf("the same fact again: %d %s", code, body)
	}

	code, body = post(writer, "contacts", "application/json", `{"company":"Larkmoor Yachts","name":"Hannah Pryce","title":"Head of Marketing","email":"hannah@larkmoor.example","email_status":"verified","linkedin":"https://www.linkedin.com/in/hannah-pryce-example","phone":"+44 20 7946 0000","source_url":"https://larkmoor.example/team"}`)
	person := keys(body)
	if code != http.StatusOK || person["existed"] != false || person["status"] != "suggested" || person["id"] == nil {
		t.Fatalf("suggesting a person: %d %s", code, body)
	}
	if _, has := person["suggested"]; has {
		t.Errorf("someone new carries suggested, which older clients never saw: %s", body)
	}
	hannah := must(s.ContactByID(o.ID, int64(person["id"].(float64))))
	if store.Deref(hannah.Email) != "hannah@larkmoor.example" || store.Deref(hannah.Linkedin) == "" || store.Deref(hannah.Phone) == "" || hannah.Title != "Head of Marketing" {
		t.Errorf("a person's fields didn't all land: %+v", hannah)
	}

	ines := must(s.QuickAddPerson(o, crm.QuickAdd{Name: "Ines Moreau", Company: "Belair Kitchens"}, "Ada Lovelace"))
	code, body = post(writer, "drafts", "application/json", `{"contact_id":`+jsonInt(ines.ID)+`,"kind":"email","subject":"Your kitchens","text":"Hello Ines."}`)
	if code != http.StatusOK || strings.TrimSpace(body) != `{"ok":true}` {
		t.Fatalf("drafting: %d %s", code, body)
	}
	if drafted := must(s.ContactByID(o.ID, ines.ID)); store.Deref(drafted.Body) != "Hello Ines." || store.Deref(drafted.Subject) != "Your kitchens" {
		t.Errorf("a draft didn't land whole: body %q subject %q", store.Deref(drafted.Body), store.Deref(drafted.Subject))
	}

	code, body = post(writer, "enquiries", "application/json", `{"kind":"signup","name":"Omar Reyes","email":"omar@reef.example","company":"Reef Boats","phone":"+44 20 7946 0001","message":"Hello.","page":"/pricing","site":"app","trap":"filled"}`)
	enquiry := keys(body)
	if code != http.StatusOK || enquiry["spam"] != false || enquiry["phone"] != "+44 20 7946 0001" || enquiry["page"] != "/pricing" {
		t.Fatalf("an enquiry by key: %d %s", code, body)
	}
	for _, field := range []string{"id", "kind", "name", "email", "company", "message", "site", "via", "contact_id", "company_id", "created_at", "state", "owner", "note"} {
		if _, has := enquiry[field]; !has {
			t.Errorf("an enquiry by key lacks %s: %s", field, body)
		}
	}

	if code, body = post(writer, "facts", "application/json", "{"); code != http.StatusBadRequest || !strings.Contains(body, `"detail"`) {
		t.Errorf("a body that isn't JSON: %d %s", code, body)
	}
	for path, scope := range map[string]string{"companies": "research:write", "facts": "research:write", "contacts": "research:write", "drafts": "drafts:write", "enquiries": "enquiries:write"} {
		if code, _ := post("", path, "application/json", "{}"); code != http.StatusUnauthorized {
			t.Errorf("POST %s with no key: %d", path, code)
		}
		if code, body := post(reader, path, "application/json", "{}"); code != http.StatusForbidden || !strings.Contains(body, scope) {
			t.Errorf("POST %s with a key lacking %s: %d %s", path, scope, code, body)
		}
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/o/alpha/api/v1/"+path, strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", srv.URL)
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusUnauthorized {
			t.Errorf("POST %s signed in, with no key: %d", path, res.StatusCode)
		}
	}
}

func jsonInt(n int64) string { b, _ := json.Marshal(n); return string(b) }
