package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"strings"
	"testing"
)

var samplePDF = []byte("%PDF-1.7\n1 0 obj << /Type /Catalog >> endobj\ntrailer << /Root 1 0 R >>\n%%EOF\n")

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
	company := *must(s.QuickAddPerson(o, QuickAdd{Name: "Hannah Pryce", Company: "Larkmoor Yachts"}, "Ada Lovelace")).CompanyID
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
	if code, _, _ := upload(t, http.DefaultClient, doc, writer, "application/pdf", append([]byte("%PDF-"), make([]byte, agreementFileLimit)...)); code != http.StatusBadRequest {
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
	if err := s.db.Model(&AgreementFile{}).Where("agreement_id = ?", a.ID).Update("data", []byte("%PDF-1.7 tampered")).Error; err != nil {
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
	if err := s.db.Model(&AgreementFile{}).Count(&left).Error; err != nil || left != 0 {
		t.Errorf("removing left %d documents (%v)", left, err)
	}
}

func TestAnAgreementIsWorkedFromItsCompanysPage(t *testing.T) {
	s, o, srv, member := signedIn(t)
	company := *must(s.QuickAddPerson(o, QuickAdd{Name: "Hannah Pryce", Company: "Larkmoor Yachts"}, "Ada Lovelace")).CompanyID
	if code, body := call(t, member, "POST", srv.URL+"/o/alpha/api/v1/agreements", "application/json", map[string]any{"company": company, "name": "Service agreement"}); code != http.StatusCreated {
		t.Fatalf("starting from the page: %d %s", code, body)
	}
	a := must(s.Agreements(o.ID, company))[0]
	doc := srv.URL + "/o/alpha/api/v1/agreements/" + itoa(a.ID) + "/document"
	// The page's own upload: a PUT of the PDF with the member's session.
	if code, body, _ := upload(t, member, doc+"?name=service.pdf", "", "application/pdf", samplePDF); code != http.StatusOK {
		t.Fatalf("uploading from the page: %d %s", code, body)
	}
	_, page := call(t, member, "GET", srv.URL+"/o/alpha/companies/"+itoa(company), "", nil)
	for _, want := range []string{"Agreements", "Service agreement", "service.pdf", "SHA-256 ", `data-upload="` + itoa(a.ID) + `"`, "/document", `data-form="api/v1/agreements"`} {
		if !strings.Contains(page, want) {
			t.Errorf("the company's page lacks %q", want)
		}
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
