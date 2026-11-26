package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

// preparing starts a draft for Larkmoor with a two-page document, and gives what a
// test of its signers needs.
func preparing(t *testing.T) (s *Store, o *Org, base string, member *http.Client, a *Agreement, hannah, theo *Contact) {
	t.Helper()
	s, o, srv, member := signedIn(t)
	hannah = must(s.QuickAddPerson(o, QuickAdd{Name: "Hannah Pryce", Company: "Larkmoor Yachts", Email: "hannah@larkmoor.example"}, "Ada Lovelace"))
	theo = must(s.QuickAddPerson(o, QuickAdd{Name: "Theo Marsh", Company: "Larkmoor Yachts", Email: "theo@larkmoor.example"}, "Ada Lovelace"))
	a = must(s.AddAgreement(o.ID, *hannah.CompanyID, "Service agreement", "Ada Lovelace"))
	base = srv.URL + "/o/alpha/api/v1/agreements/" + itoa(a.ID)
	if code, body, _ := upload(t, member, base+"/document?name=service.pdf", "", "application/pdf", testPDF(2, "")); code != http.StatusOK {
		t.Fatalf("uploading: %d %s", code, body)
	}
	return s, o, base, member, a, hannah, theo
}

func items[T any](t *testing.T, body string) []T {
	t.Helper()
	var out struct{ Items []T }
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("%v: %s", err, body)
	}
	return out.Items
}

func TestAnAgreementsSignersAreItsCompanysPeopleOrItsMembers(t *testing.T) {
	s, o, base, member, a, hannah, theo := preparing(t)
	recipients := base + "/recipients"
	ada := must(s.UserByEmail("ada@example.com"))

	if got := must(s.Agreement(o.ID, a.ID)); got.PageCount != 2 || got.Round != 1 || !got.InOrder {
		t.Errorf("a new draft with its document: %d pages, round %d, in order %v", got.PageCount, got.Round, got.InOrder)
	}

	elsewhere := must(s.QuickAddPerson(o, QuickAdd{Name: "Rhys Doyle", Company: "Ferncliff Marine", Email: "rhys@ferncliff.example"}, "Ada Lovelace"))
	noAddress := must(s.QuickAddPerson(o, QuickAdd{Name: "Mo Lind", Company: "Larkmoor Yachts"}, "Ada Lovelace"))
	for why, body := range map[string]map[string]any{
		"someone at another company":          {"role": "signer", "contact": elsewhere.ID},
		"someone with no address":             {"role": "signer", "contact": noAddress.ID},
		"nobody":                              {"role": "signer"},
		"a contact and a member":              {"role": "signer", "contact": hannah.ID, "member": ada.ID},
		"a role there isn't":                  {"role": "witness", "contact": hannah.ID},
		"a member of no organisation of ours": {"role": "signer", "member": 99999},
	} {
		if code, out := call(t, member, "POST", recipients, "application/json", body); code == http.StatusCreated {
			t.Errorf("%s was added: %s", why, out)
		}
	}

	for _, body := range []map[string]any{{"role": "signer", "contact": hannah.ID}, {"role": "signer", "member": ada.ID}, {"role": "copy", "contact": theo.ID}} {
		if code, out := call(t, member, "POST", recipients, "application/json", body); code != http.StatusCreated {
			t.Fatalf("adding %v: %d %s", body, code, out)
		}
	}
	if code, _ := call(t, member, "POST", recipients, "application/json", map[string]any{"role": "copy", "contact": hannah.ID}); code != http.StatusBadRequest {
		t.Errorf("adding Hannah twice: %d", code)
	}
	_, body := call(t, member, "GET", recipients, "", nil)
	list := items[Party](t, body)
	if len(list) != 3 || list[0].Name != "Hannah Pryce" || list[0].Email != "hannah@larkmoor.example" || list[1].Name != "Ada Lovelace" || list[1].Email != "ada@example.com" || list[2].Role != RecipientCopy {
		t.Fatalf("who it goes to: %+v", list)
	}

	// Ada moves ahead of Hannah, then the never-contact list catches Larkmoor.
	if code, out := call(t, member, "PATCH", recipients+"/"+itoa(list[1].ID), "application/json", map[string]any{"position": 1}); code != http.StatusOK || !strings.Contains(out, `"position":1`) {
		t.Errorf("moving Ada first: %d %s", code, out)
	}
	_, body = call(t, member, "GET", recipients, "", nil)
	if list := items[Party](t, body); list[0].Name != "Ada Lovelace" || list[1].Name != "Hannah Pryce" {
		t.Errorf("the order after moving: %+v", list)
	}
	ines := must(s.QuickAddPerson(o, QuickAdd{Name: "Ines Ward", Company: "Larkmoor Yachts", Email: "ines@larkmoor.example"}, "Ada Lovelace"))
	o.Settings.NeverContact = "ines@larkmoor.example"
	if err := s.SaveOrg(o); err != nil {
		t.Fatal(err)
	}
	if code, _ := call(t, member, "POST", recipients, "application/json", map[string]any{"role": "signer", "contact": ines.ID}); code != http.StatusBadRequest {
		t.Errorf("someone on the never-contact list was added: %d", code)
	}
}

func TestFieldsLieOnTheDocumentForItsSigners(t *testing.T) {
	s, o, base, member, a, hannah, theo := preparing(t)
	signer := must(s.AddRecipient(o, a.ID, RecipientSigner, &hannah.ID, nil, "Ada Lovelace"))
	copied := must(s.AddRecipient(o, a.ID, RecipientCopy, &theo.ID, nil, "Ada Lovelace"))
	fields := base + "/fields"
	field := func(recipient int64, page int, kind string, x, y, w, h float64) map[string]any {
		return map[string]any{"recipient_id": recipient, "page": page, "kind": kind, "x": x, "y": y, "w": w, "h": h}
	}
	put := func(fs ...map[string]any) (int, string) {
		return call(t, member, "PUT", fields, "application/json", map[string]any{"items": fs})
	}

	if missing := must(s.Readiness(o.ID, *must(s.Agreement(o.ID, a.ID)))); len(missing) != 1 || !strings.Contains(missing[0], "Hannah Pryce") {
		t.Errorf("before any fields, it lacks %v", missing)
	}
	for why, f := range map[string]map[string]any{
		"a page the document hasn't":      field(signer.ID, 3, "signature", 0.1, 0.1, 0.3, 0.05),
		"one running off the page":        field(signer.ID, 1, "signature", 0.8, 0.1, 0.3, 0.05),
		"one for someone who gets a copy": field(copied.ID, 1, "signature", 0.1, 0.1, 0.3, 0.05),
		"a kind there isn't":              field(signer.ID, 1, "stamp", 0.1, 0.1, 0.3, 0.05),
		"one too small to fill in":        field(signer.ID, 1, "signature", 0.1, 0.1, 0.001, 0.05),
	} {
		if code, out := put(field(signer.ID, 1, "date", 0.1, 0.5, 0.2, 0.03), f); code == http.StatusOK {
			t.Errorf("%s was kept: %s", why, out)
		}
	}
	if code, out := put(field(signer.ID, 2, "signature", 0.1, 0.7, 0.35, 0.06), field(signer.ID, 2, "date", 0.1, 0.8, 0.2, 0.03)); code != http.StatusOK || len(items[AgreementField](t, out)) != 2 {
		t.Fatalf("placing two fields: %d %s", code, out)
	}
	if missing := must(s.Readiness(o.ID, *must(s.Agreement(o.ID, a.ID)))); len(missing) != 0 {
		t.Errorf("with a signature placed, it still lacks %v", missing)
	}

	// A one-page replacement loses what was on page 2.
	if code, body, _ := upload(t, member, base+"/document?name=short.pdf", "", "application/pdf", testPDF(1, "")); code != http.StatusOK {
		t.Fatalf("replacing the document: %d %s", code, body)
	}
	if left := must(s.AgreementFields(o.ID, a.ID)); len(left) != 0 {
		t.Errorf("fields on a page the new document hasn't were kept: %+v", left)
	}
	if code, out := put(field(signer.ID, 1, "signature", 0.1, 0.7, 0.35, 0.06)); code != http.StatusOK {
		t.Fatalf("placing on the new document: %d %s", code, out)
	}

	// Making Hannah a copy takes her fields away; taking Theo off leaves hers alone.
	if code, out := call(t, member, "PATCH", base+"/recipients/"+itoa(signer.ID), "application/json", map[string]any{"role": "copy"}); code != http.StatusOK {
		t.Fatalf("making Hannah a copy: %d %s", code, out)
	}
	if left := must(s.AgreementFields(o.ID, a.ID)); len(left) != 0 {
		t.Errorf("someone who only gets a copy kept fields: %+v", left)
	}
	if code, _ := call(t, member, "DELETE", base+"/recipients/"+itoa(copied.ID), "", nil); code != http.StatusNoContent {
		t.Errorf("taking Theo off: %d", code)
	}
}

// Erasing someone takes them off what they were to sign; removing a member does too,
// and removing the draft takes its recipients and fields with it.
func TestAnAgreementsRecipientsGoWithWhoTheyStandFor(t *testing.T) {
	s, o, _, _, a, hannah, theo := preparing(t)
	grace := must(s.CreateUser("grace@example.com", "Grace Hopper", "correct horse battery"))
	if err := s.AddMember(o.ID, grace.ID, "member", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	h := must(s.AddRecipient(o, a.ID, RecipientSigner, &hannah.ID, nil, "Ada Lovelace"))
	g := must(s.AddRecipient(o, a.ID, RecipientSigner, nil, &grace.ID, "Ada Lovelace"))
	must(s.AddRecipient(o, a.ID, RecipientSigner, &theo.ID, nil, "Ada Lovelace"))
	must(s.PutAgreementFields(o.ID, a.ID, []AgreementField{
		{RecipientID: h.ID, Page: 1, Kind: "signature", X: 0.1, Y: 0.1, W: 0.3, H: 0.05},
		{RecipientID: g.ID, Page: 1, Kind: "signature", X: 0.5, Y: 0.1, W: 0.3, H: 0.05},
	}))
	if export := must(s.ExportPerson(o.ID, hannah.ID, "Ada Lovelace")); len(export.Agreements) != 1 {
		t.Errorf("Hannah's export of what she is to sign: %+v", export.Agreements)
	}
	if export := must(s.ExportOrg(o.ID, "Ada Lovelace")); len(export.Recipients) != 3 || len(export.Fields) != 2 {
		t.Errorf("the organisation's export: %d recipients, %d fields", len(export.Recipients), len(export.Fields))
	}

	if err := s.ErasePerson(o.ID, hannah.ID, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RemoveMember(o.ID, grace.ID, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	left := must(s.AgreementRecipients(o.ID, a.ID))
	if len(left) != 1 || left[0].Name != "Theo Marsh" {
		t.Errorf("after erasing Hannah and removing Grace: %+v", left)
	}
	if fs := must(s.AgreementFields(o.ID, a.ID)); len(fs) != 0 {
		t.Errorf("their fields stayed: %+v", fs)
	}
	if err := s.RemoveAgreement(o.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	var n int64
	if err := s.db.Model(&AgreementRecipient{}).Count(&n).Error; err != nil || n != 0 {
		t.Errorf("removing the draft left %d recipients (%v)", n, err)
	}
}

func TestAKeyPreparesAnAgreementWithTheWriteScope(t *testing.T) {
	s, o, base, _, _, hannah, _ := preparing(t)
	key := func(scopes ...string) string {
		secret, _, err := s.CreateKey(o.ID, "robot "+strings.Join(scopes, "+"), scopes, "Ada Lovelace")
		if err != nil {
			t.Fatal(err)
		}
		return secret
	}
	reader, writer := key("read"), key("agreements:write")
	add := map[string]any{"role": "signer", "contact": hannah.ID}
	if code, _ := asKey(t, "POST", base+"/recipients", reader, add); code != http.StatusForbidden {
		t.Errorf("a read key adding a signer: %d", code)
	}
	code, body := asKey(t, "POST", base+"/recipients", writer, add)
	if code != http.StatusCreated {
		t.Fatalf("a write key adding a signer: %d %s", code, body)
	}
	if code, body := asKey(t, "PATCH", base, writer, map[string]any{"in_order": false}); code != http.StatusOK || !strings.Contains(body, `"in_order":false`) {
		t.Errorf("signing all at once: %d %s", code, body)
	}
	if code, body := asKey(t, "GET", base+"/recipients", reader, nil); code != http.StatusOK || !strings.Contains(body, "hannah@larkmoor.example") {
		t.Errorf("a read key listing who signs: %d %s", code, body)
	}
}

// The Prepare page lists who signs and lets sales change it; someone in support only
// reads it. Its editor is a script of its own, with pdf.js's worker served as script.
func TestAnAgreementIsPreparedOnItsOwnPage(t *testing.T) {
	s, o, base, member, a, hannah, theo := preparing(t)
	must(s.AddRecipient(o, a.ID, RecipientSigner, &hannah.ID, nil, "Ada Lovelace"))
	must(s.AddRecipient(o, a.ID, RecipientCopy, &theo.ID, nil, "Ada Lovelace"))
	srv := strings.TrimSuffix(base, "/o/alpha/api/v1/agreements/"+itoa(a.ID))
	page := srv + "/o/alpha/agreements/" + itoa(a.ID) + "/prepare"

	code, body := call(t, member, "GET", page, "", nil)
	if code != http.StatusOK {
		t.Fatalf("the page: %d", code)
	}
	for _, want := range []string{"Prepare for signing", "1 · Hannah Pryce", "theo@larkmoor.example", `data-editor="` + itoa(a.ID) + `"`, `name="placing"`, `data-kind="signature"`, "/assets/js/prepare.js", "Place a signature for Hannah Pryce", "nothing here leaves Prospector"} {
		if !strings.Contains(body, want) {
			t.Errorf("the Prepare page lacks %q", want)
		}
	}
	if strings.Contains(body, ">Hannah Pryce · hannah@larkmoor.example</option>") {
		t.Error("Hannah is offered again though she is on it")
	}
	if _, company := call(t, member, "GET", srv+"/o/alpha/companies/"+itoa(*hannah.CompanyID), "", nil); !strings.Contains(company, "agreements/"+itoa(a.ID)+"/prepare") {
		t.Error("the company's page doesn't lead to Prepare for signing")
	}

	grace := must(s.CreateUser("grace@example.com", "Grace Hopper", "correct horse battery"))
	if err := s.AddMember(o.ID, grace.ID, "member", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAreas(o.ID, grace.ID, []string{"support"}); err != nil {
		t.Fatal(err)
	}
	support := newMailClient()
	if !signIn(t, support, &httptest.Server{URL: srv}, "grace@example.com", "correct horse battery") {
		t.Fatal("Grace cannot sign in")
	}
	_, body = call(t, support, "GET", page, "", nil)
	if !strings.Contains(body, "Hannah Pryce") || strings.Contains(body, `name="placing"`) || strings.Contains(body, "data-post=") || strings.Contains(body, "data-kind=") {
		t.Error("someone in support is offered the controls, or doesn't see who signs")
	}

	res, err := http.Get(srv + "/assets/js/pdf.worker.min.mjs")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK || !strings.Contains(res.Header.Get("Content-Type"), "javascript") {
		t.Errorf("pdf.js's worker: %d %q", res.StatusCode, res.Header.Get("Content-Type"))
	}
}

// A PDF Prospector couldn't later fill in and seal is refused when it is uploaded.
func TestAnAgreementRefusesAPDFItCouldntSeal(t *testing.T) {
	_, _, base, member, _, _, _ := preparing(t)
	signed := testPDF(1, "<< /Type /Sig /ByteRange [0 10 20 30] >>")
	var encrypted bytes.Buffer
	if err := api.Encrypt(context.Background(), bytes.NewReader(testPDF(1, "")), &encrypted, model.NewAESConfiguration("open sesame", "owner secret", 256)); err != nil {
		t.Fatal(err)
	}
	for why, data := range map[string][]byte{
		"an encrypted PDF":                  encrypted.Bytes(),
		"an already signed PDF":             signed,
		"a PDF with no pages":               testPDF(0, ""),
		"a PDF that is only its first line": []byte("%PDF-1.7\n"),
	} {
		if code, body, _ := upload(t, member, base+"/document", "", "application/pdf", data); code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", why, code, body)
		}
	}
}
