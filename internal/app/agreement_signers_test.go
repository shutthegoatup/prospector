package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"

	"github.com/shutthegoatup/prospector/internal/crm"
	"github.com/shutthegoatup/prospector/internal/identity"
	"github.com/shutthegoatup/prospector/internal/request"
	"github.com/shutthegoatup/prospector/internal/signing"
	"github.com/shutthegoatup/prospector/internal/store"
)

// preparing starts a draft for Larkmoor with a two-page document, and gives what a
// test of its signers needs.
func preparing(t *testing.T) (s *Store, o *store.Org, base string, member *http.Client, a *store.Agreement, hannah, theo *store.Contact) {
	t.Helper()
	s, o, srv, member := signedIn(t)
	hannah = must(s.QuickAddPerson(o, crm.QuickAdd{Name: "Hannah Pryce", Company: "Larkmoor Yachts", Email: "hannah@larkmoor.example"}, "Ada Lovelace"))
	theo = must(s.QuickAddPerson(o, crm.QuickAdd{Name: "Theo Marsh", Company: "Larkmoor Yachts", Email: "theo@larkmoor.example"}, "Ada Lovelace"))
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
	t.Parallel()
	s, o, base, member, a, hannah, theo := preparing(t)
	recipients := base + "/recipients"
	ada := must(s.UserByEmail("ada@example.com"))

	if got := must(s.Agreement(o.ID, a.ID)); got.PageCount != 2 || got.Round != 1 || !got.InOrder {
		t.Errorf("a new draft with its document: %d pages, round %d, in order %v", got.PageCount, got.Round, got.InOrder)
	}

	elsewhere := must(s.QuickAddPerson(o, crm.QuickAdd{Name: "Rhys Doyle", Company: "Ferncliff Marine", Email: "rhys@ferncliff.example"}, "Ada Lovelace"))
	noAddress := must(s.QuickAddPerson(o, crm.QuickAdd{Name: "Mo Lind", Company: "Larkmoor Yachts"}, "Ada Lovelace"))
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
	list := items[signing.Party](t, body)
	if len(list) != 3 || list[0].Name != "Hannah Pryce" || list[0].Email != "hannah@larkmoor.example" || list[1].Name != "Ada Lovelace" || list[1].Email != "ada@example.com" || list[2].Role != signing.RecipientCopy {
		t.Fatalf("who it goes to: %+v", list)
	}

	// Ada moves ahead of Hannah, then the never-contact list catches Larkmoor.
	if code, out := call(t, member, "PATCH", recipients+"/"+itoa(list[1].ID), "application/json", map[string]any{"position": 1}); code != http.StatusOK || !strings.Contains(out, `"position":1`) {
		t.Errorf("moving Ada first: %d %s", code, out)
	}
	_, body = call(t, member, "GET", recipients, "", nil)
	if list := items[signing.Party](t, body); list[0].Name != "Ada Lovelace" || list[1].Name != "Hannah Pryce" {
		t.Errorf("the order after moving: %+v", list)
	}
	ines := must(s.QuickAddPerson(o, crm.QuickAdd{Name: "Ines Ward", Company: "Larkmoor Yachts", Email: "ines@larkmoor.example"}, "Ada Lovelace"))
	o.Settings.NeverContact = "ines@larkmoor.example"
	if err := s.SaveOrg(o); err != nil {
		t.Fatal(err)
	}
	if code, _ := call(t, member, "POST", recipients, "application/json", map[string]any{"role": "signer", "contact": ines.ID}); code != http.StatusBadRequest {
		t.Errorf("someone on the never-contact list was added: %d", code)
	}
}

func TestFieldsLieOnTheDocumentForItsSigners(t *testing.T) {
	t.Parallel()
	s, o, base, member, a, hannah, theo := preparing(t)
	signer := must(s.AddRecipient(o, a.ID, signing.RecipientSigner, &hannah.ID, nil, "Ada Lovelace"))
	copied := must(s.AddRecipient(o, a.ID, signing.RecipientCopy, &theo.ID, nil, "Ada Lovelace"))
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
	if code, out := put(field(signer.ID, 2, "signature", 0.1, 0.7, 0.35, 0.06), field(signer.ID, 2, "date", 0.1, 0.8, 0.2, 0.03)); code != http.StatusOK || len(items[store.AgreementField](t, out)) != 2 {
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
// Once an agreement has gone out, who it went to is part of its record: erasing them,
// removing a member or a restore without them detaches them and leaves the row.
func TestAnAgreementThatWentOutKeepsWhoItWentTo(t *testing.T) {
	t.Parallel()
	s, o, _, _, a, hannah, _ := preparing(t)
	grace := must(s.CreateUser("grace@example.com", "Grace Hopper", "correct horse battery"))
	if err := s.AddMember(o.ID, grace.ID, "member", "Ada Lovelace", "Owner One"); err != nil {
		t.Fatal(err)
	}
	h := must(s.AddRecipient(o, a.ID, signing.RecipientSigner, &hannah.ID, nil, "Ada Lovelace"))
	g := must(s.AddRecipient(o, a.ID, signing.RecipientSigner, nil, &grace.ID, "Ada Lovelace"))
	must(s.PutAgreementFields(o.ID, a.ID, []store.AgreementField{{RecipientID: h.ID, Page: 1, Kind: "signature", X: 0.1, Y: 0.1, W: 0.3, H: 0.05}}))
	draft := must(s.AddAgreement(o.ID, *hannah.CompanyID, "Still a draft", "Ada Lovelace"))
	must(s.AddRecipient(o, draft.ID, signing.RecipientSigner, &hannah.ID, nil, "Ada Lovelace"))
	if err := s.DB.Model(&store.Agreement{}).Where("id = ?", a.ID).Update("status", "sent").Error; err != nil {
		t.Fatal(err)
	}

	if err := s.ErasePerson(o.ID, hannah.ID, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RemoveMember(o.ID, grace.ID, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	var rows []store.AgreementRecipient
	if err := s.DB.Where("agreement_id = ?", a.ID).Order("id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].ID != h.ID || rows[0].ContactID != nil || rows[1].ID != g.ID || rows[1].UserID != nil {
		t.Errorf("who a sent agreement went to, after erasing and removing them: %+v", rows)
	}
	var fields int64
	if err := s.DB.Model(&store.AgreementField{}).Where("agreement_id = ?", a.ID).Count(&fields).Error; err != nil || fields != 1 {
		t.Errorf("the sent agreement's fields: %d (%v)", fields, err)
	}
	if left := must(s.AgreementRecipients(o.ID, draft.ID)); len(left) != 0 {
		t.Errorf("the draft kept the erased person: %+v", left)
	}
}

// An operation that names no key scope is for people signed in: a key is refused,
// whatever scopes it has.
func TestAnOperationWithoutAKeyScopeRefusesKeys(t *testing.T) {
	t.Parallel()
	a := &App{s: tempStore(t)}
	ran := false
	handler := a.apiScope(map[string][]string{})(func(context.Context, http.ResponseWriter, *http.Request, any) (any, error) {
		ran = true
		return nil, nil
	}, "SomethingForPeopleOnly")
	var all []string
	for _, sc := range identity.Scopes {
		all = append(all, sc.Name)
	}
	ctx := context.WithValue(context.Background(), request.ReqKey{}, &request.Req{Key: &store.APIKey{Scopes: strings.Join(all, " ")}})
	w := httptest.NewRecorder()
	if _, err := handler(ctx, w, httptest.NewRequest(http.MethodPost, "/", nil), nil); err != nil {
		t.Fatal(err)
	}
	if ran || w.Code != http.StatusForbidden {
		t.Errorf("a key with every scope on an operation that names none: ran %v, %d", ran, w.Code)
	}
}

func TestAnAgreementsRecipientsGoWithWhoTheyStandFor(t *testing.T) {
	t.Parallel()
	s, o, _, _, a, hannah, theo := preparing(t)
	grace := must(s.CreateUser("grace@example.com", "Grace Hopper", "correct horse battery"))
	if err := s.AddMember(o.ID, grace.ID, "member", "Ada Lovelace", "Owner One"); err != nil {
		t.Fatal(err)
	}
	h := must(s.AddRecipient(o, a.ID, signing.RecipientSigner, &hannah.ID, nil, "Ada Lovelace"))
	g := must(s.AddRecipient(o, a.ID, signing.RecipientSigner, nil, &grace.ID, "Ada Lovelace"))
	must(s.AddRecipient(o, a.ID, signing.RecipientSigner, &theo.ID, nil, "Ada Lovelace"))
	must(s.PutAgreementFields(o.ID, a.ID, []store.AgreementField{
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
	if err := s.RemoveAgreement(o.ID, a.ID, "Owner One"); err != nil {
		t.Fatal(err)
	}
	var n int64
	if err := s.DB.Model(&store.AgreementRecipient{}).Count(&n).Error; err != nil || n != 0 {
		t.Errorf("removing the draft left %d recipients (%v)", n, err)
	}
}

func TestAKeyPreparesAnAgreementWithTheWriteScope(t *testing.T) {
	t.Parallel()
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
// reads it. It is the app's shell, and pdf.js's worker is served as script.
func TestAnAgreementIsPreparedOnItsOwnPage(t *testing.T) {
	t.Parallel()
	s, o, base, member, a, hannah, theo := preparing(t)
	must(s.AddRecipient(o, a.ID, signing.RecipientSigner, &hannah.ID, nil, "Ada Lovelace"))
	must(s.AddRecipient(o, a.ID, signing.RecipientCopy, &theo.ID, nil, "Ada Lovelace"))
	// Ada signs after Hannah, with Theo's copy between them in the order.
	ada := must(s.AddRecipient(o, a.ID, signing.RecipientSigner, nil, &must(s.UserByEmail("ada@example.com")).ID, "Ada Lovelace"))
	srv := strings.TrimSuffix(base, "/o/alpha/api/v1/agreements/"+itoa(a.ID))
	page := srv + "/o/alpha/agreements/" + itoa(a.ID) + "/prepare"

	if code, body := call(t, member, "GET", page, "", nil); code != http.StatusOK || !strings.Contains(body, `id="app"`) {
		t.Fatalf("the page: %d", code)
	}
	order := func(c *http.Client) string {
		t.Helper()
		code, body := call(t, c, "GET", base+"/recipients", "", nil)
		if code != http.StatusOK {
			t.Fatalf("who signs: %d %s", code, body)
		}
		var names []string
		for _, r := range items[struct {
			Position int
			Name     string
			Role     string
		}](t, body) {
			names = append(names, itoa(int64(r.Position))+" "+r.Name+" "+r.Role)
		}
		slices.Sort(names)
		return strings.Join(names, ", ")
	}
	if got := order(member); got != "1 Hannah Pryce signer, 2 Theo Marsh copy, 3 Ada Lovelace signer" {
		t.Errorf("who signs, in order: %s", got)
	}
	// Ada moves to first, ahead of Hannah.
	if code, body := call(t, member, "PATCH", base+"/recipients/"+itoa(ada.ID), "application/json", map[string]any{"position": 1}); code != http.StatusOK {
		t.Fatalf("moving Ada up: %d %s", code, body)
	}
	if got := order(member); got != "1 Ada Lovelace signer, 2 Hannah Pryce signer, 3 Theo Marsh copy" {
		t.Errorf("after moving Ada up: %s", got)
	}
	// The company's page leads to Prepare for signing for an agreement with its document.
	if shown := companyPageOverview(t, member, srv, *hannah.CompanyID).Agreements; len(shown) != 1 || shown[0].Id != a.ID || shown[0].FileHash == "" {
		t.Errorf("the company's page should offer to prepare the agreement: %+v", shown)
	}

	grace := must(s.CreateUser("grace@example.com", "Grace Hopper", "correct horse battery"))
	if err := s.AddMember(o.ID, grace.ID, "member", "Ada Lovelace", "Owner One"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAreas(o.ID, grace.ID, []string{"support"}); err != nil {
		t.Fatal(err)
	}
	support := newMailClient()
	if !signIn(t, support, &httptest.Server{URL: srv}, "grace@example.com", "correct horse battery") {
		t.Fatal("Grace cannot sign in")
	}
	if got := order(support); !strings.Contains(got, "Hannah Pryce") {
		t.Errorf("someone in support doesn't see who signs: %s", got)
	}
	if code, _ := call(t, support, "PATCH", base+"/recipients/"+itoa(ada.ID), "application/json", map[string]any{"position": 3}); code != http.StatusForbidden {
		t.Errorf("someone in support changing who signs: %d", code)
	}
	if code, _ := call(t, support, "POST", base+"/recipients", "application/json", map[string]any{"role": "copy", "contact_id": theo.ID}); code != http.StatusForbidden {
		t.Errorf("someone in support adding a signer: %d", code)
	}

	// pdf.js compiles WebAssembly to draw JPEG 2000 images, so the page and its worker
	// allow that, and only that: evaluating script stays refused, and other pages are
	// as strict as ever.
	policy := func(c *http.Client, u string) string {
		t.Helper()
		res, err := c.Get(u)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("GET %s: %d", u, res.StatusCode)
		}
		if strings.HasSuffix(u, ".mjs") && !strings.Contains(res.Header.Get("Content-Type"), "javascript") {
			t.Errorf("pdf.js's worker is served as %q", res.Header.Get("Content-Type"))
		}
		return res.Header.Get("Content-Security-Policy")
	}
	for _, u := range []string{page, srv + "/assets/js/pdf.worker.min.mjs"} {
		if got := policy(member, u); got != strings.Replace(contentPolicy, "script-src 'self';", "script-src 'self' 'wasm-unsafe-eval';", 1) {
			t.Errorf("%s has the policy %q", u, got)
		}
	}
	if got := policy(member, srv+"/o/alpha/companies/"+itoa(*hannah.CompanyID)); got != contentPolicy {
		t.Errorf("the company's page has the policy %q", got)
	}
}

// Real PDFs break rules of their own version that don't stop them being filled in: one
// claiming 1.3 with an annotation 1.4 brought is taken, its pages counted.
func TestAnAgreementTakesAPDFThatBendsItsVersionsRules(t *testing.T) {
	t.Parallel()
	s, o, base, member, a, _, _ := preparing(t)
	bent := buildPDF("1.3", 3, "", "/Annots [<< /Type /Annot /Subtype /Caret /Rect [10 10 20 20] >>] ")
	if code, body, _ := upload(t, member, base+"/document?name=form.pdf", "", "application/pdf", bent); code != http.StatusOK {
		t.Fatalf("a PDF that bends its version's rules: %d %s", code, body)
	}
	if got := must(s.Agreement(o.ID, a.ID)); got.PageCount != 3 {
		t.Errorf("its pages: %d", got.PageCount)
	}
}

// The React Prepare page reads one agreement with what preparing it needs: its company,
// any problem with its document, and who could still be added. It is served with the
// policy pdf.js needs; other React pages keep the strict one.
func TestTheReactPreparePageReadsItsAgreementThroughTheAPI(t *testing.T) {
	t.Parallel()
	s, o, base, member, a, hannah, theo := preparing(t)
	must(s.AddRecipient(o, a.ID, signing.RecipientSigner, &hannah.ID, nil, "Ada Lovelace"))
	srv := strings.TrimSuffix(base, "/o/alpha/api/v1/agreements/"+itoa(a.ID))

	code, body := call(t, member, "GET", base, "", nil)
	if code != http.StatusOK {
		t.Fatalf("reading the agreement: %d %s", code, body)
	}
	var got struct {
		Company, Problem string
		PageCount        int `json:"page_count"`
		People, Members  []struct {
			ID    int64
			Name  string
			Email string
		}
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatal(err)
	}
	if got.Company != "Larkmoor Yachts" || got.Problem != "" || got.PageCount != 2 {
		t.Errorf("the agreement: %+v", got)
	}
	if len(got.People) != 1 || got.People[0].ID != theo.ID || got.People[0].Email != "theo@larkmoor.example" {
		t.Errorf("people who could still be added, Hannah being on it: %+v", got.People)
	}
	if len(got.Members) != 1 || got.Members[0].Email != "ada@example.com" {
		t.Errorf("members who could be added: %+v", got.Members)
	}
	reader, _, err := s.CreateKey(o.ID, "reader", []string{"read"}, "Ada Lovelace")
	if err != nil {
		t.Fatal(err)
	}
	if code, _ := asKey(t, "GET", base, reader, nil); code != http.StatusOK {
		t.Errorf("a read key reading the agreement: %d", code)
	}
	if code, _ := asKey(t, "GET", srv+"/o/alpha/api/v1/agreements/99999", reader, nil); code != http.StatusNotFound {
		t.Errorf("an agreement that isn't there: %d", code)
	}

	policy := func(u string) string {
		t.Helper()
		res, err := member.Get(u)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("GET %s: %d", u, res.StatusCode)
		}
		return res.Header.Get("Content-Security-Policy")
	}
	if got := policy(srv + "/o/alpha/app/agreements/" + itoa(a.ID) + "/prepare"); got != pdfPolicy {
		t.Errorf("the React Prepare page has the policy %q", got)
	}
	if got := policy(srv + "/o/alpha/app/companies"); got != contentPolicy {
		t.Errorf("another React page has the policy %q", got)
	}
}

// A PDF Prospector couldn't later fill in and seal is refused when it is uploaded.
func TestAnAgreementRefusesAPDFItCouldntSeal(t *testing.T) {
	t.Parallel()
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

// TestAgreementsSavedBeforePagesWereCountedArePrepared: an agreement saved before its
// pages were counted has them counted when first needed, and one whose document can't
// be read says so until it is uploaded again.
func TestAgreementsSavedBeforePagesWereCountedArePrepared(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	hannah := must(s.QuickAddPerson(o, crm.QuickAdd{Name: "Hannah Pryce", Company: "Larkmoor Yachts", Email: "hannah@larkmoor.example"}, "Ada Lovelace"))
	readable := must(s.AddAgreement(o.ID, *hannah.CompanyID, "Service agreement", "Ada Lovelace"))
	must(s.PutAgreementFile(o.ID, readable.ID, "service.pdf", testPDF(2, ""), "Ada Lovelace"))
	legacy := must(s.AddAgreement(o.ID, *hannah.CompanyID, "Old agreement", "Ada Lovelace"))
	scrap := []byte("%PDF-1.7 only its first bytes were ever checked")
	sum := sha256.Sum256(scrap)
	if err := s.DB.Create(&store.AgreementFile{AgreementID: legacy.ID, OrgID: o.ID, Data: scrap}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Model(&store.Agreement{}).Where("id = ?", legacy.ID).Updates(map[string]any{"file_name": "old.pdf", "file_hash": hex.EncodeToString(sum[:])}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB.Model(&store.Agreement{}).Where("org_id = ?", o.ID).Update("page_count", 0).Error; err != nil {
		t.Fatal(err)
	}

	r := must(s.AddRecipient(o, readable.ID, signing.RecipientSigner, &hannah.ID, nil, "Ada Lovelace"))
	if _, err := s.PutAgreementFields(o.ID, readable.ID, []store.AgreementField{{RecipientID: r.ID, Page: 2, Kind: "signature", X: 0.1, Y: 0.8, W: 0.3, H: 0.05}}); err != nil {
		t.Fatalf("placing a field on an agreement whose pages weren't counted: %v", err)
	}
	if a := must(s.Agreement(o.ID, readable.ID)); a.PageCount != 2 {
		t.Errorf("its pages, counted when first needed: %d", a.PageCount)
	}

	l := must(s.AddRecipient(o, legacy.ID, signing.RecipientSigner, &hannah.ID, nil, "Ada Lovelace"))
	field := []store.AgreementField{{RecipientID: l.ID, Page: 1, Kind: "signature", X: 0.1, Y: 0.8, W: 0.3, H: 0.05}}
	if _, err := s.PutAgreementFields(o.ID, legacy.ID, field); err == nil || !strings.Contains(err.Error(), "can't be read") {
		t.Errorf("fields on a document that can't be read: %v", err)
	}
	must(s.PutAgreementFile(o.ID, legacy.ID, "old.pdf", testPDF(1, ""), "Ada Lovelace"))
	if _, err := s.PutAgreementFields(o.ID, legacy.ID, field); err != nil {
		t.Errorf("fields once it is uploaded again: %v", err)
	}
}
