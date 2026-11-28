package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/shutthegoatup/prospector/api"
	"github.com/shutthegoatup/prospector/internal/crm"
	"github.com/shutthegoatup/prospector/internal/signing"
)

var samplePDF = testPDF(2, "")

// testPDF is a well-formed PDF of blank A4 pages, with a comment line carrying note.
func testPDF(pages int, note string) []byte { return buildPDF("1.7", pages, note, "") }

// buildPDF is testPDF with the version it claims and something more on each page.
func buildPDF(version string, pages int, note, onPage string) []byte {
	var b bytes.Buffer
	b.WriteString("%PDF-" + version + "\n")
	if note != "" {
		b.WriteString("% " + note + "\n")
	}
	var offsets []int
	obj := func(body string) {
		offsets = append(offsets, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", len(offsets), body)
	}
	obj("<< /Type /Catalog /Pages 2 0 R >>")
	kids := ""
	for i := range pages {
		kids += fmt.Sprintf("%d 0 R ", 3+i)
	}
	obj(fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", kids, pages))
	for range pages {
		obj("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] " + onPage + ">>")
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(offsets)+1)
	for _, o := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets)+1, xref)
	return b.Bytes()
}

// upload sends raw bytes as the agreement's document, as a key or a member's browser does.
func upload(t *testing.T, c *http.Client, url, key, contentType string, body []byte) (int, string, http.Header) {
	t.Helper()
	req, _ := http.NewRequest("PUT", url, bytes.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	res, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	out, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(out), res.Header
}

func TestAnAgreementKeepsItsDocumentAsUploaded(t *testing.T) {
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
	reader, writer, stranger := key(o.ID, "read"), key(o.ID, "agreements:write"), key(other.ID, "read", "agreements:write")
	company := *must(s.QuickAddPerson(o, crm.QuickAdd{Name: "Hannah Pryce", Company: "Larkmoor Yachts"}, "Ada Lovelace")).CompanyID
	list := srv.URL + "/o/alpha/api/v1/agreements"

	if code, body := asKey(t, "POST", list, reader, map[string]any{"company": company, "name": "Service agreement"}); code != http.StatusForbidden || !strings.Contains(body, "agreements:write") {
		t.Errorf("a read key starting an agreement: %d %s", code, body)
	}
	if code, _ := asKey(t, "POST", list, writer, map[string]any{"company": company, "name": "  "}); code != http.StatusBadRequest {
		t.Errorf("an agreement with no name: %d", code)
	}
	if code, _ := asKey(t, "POST", srv.URL+"/o/beta/api/v1/agreements", stranger, map[string]any{"company": company, "name": "Theirs"}); code != http.StatusNotFound {
		t.Errorf("another organisation starting one for our company: %d", code)
	}
	code, body := asKey(t, "POST", list, writer, map[string]any{"company": company, "name": "Annual service  agreement"})
	if code != http.StatusCreated || !strings.Contains(body, `"name":"Annual service agreement"`) || !strings.Contains(body, `"status":"draft"`) || !strings.Contains(body, `"file_hash":""`) {
		t.Fatalf("starting an agreement: %d %s", code, body)
	}
	a := must(s.Agreements(o.ID, 0))[0]
	doc := list + "/" + itoa(a.ID) + "/document"

	if code, _, _ := upload(t, http.DefaultClient, doc+"?name=a.pdf", writer, "application/pdf", []byte("<html><script>alert(1)</script>")); code != http.StatusBadRequest {
		t.Errorf("something that isn't a PDF: %d", code)
	}
	if code, _, _ := upload(t, http.DefaultClient, doc, writer, "application/pdf", append([]byte("%PDF-"), make([]byte, signing.AgreementFileLimit)...)); code != http.StatusRequestEntityTooLarge {
		t.Errorf("a document past the limit: %d", code)
	}
	if code, _, _ := upload(t, http.DefaultClient, doc, reader, "application/pdf", samplePDF); code != http.StatusForbidden {
		t.Errorf("a read key uploading: %d", code)
	}
	code, body, _ = upload(t, http.DefaultClient, doc+"?name="+`..%2F..%2Fsecret%22%0D%0AX-Evil%3A%201`, writer, "application/pdf", samplePDF)
	sum := sha256.Sum256(samplePDF)
	if code != http.StatusOK || !strings.Contains(body, hex.EncodeToString(sum[:])) || !strings.Contains(body, `"file_size":`+itoa(int64(len(samplePDF)))) {
		t.Fatalf("uploading the document: %d %s", code, body)
	}
	if now := must(s.Agreement(o.ID, a.ID)); strings.ContainsAny(now.FileName, "/\r\n\"") || !strings.HasSuffix(now.FileName, ".pdf") {
		t.Errorf("the file name was kept as %q", now.FileName)
	}

	// It comes back exactly as it went, always as a download.
	req, _ := http.NewRequest("GET", doc, nil)
	req.Header.Set("Authorization", "Bearer "+reader)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || !bytes.Equal(got, samplePDF) || res.Header.Get("Content-Type") != "application/pdf" ||
		!strings.HasPrefix(res.Header.Get("Content-Disposition"), "attachment") || res.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("downloading: %d, %q, %q, %d bytes", res.StatusCode, res.Header.Get("Content-Type"), res.Header.Get("Content-Disposition"), len(got))
	}
	if strings.ContainsAny(res.Header.Get("Content-Disposition"), "\r\n") || len(res.Header.Values("X-Evil")) > 0 {
		t.Error("the file name reached the headers as it was sent")
	}
	if code, _, _ := upload(t, http.DefaultClient, srv.URL+"/o/beta/api/v1/agreements/"+itoa(a.ID)+"/document", stranger, "application/pdf", samplePDF); code != http.StatusNotFound {
		t.Errorf("another organisation replacing our document by number: %d", code)
	}
	if code, _ := asKey(t, "GET", srv.URL+"/o/beta/api/v1/agreements/"+itoa(a.ID)+"/document", stranger, nil); code != http.StatusNotFound {
		t.Errorf("another organisation downloading our document by number: %d", code)
	}

	// A document changed behind Prospector's back is not handed out.
	if err := s.DB.Model(&AgreementFile{}).Where("agreement_id = ?", a.ID).Update("data", testPDF(2, "tampered")).Error; err != nil {
		t.Fatal(err)
	}
	if code, body := asKey(t, "GET", doc, reader, nil); code != http.StatusInternalServerError || strings.Contains(body, "tampered") {
		t.Errorf("a document that doesn't match its hash: %d %s", code, body)
	}

	// Renamed, listed by company, and removed with its document.
	if code, body := asKey(t, "PATCH", list+"/"+itoa(a.ID), writer, map[string]any{"name": "Annual service agreement 2027"}); code != http.StatusOK || !strings.Contains(body, "2027") {
		t.Errorf("renaming: %d %s", code, body)
	}
	if code, body := asKey(t, "GET", list+"?company="+itoa(company), reader, nil); code != http.StatusOK || !strings.Contains(body, "2027") {
		t.Errorf("listing a company's: %d %s", code, body)
	}
	if code, body := asKey(t, "DELETE", srv.URL+"/o/beta/api/v1/agreements/"+itoa(a.ID), stranger, nil); code != http.StatusNotFound {
		t.Errorf("another organisation removing ours: %d %s", code, body)
	}
	if export := must(s.ExportOrg(o.ID, "Ada Lovelace")); len(export.Agreements) != 1 || export.Agreements[0].FileHash == "" {
		t.Errorf("the organisation's export of agreements: %+v", export.Agreements)
	}
	if code, body := call(t, member, "DELETE", list+"/"+itoa(a.ID), "", nil); code != http.StatusNoContent {
		t.Errorf("removing: %d %s", code, body)
	}
	var left int64
	if err := s.DB.Model(&AgreementFile{}).Count(&left).Error; err != nil || left != 0 {
		t.Errorf("removing left %d documents (%v)", left, err)
	}
}

func TestAnAgreementIsWorkedFromItsCompanysPage(t *testing.T) {
	t.Parallel()
	s, o, srv, member := signedIn(t)
	company := *must(s.QuickAddPerson(o, crm.QuickAdd{Name: "Hannah Pryce", Company: "Larkmoor Yachts"}, "Ada Lovelace")).CompanyID
	if code, body := call(t, member, "POST", srv.URL+"/o/alpha/api/v1/agreements", "application/json", map[string]any{"company": company, "name": "Service agreement"}); code != http.StatusCreated {
		t.Fatalf("starting from the page: %d %s", code, body)
	}
	a := must(s.Agreements(o.ID, company))[0]
	doc := srv.URL + "/o/alpha/api/v1/agreements/" + itoa(a.ID) + "/document"
	// The page's own upload: a PUT of the PDF with the member's session.
	if code, body, _ := upload(t, member, doc+"?name=service.pdf", "", "application/pdf", samplePDF); code != http.StatusOK {
		t.Fatalf("uploading from the page: %d %s", code, body)
	}
	shown := companyPageOverview(t, member, srv.URL, company).Agreements
	if len(shown) != 1 || shown[0].Id != a.ID || shown[0].Name != "Service agreement" || shown[0].FileName != "service.pdf" || len(shown[0].FileHash) != 64 {
		t.Errorf("the company's page shows its agreements as %+v", shown)
	}
	if code, body := call(t, member, "GET", doc, "", nil); code != http.StatusOK || !strings.HasPrefix(body, "%PDF") {
		t.Errorf("downloading the document from the page: %d", code)
	}
	// Another site can't make a member's browser replace it.
	req, _ := http.NewRequest("PUT", doc, bytes.NewReader(samplePDF))
	req.Header.Set("Content-Type", "application/pdf")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.Header.Set("Origin", "https://elsewhere.example")
	res, err := member.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Errorf("a cross-site upload with a member's session: %d", res.StatusCode)
	}
}

// What the review of documents found: a body cleaned of what looked like a NUL, a type
// not checked, and names that hide what they are.
func TestADocumentIsKeptByteForByte(t *testing.T) {
	t.Parallel()
	s, o, srv, member := signedIn(t)
	company := *must(s.QuickAddPerson(o, crm.QuickAdd{Name: "Hannah Pryce", Company: "Larkmoor Yachts"}, "Ada Lovelace")).CompanyID
	a := must(s.AddAgreement(o.ID, company, "API agreement", "Ada Lovelace"))
	doc := srv.URL + "/o/alpha/api/v1/agreements/" + itoa(a.ID) + "/document"
	quoting := testPDF(1, "(the API sends \\u0000 for a NUL) Tj")
	if code, body, _ := upload(t, member, doc, "", "application/pdf", quoting); code != http.StatusOK {
		t.Fatalf("uploading: %d %s", code, body)
	}
	_, data := must2(s.AgreementDocument(o.ID, a.ID))
	if !bytes.Equal(data, quoting) {
		t.Errorf("the document was changed on the way in:\n%q\n%q", quoting, data)
	}
	for _, kind := range []string{"application/json", "text/html", ""} {
		if code, _, _ := upload(t, member, doc, "", kind, samplePDF); code != http.StatusUnsupportedMediaType {
			t.Errorf("a document sent as %q: %d", kind, code)
		}
	}
	for given, want := range map[string]string{"..": "agreement.pdf", "evil\u202efdp.exe": "evilfdp.exe.pdf", "Contract.PDF": "Contract.PDF"} {
		if got := signing.FileName(given); got != want {
			t.Errorf("a file named %q is kept as %q, want %q", given, got, want)
		}
	}
}

func must2[A, B any](a A, b B, err error) (A, B) {
	if err != nil {
		panic(err)
	}
	return a, b
}

// Someone who works only in support sees a company's agreements, and none of the controls
// that change them, which are sales work.
func TestOnlySalesWorksAgreements(t *testing.T) {
	t.Parallel()
	s, o, srv, _ := signedIn(t)
	company := *must(s.QuickAddPerson(o, crm.QuickAdd{Name: "Hannah Pryce", Company: "Larkmoor Yachts"}, "Ada Lovelace")).CompanyID
	a := must(s.AddAgreement(o.ID, company, "Service agreement", "Ada Lovelace"))
	grace := must(s.CreateUser("grace@example.com", "Grace Hopper", "correct horse battery"))
	if err := s.AddMember(o.ID, grace.ID, "member", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAreas(o.ID, grace.ID, []string{"support"}); err != nil {
		t.Fatal(err)
	}
	support := newMailClient()
	if !signIn(t, support, srv, "grace@example.com", "correct horse battery") {
		t.Fatal("Grace cannot sign in")
	}
	if shown := companyPageOverview(t, support, srv.URL, company).Agreements; len(shown) != 1 || shown[0].Name != "Service agreement" {
		t.Errorf("someone in support doesn't see the agreement: %+v", shown)
	}
	if code, _ := call(t, support, "POST", srv.URL+"/o/alpha/api/v1/agreements", "application/json", map[string]any{"company": company, "name": "Another"}); code != http.StatusForbidden {
		t.Errorf("someone in support starting an agreement: %d", code)
	}
	if code, _, _ := upload(t, support, srv.URL+"/o/alpha/api/v1/agreements/"+itoa(a.ID)+"/document", "", "application/pdf", samplePDF); code != http.StatusForbidden {
		t.Errorf("someone in support uploading: %d", code)
	}
}

// companyPageOverview is what a company's page reads about it.
func companyPageOverview(t *testing.T, c *http.Client, base string, company int64) api.CompanyOverview {
	t.Helper()
	code, body := call(t, c, "GET", base+"/o/alpha/api/v1/companies/"+itoa(company)+"/overview", "", nil)
	var out api.CompanyOverview
	if code != http.StatusOK || json.Unmarshal([]byte(body), &out) != nil {
		t.Fatalf("the company's overview: %d %s", code, body)
	}
	return out
}
