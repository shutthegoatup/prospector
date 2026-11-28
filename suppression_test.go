package main

import (
	"encoding/json"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/shutthegoatup/prospector/api"
	"github.com/shutthegoatup/prospector/internal/crm"
	"github.com/shutthegoatup/prospector/internal/mailer"
	"github.com/shutthegoatup/prospector/internal/marketing"
	"github.com/shutthegoatup/prospector/internal/paging"
)

// link makes an unsubscribe link for an address, as a marketing mail will.
func link(t *testing.T, s *Store, org int64, address string) string {
	t.Helper()
	token, err := s.MintUnsubscribeLink(s.DB, org, address)
	if err != nil {
		t.Fatal(err)
	}
	return "/u/" + token
}

func TestAnUnsubscribeLinkStopsMarketingMailInOneClick(t *testing.T) {
	t.Parallel()
	s, o, srv, _ := signedIn(t)
	other := mustOrg(t, s, "beta")
	hannah := link(t, s, o.ID, "Hannah@Larkmoor.example")
	stranger := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	listed := func(org int64) int {
		t.Helper()
		rows, _, err := s.Suppressions(org, paging.Page{})
		if err != nil {
			t.Fatal(err)
		}
		return len(rows)
	}

	// Looking at the link changes nothing, however often: mail scanners follow links.
	for range 2 {
		res, err := stranger.Get(srv.URL + hannah)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusOK || res.Header.Get("Referrer-Policy") != "no-referrer" || !strings.Contains(res.Header.Get("X-Robots-Tag"), "noindex") {
			t.Errorf("looking at the link: %d, referrer %q, robots %q", res.StatusCode, res.Header.Get("Referrer-Policy"), res.Header.Get("X-Robots-Tag"))
		}
	}
	if listed(o.ID) != 0 {
		t.Fatal("looking at the link unsubscribed them")
	}
	if _, page := call(t, stranger, "GET", srv.URL+hannah, "", nil); strings.Contains(strings.ToLower(page), "hannah") || strings.Contains(page, "larkmoor") || strings.Contains(page, "Alpha") {
		t.Error("the page says whose link it is: mail gets forwarded")
	}

	// A mail provider's one-click request, as RFC 8058 has it: a form body, no cookie, no
	// origin, and an answer that is not a redirect.
	req, _ := http.NewRequest("POST", srv.URL+hannah, strings.NewReader(url.Values{"List-Unsubscribe": {"One-Click"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := stranger.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("a one-click unsubscribe answered %d", res.StatusCode)
	}
	rows, _, _ := s.Suppressions(o.ID, paging.Page{})
	if len(rows) != 1 || rows[0].Address != "hannah@larkmoor.example" || rows[0].Reason != marketing.SuppressUnsubscribed {
		t.Fatalf("after unsubscribing: %+v", rows)
	}
	if listed(other.ID) != 0 {
		t.Error("an unsubscribe from one organisation's mail reached another's list")
	}

	// From a webmail page on its own origin, and twice: still one row, still theirs.
	req, _ = http.NewRequest("POST", srv.URL+hannah, strings.NewReader("List-Unsubscribe=One-Click"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://webmail.example")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	if res, err := stranger.Do(req); err != nil || res.StatusCode != http.StatusOK {
		t.Errorf("a one-click unsubscribe from a webmail page: %v %v", res, err)
	}
	if listed(o.ID) != 1 {
		t.Errorf("unsubscribing twice left %d rows", listed(o.ID))
	}

	// A link that was never made is answered as one that was, and records nothing.
	if code, page := call(t, stranger, "POST", srv.URL+"/u/never-made", "application/x-www-form-urlencoded", nil); code != http.StatusOK || !served(t, page).Done {
		t.Errorf("a made-up link: %d %.200s", code, page)
	}
	if listed(o.ID) != 1 {
		t.Error("a made-up link recorded something")
	}

	// Marketing mail can't go to them now, whatever the case of the address.
	if err := s.Marketable(s.DB, o, "HANNAH@larkmoor.example", ""); err != marketing.ErrSuppressed {
		t.Errorf("an unsubscribed address is marketable: %v", err)
	}
	if err := s.Marketable(s.DB, other, "hannah@larkmoor.example", ""); err != nil {
		t.Errorf("the address is held against another organisation: %v", err)
	}
}

func TestTheListIsKeptByMembersAndKeys(t *testing.T) {
	t.Parallel()
	s, o, srv, member := signedIn(t)
	other := mustOrg(t, s, "beta")
	key := func(org int64, scopes ...string) string {
		secret, _, err := s.CreateKey(org, "robot "+strings.Join(scopes, "+"), scopes, "Ada Lovelace")
		if err != nil {
			t.Fatal(err)
		}
		return secret
	}
	reader, keeper, stranger := key(o.ID, "read"), key(o.ID, "campaigns:write"), key(other.ID, "read", "campaigns:write")
	list := srv.URL + "/o/alpha/api/v1/suppressions"

	if code, body := asKey(t, "POST", list, reader, map[string]any{"address": "tom@larkmoor.example"}); code != http.StatusForbidden || !strings.Contains(body, "campaigns:write") {
		t.Errorf("a read key adding to the list: %d %s", code, body)
	}
	for _, bad := range []string{"", "not an address", "a@b.example, c@d.example"} {
		if code, _ := asKey(t, "POST", list, keeper, map[string]any{"address": bad}); code != http.StatusBadRequest {
			t.Errorf("adding %q: %d", bad, code)
		}
	}
	code, body := asKey(t, "POST", list, keeper, map[string]any{"address": " Tom@Larkmoor.example "})
	if code != http.StatusCreated || !strings.Contains(body, `"address":"tom@larkmoor.example"`) || !strings.Contains(body, `"reason":"added"`) {
		t.Fatalf("adding to the list: %d %s", code, body)
	}
	if code, body := call(t, member, "POST", list, "application/json", map[string]any{"address": "tom@larkmoor.example"}); code != http.StatusCreated {
		t.Errorf("adding the same address again: %d %s", code, body)
	}
	added := must(s.Suppress(o.ID, "tom@larkmoor.example", "Ada Lovelace"))

	// Someone who unsubscribes themselves outranks a member's entry, and can't be taken off.
	if err := s.UnsubscribeByToken(strings.TrimPrefix(link(t, s, o.ID, "tom@larkmoor.example"), "/u/")); err != nil {
		t.Fatal(err)
	}
	theirs := srv.URL + "/o/alpha/api/v1/suppressions/" + itoa(added.ID)
	if code, body := asKey(t, "DELETE", theirs, keeper, nil); code != http.StatusBadRequest || !strings.Contains(body, "themselves") {
		t.Errorf("taking off someone who unsubscribed: %d %s", code, body)
	}

	// One a member added can be taken off, by them or a key, but not from elsewhere.
	sven := must(s.Suppress(o.ID, "sven@nordkap.example", "Ada Lovelace"))
	if code, body := asKey(t, "DELETE", srv.URL+"/o/beta/api/v1/suppressions/"+itoa(sven.ID), stranger, nil); code != http.StatusNotFound {
		t.Errorf("another organisation taking ours off by number: %d %s", code, body)
	}
	if code, _ := asKey(t, "GET", list, stranger, nil); code < 400 {
		t.Errorf("another organisation's key reading our list: %d", code)
	}
	if code, body := asKey(t, "DELETE", list+"/"+itoa(sven.ID), keeper, nil); code != http.StatusNoContent {
		t.Errorf("taking off an added address: %d %s", code, body)
	}

	// The list comes a page at a time, newest first.
	for _, who := range []string{"a@one.example", "b@two.example", "c@three.example"} {
		must(s.Suppress(o.ID, who, "Ada Lovelace"))
	}
	code, body = asKey(t, "GET", list+"?limit=2", reader, nil)
	if code != http.StatusOK || !strings.Contains(body, "c@three.example") || strings.Contains(body, "a@one.example") || !strings.Contains(body, `"next_cursor":"`) {
		t.Fatalf("the first page: %d %s", code, body)
	}
	cursor := body[strings.Index(body, `"next_cursor":"`)+len(`"next_cursor":"`):]
	cursor = cursor[:strings.Index(cursor, `"`)]
	if code, body := asKey(t, "GET", list+"?limit=2&cursor="+cursor, reader, nil); code != http.StatusOK || !strings.Contains(body, "a@one.example") || strings.Contains(body, "c@three.example") || strings.Contains(body, "next_cursor") {
		t.Errorf("the second page: %d %s", code, body)
	}
	if code, _ := asKey(t, "GET", list+"?cursor=nonsense", reader, nil); code != http.StatusBadRequest {
		t.Errorf("a cursor this list never gave out: %d", code)
	}

	// Settings reads the list as a member, and offers to take off only what was added.
	code, page := call(t, member, "GET", list+"?limit=200", "", nil)
	var rows struct{ Items []api.Suppression }
	if code != http.StatusOK || json.Unmarshal([]byte(page), &rows) != nil {
		t.Fatalf("the list, read as a member: %d %s", code, page)
	}
	found := false
	for _, r := range rows.Items {
		if r.Address == "tom@larkmoor.example" {
			found = true
			if r.Id != added.ID || r.Reason != "unsubscribed" || r.By != "" {
				t.Errorf("someone who unsubscribed themselves should be shown as theirs, so they can't be taken off: %+v", r)
			}
		}
	}
	if !found {
		t.Error("the list lacks someone who unsubscribed")
	}
}

// Never-contact and erasure keep an address from marketing mail too, and erasing
// someone takes their address off the list while still keeping them unmailed.
func TestErasureAndNeverContactKeepAnAddressUnmailed(t *testing.T) {
	t.Parallel()
	s, o, _, _ := signedIn(t)
	hannah := must(s.QuickAddPerson(o, crm.QuickAdd{Name: "Hannah Pryce", Company: "Larkmoor Yachts", Email: "hannah@larkmoor.example"}, "Ada Lovelace"))
	if err := s.UnsubscribeByToken(strings.TrimPrefix(link(t, s, o.ID, "hannah@larkmoor.example"), "/u/")); err != nil {
		t.Fatal(err)
	}
	if export := must(s.ExportPerson(o.ID, hannah.ID, "Ada Lovelace")); len(export.Suppressed) != 1 {
		t.Errorf("her export of the list: %+v", export.Suppressed)
	}
	if export := must(s.ExportOrg(o.ID, "Ada Lovelace")); len(export.Suppressed) != 1 {
		t.Errorf("the organisation's export of the list: %+v", export.Suppressed)
	}
	if err := s.ErasePerson(o.ID, hannah.ID, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	var left int64
	for _, table := range []any{&Suppression{}, &UnsubscribeLink{}} {
		if err := s.DB.Model(table).Where("address = ?", "hannah@larkmoor.example").Count(&left).Error; err != nil || left != 0 {
			t.Errorf("erasing her left %d rows of %T with her address (%v)", left, table, err)
		}
	}
	if err := s.Marketable(s.DB, o, "hannah@larkmoor.example", ""); err != errErased {
		t.Errorf("an erased address is marketable: %v", err)
	}

	o.Settings.NeverContact = "nordkap.example"
	if err := s.SaveOrg(o); err != nil {
		t.Fatal(err)
	}
	if err := s.Marketable(s.DB, o, "sven@nordkap.example", ""); err == nil {
		t.Error("an address on the never-contact list is marketable")
	}
	if err := s.Marketable(s.DB, o, "tom@larkmoor.example", ""); err != nil {
		t.Errorf("an address nothing holds against is not marketable: %v", err)
	}
}

func TestAMarketingMailCarriesItsOneClickHeaders(t *testing.T) {
	t.Parallel()
	from, to := &mail.Address{Address: "news@alpha.example"}, &mail.Address{Address: "hannah@larkmoor.example"}
	wire := func(unsubscribe string) string {
		return string(mailer.Compose(from, to, mailer.Message{Subject: "News", Body: "Hello", Unsubscribe: unsubscribe}, time.Now()))
	}
	got := wire("https://prospector.example/u/abc123")
	if !strings.Contains(got, "\r\nList-Unsubscribe: <https://prospector.example/u/abc123>\r\n") || !strings.Contains(got, "\r\nList-Unsubscribe-Post: List-Unsubscribe=One-Click\r\n") {
		t.Errorf("a marketing mail without its one-click headers:\n%s", got)
	}
	for _, refused := range []string{"", "http://prospector.example/u/abc", "https://prospector.example/u/a\r\nBcc: victim@elsewhere.example", "https://prospector.example/u/a>, <mailto:x@y.example"} {
		if got := wire(refused); strings.Contains(got, "List-Unsubscribe") || strings.Contains(got, "Bcc") {
			t.Errorf("an unsubscribe link of %q made it into the mail:\n%s", refused, got)
		}
	}
}

// What the review of the list found: a path aimed at another route, a delete racing an
// unsubscribe, an address ending in a dot, and an address suggested for someone.
func TestTheListHoldsAtItsEdges(t *testing.T) {
	t.Parallel()
	s, o, srv, _ := signedIn(t)
	visitor := &http.Client{}
	for _, aimed := range []string{"/u/..%2Flogout", "/u/..%2F..%2Fc%2Fsteps%2F1%2Fdone", "/u/short", "/u/" + strings.Repeat("a", 42) + "%2F"} {
		_, page := call(t, visitor, "GET", srv.URL+aimed, "", nil)
		if served(t, page).Token != "" || strings.Contains(page, "..") {
			t.Errorf("%s gave a page with a form, or kept the path: %.300s", aimed, page)
		}
	}
	good := link(t, s, o.ID, "hannah@larkmoor.example")
	if _, page := call(t, visitor, "GET", srv.URL+good, "", nil); "/u/"+served(t, page).Token != good {
		t.Errorf("a real link lost its form: %.300s", page)
	}

	// A member takes an address off just as the person unsubscribes: theirs stays.
	added := must(s.Suppress(o.ID, "tom@larkmoor.example", "Ada Lovelace"))
	if err := s.DB.Model(&Suppression{}).Where("id = ?", added.ID).Update("reason", marketing.SuppressUnsubscribed).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.Unsuppress(o.ID, added.ID, "Ada Lovelace"); err == nil {
		t.Error("a member took off someone who had unsubscribed")
	}
	// And an unsubscribe arriving after a member took the row off is still kept.
	sven := must(s.Suppress(o.ID, "sven@nordkap.example", "Ada Lovelace"))
	token := strings.TrimPrefix(link(t, s, o.ID, "sven@nordkap.example"), "/u/")
	if err := s.Unsuppress(o.ID, sven.ID, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	if err := s.UnsubscribeByToken(token); err != nil {
		t.Fatal(err)
	}
	if err := s.Marketable(s.DB, o, "sven@nordkap.example", ""); err != marketing.ErrSuppressed {
		t.Errorf("an unsubscribe after the row was taken off was lost: %v", err)
	}

	// Erasure and the person's export count the addresses erasure does.
	jo := must(s.QuickAddPerson(o, crm.QuickAdd{Name: "Jo Marsh", Company: "Larkmoor Yachts", Email: "jo@larkmoor.example."}, "Ada Lovelace"))
	if err := s.DB.Create(&Suggestion{OrgID: o.ID, ContactID: jo.ID, Field: "email", Value: "jo.marsh@larkmoor.example", SourceURL: "https://larkmoor.example/team"}).Error; err != nil {
		t.Fatal(err)
	}
	for _, address := range []string{"jo@larkmoor.example", "jo.marsh@larkmoor.example"} {
		if err := s.UnsubscribeByToken(strings.TrimPrefix(link(t, s, o.ID, address), "/u/")); err != nil {
			t.Fatal(err)
		}
	}
	if export := must(s.ExportPerson(o.ID, jo.ID, "Ada Lovelace")); len(export.Suppressed) != 2 {
		t.Errorf("her export has %d of her 2 addresses on the list: %+v", len(export.Suppressed), export.Suppressed)
	}
	if err := s.ErasePerson(o.ID, jo.ID, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	var left int64
	if err := s.DB.Model(&Suppression{}).Where("address like ?", "jo%").Count(&left).Error; err != nil || left != 0 {
		t.Errorf("erasing her left %d of her addresses on the list (%v)", left, err)
	}

	from, to := &mail.Address{Address: "news@alpha.example"}, &mail.Address{Address: "hannah@larkmoor.example"}
	for _, odd := range []string{"https://prospector.example/u/a\tb", "https://prospector.example/u/a\x00b", "https://prospector.example/u/é"} {
		if wire := string(mailer.Compose(from, to, mailer.Message{Subject: "News", Body: "Hello", Unsubscribe: odd}, time.Now())); strings.Contains(wire, "List-Unsubscribe") {
			t.Errorf("an unsubscribe link of %q made it into a header", odd)
		}
	}
}
