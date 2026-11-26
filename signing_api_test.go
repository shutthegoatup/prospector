package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"
)

// Through the APIs, end to end: a member sends, the outsider signs from their link with
// the emailed code, the member signs signed in, and the sealed copy downloads.
func TestAnAgreementIsSignedThroughTheAPIs(t *testing.T) {
	box := &postbox{}
	var app *App
	s, srv, member, _ := mailSite(t, func(a *App) { a.send, app = box.send, a })
	o := must(s.OrgBySlug("alpha"))
	if err := s.SaveOrgMail(&OrgMail{OrgID: o.ID, Host: "smtp.alpha.example", Port: 587, Username: "deals@alpha.example", Sender: "Alpha <deals@alpha.example>"}); err != nil {
		t.Fatal(err)
	}
	ada := must(s.UserByEmail("ada@example.com"))
	hannah := person(t, s, o, "Hannah Pryce", "Larkmoor Yachts", "hannah@larkmoor.example", false)
	a := must(s.AddAgreement(o.ID, *hannah.CompanyID, "Service agreement", "Ada Lovelace"))
	must(s.PutAgreementFile(o.ID, a.ID, "service.pdf", testPDF(1, ""), "Ada Lovelace"))
	h := must(s.AddRecipient(o, a.ID, RecipientSigner, &hannah.ID, nil, "Ada Lovelace"))
	m := must(s.AddRecipient(o, a.ID, RecipientSigner, nil, &ada.ID, "Ada Lovelace"))
	must(s.PutAgreementFields(o.ID, a.ID, []AgreementField{
		{RecipientID: h.ID, Page: 1, Kind: "signature", X: 0.1, Y: 0.7, W: 0.3, H: 0.06},
		{RecipientID: m.ID, Page: 1, Kind: "signature", X: 0.55, Y: 0.7, W: 0.3, H: 0.06},
	}))
	base := srv.URL + "/o/alpha/api/v1/agreements/" + itoa(a.ID)
	round := func() { app.signingRound(context.Background(), time.Now()) }

	every := []string{}
	for _, sc := range Scopes {
		every = append(every, sc.Name)
	}
	writer, _ := must2(s.CreateKey(o.ID, "writer", []string{"agreements:write"}, "Ada Lovelace"))
	all, _ := must2(s.CreateKey(o.ID, "all", every, "Ada Lovelace"))
	if code, body := asKey(t, "POST", base+"/send", writer, map[string]any{}); code != http.StatusForbidden || !strings.Contains(body, "agreements:send") {
		t.Errorf("a key without agreements:send sending: %d %s", code, body)
	}
	code, body := call(t, member, "POST", base+"/send", "application/json", map[string]any{"message": "As discussed.", "days": 7})
	if code != http.StatusOK || !strings.Contains(body, `"status":"sent"`) {
		t.Fatalf("sending: %d %s", code, body)
	}
	round()

	// Hannah's browser: the link, then the code, then the document and her signature.
	_, link := latestMail(t, box, "hannah@larkmoor.example")
	signer := newMailClient()
	token := strings.TrimPrefix(link, testSite+"/s/")
	code, body = call(t, signer, "POST", srv.URL+"/s/api/v1/open", "application/json", map[string]any{"link": token})
	if code != http.StatusOK || !strings.Contains(body, `"proven":false`) || strings.Contains(body, "As discussed") || !strings.Contains(body, `"sent_to":"h`) {
		t.Fatalf("opening the link: %d %s", code, body)
	}
	if code, _ := call(t, signer, "GET", srv.URL+"/s/api/v1/document", "", nil); code == http.StatusOK {
		t.Error("the document before the code")
	}
	if code, body := call(t, signer, "POST", srv.URL+"/s/api/v1/code", "application/json", map[string]any{}); code != http.StatusOK {
		t.Fatalf("asking for a code: %d %s", code, body)
	}
	mail, _ := latestMail(t, box, "hannah@larkmoor.example")
	got := regexp.MustCompile(`\b\d{6}\b`).FindString(mail.Body)
	if code, body = call(t, signer, "POST", srv.URL+"/s/api/v1/code/check", "application/json", map[string]any{"code": got}); code != http.StatusOK || !strings.Contains(body, "As discussed") || !strings.Contains(body, `"state":"sign"`) {
		t.Fatalf("giving the code back: %d %s", code, body)
	}
	if code, _ := call(t, signer, "GET", srv.URL+"/s/api/v1/document", "", nil); code != http.StatusOK {
		t.Errorf("the document after the code: %d", code)
	}

	// Another site can't make her browser sign.
	req, _ := http.NewRequest("POST", srv.URL+"/s/api/v1/sign", bytes.NewReader([]byte(`{"typed_name":"Hannah Pryce","consent":true}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.Header.Set("Origin", "https://elsewhere.example")
	if res, err := signer.Do(req); err != nil || res.StatusCode != http.StatusForbidden {
		t.Errorf("a cross-site signature: %v %v", res, err)
	}

	if code, body = call(t, signer, "POST", srv.URL+"/s/api/v1/sign", "application/json", map[string]any{"typed_name": "Hannah Pryce", "consent": true}); code != http.StatusOK || !strings.Contains(body, `"state":"signed"`) {
		t.Fatalf("Hannah signing: %d %s", code, body)
	}

	// Ada signs signed in; no key can, whatever it holds.
	if code, body := asKey(t, "POST", base+"/sign", all, map[string]any{"typed_name": "Ada Lovelace", "consent": true}); code != http.StatusForbidden {
		t.Errorf("a key with every scope signing: %d %s", code, body)
	}
	if _, body := call(t, member, "GET", base, "", nil); !strings.Contains(body, `"my_turn":true`) {
		t.Errorf("the agreement doesn't say it's Ada's turn: %s", body)
	}
	if code, body := call(t, member, "POST", base+"/sign", "application/json", map[string]any{"typed_name": "Ada Lovelace", "consent": true}); code != http.StatusOK || !strings.Contains(body, `"status":"sealing"`) {
		t.Fatalf("Ada signing: %d %s", code, body)
	}
	round()
	res, err := member.Get(base + "/sealed")
	if err != nil {
		t.Fatal(err)
	}
	sealedCopy, _ := io.ReadAll(res.Body)
	res.Body.Close()
	_, cert := must2(s.sealKey(o))
	if res.StatusCode != http.StatusOK || !strings.HasPrefix(res.Header.Get("Content-Disposition"), "attachment") || checkSeal(sealedCopy, cert) != nil {
		t.Errorf("the sealed copy: %d %q", res.StatusCode, res.Header.Get("Content-Disposition"))
	}
	if _, body := call(t, member, "GET", base+"/events?limit=200", "", nil); !strings.Contains(body, `"kind":"sealed"`) || !strings.Contains(body, `"hash":"`) {
		t.Errorf("what happened: %s", body)
	}
	if code, _ := call(t, signer, "GET", srv.URL+"/s/api/v1/sealed", "", nil); code != http.StatusOK {
		t.Errorf("Hannah downloading the sealed copy: %d", code)
	}
}

// The signer's page is served to anyone with a link: none of the members' app, the
// pdf.js policy, never a referrer (the link is in its address) and not for search engines.
func TestTheSignersPageIsServedWithoutTheApp(t *testing.T) {
	_, srv, _, _ := mailSite(t, nil)
	res, err := http.Get(srv.URL + "/s/some-token")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || !strings.Contains(string(body), `id="sign"`) || !strings.Contains(string(body), "/assets/app/sign.js?v=") || strings.Contains(string(body), "main.js") {
		t.Errorf("the signer's page: %d %.300s", res.StatusCode, body)
	}
	for header, want := range map[string]string{"Content-Security-Policy": pdfPolicy, "Referrer-Policy": "no-referrer", "X-Robots-Tag": "noindex, nofollow"} {
		if got := res.Header.Get(header); got != want {
			t.Errorf("%s: %q, want %q", header, got, want)
		}
	}
	if code, _ := call(t, newMailClient(), "GET", srv.URL+"/s/api/v1/signing", "", nil); code == http.StatusOK {
		t.Error("the signing API answered a browser with no session")
	}
}

// latestMail is the latest mail to an address and the link in it.
func latestMail(t *testing.T, box *postbox, address string) (Message, string) {
	t.Helper()
	box.mu.Lock()
	defer box.mu.Unlock()
	for i := len(box.sent) - 1; i >= 0; i-- {
		if m := box.sent[i]; m.To == address {
			return m, regexp.MustCompile(`https://prospector\.example/\S+`).FindString(m.Body)
		}
	}
	t.Fatalf("no mail to %s", address)
	return Message{}, ""
}
