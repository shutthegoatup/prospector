package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"gorm.io/gorm"
)

func TestAnAgreementsEventsShowAnyChangeAfterTheFact(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	add := func(kind, detail string) {
		t.Helper()
		if err := s.DB.Transaction(func(tx *gorm.DB) error {
			_, err := recordAgreementEvent(tx, AgreementEvent{OrgID: o.ID, AgreementID: 7, Round: 1, Kind: kind, Actor: "Ada Lovelace", Detail: detail, IP: "203.0.113.5"})
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	add(EventSent, "to 2 signers")
	add(EventOpened, "")
	add(EventSigned, "as Hannah Pryce")
	events := must(s.AgreementEvents(o.ID, 7))
	if len(events) != 3 || events[0].Prev != "" || events[1].Prev != events[0].Hash || events[2].Prev != events[1].Hash {
		t.Fatalf("the chain: %+v", events)
	}

	// Two writers adding after the same event can't both succeed.
	fork := events[2]
	fork.ID, fork.Kind = 0, EventDeclined
	if err := s.DB.Create(&fork).Error; err == nil {
		t.Error("a second event after the same one was kept")
	}

	for what, change := range map[string]func(*gorm.DB) error{
		"a detail rewritten": func(tx *gorm.DB) error {
			return tx.Model(&AgreementEvent{}).Where("id = ?", events[1].ID).Update("detail", "nothing to see").Error
		},
		"an event taken out": func(tx *gorm.DB) error {
			return tx.Delete(&AgreementEvent{}, events[1].ID).Error
		},
		"a time moved": func(tx *gorm.DB) error {
			return tx.Model(&AgreementEvent{}).Where("id = ?", events[2].ID).Update("at", events[2].At.Add(-time.Hour)).Error
		},
	} {
		err := s.DB.Transaction(func(tx *gorm.DB) error {
			if err := change(tx); err != nil {
				return err
			}
			var all []AgreementEvent
			if err := tx.Where("agreement_id = ?", 7).Order("id").Find(&all).Error; err != nil {
				return err
			}
			if err := checkChain(all); !errors.Is(err, errBrokenChain) {
				t.Errorf("%s went unnoticed: %v", what, err)
			}
			return errors.New("undo")
		})
		if err == nil || err.Error() != "undo" {
			t.Fatal(err)
		}
	}
	if _, err := s.AgreementEvents(o.ID, 7); err != nil {
		t.Errorf("the untouched chain after the undone changes: %v", err)
	}
}

func TestAnOrganisationsSealingKeyIsMadeOnceAndKeptSealed(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	key, cert := must2(s.sealKey(o))
	again, same := must2(s.sealKey(o))
	if !key.Equal(again) || !cert.Equal(same) {
		t.Error("a second use made another key")
	}
	if cert.Subject.CommonName != "Alpha (Prospector)" || len(cert.UnknownExtKeyUsage) != 1 || !cert.UnknownExtKeyUsage[0].Equal(documentSigning) {
		t.Errorf("the certificate: %s, %v", cert.Subject, cert.UnknownExtKeyUsage)
	}
	var stored SealKey
	if err := s.DB.First(&stored, "org_id = ?", o.ID).Error; err != nil || !sealed(stored.Key) {
		t.Errorf("the private key is stored as %.12q (%v)", stored.Key, err)
	}
	other := mustOrg(t, s, "beta")
	if _, theirs := must2(s.sealKey(other)); theirs.Equal(cert) {
		t.Error("two organisations share a key")
	}

	// Moved onto a new server key, it still opens, and is the same key.
	s.Keyring = mustKeys(t, "2:"+newKey+",1:"+testKey)
	if moved, err := s.Reseal(); err != nil || moved < 1 {
		t.Fatalf("resealing: %d moved (%v)", moved, err)
	}
	s.Keyring = mustKeys(t, "2:"+newKey)
	if after, _ := must2(s.sealKey(o)); !after.Equal(key) {
		t.Error("the key changed when the server's key did")
	}

	s.Keyring = nil
	if _, _, err := s.sealKey(mustOrg(t, s, "gamma")); !errors.Is(err, errNoKeys) {
		t.Errorf("making a key without a server secret to keep it under: %v", err)
	}
}

// A PDF that bends its version's rules is taken at upload, so it has to seal too.
func TestAPDFTakenAtUploadCanBeSealed(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	hannah := must(s.QuickAddPerson(o, QuickAdd{Name: "Hannah Pryce", Company: "Larkmoor Yachts", Email: "hannah@larkmoor.example"}, "Ada Lovelace"))
	a := must(s.AddAgreement(o.ID, *hannah.CompanyID, "Form", "Ada Lovelace"))
	must(s.PutAgreementFile(o.ID, a.ID, "form.pdf", buildPDF("1.3", 2, "", "/Annots [<< /Type /Annot /Subtype /Caret /Rect [10 10 20 20] >>] "), "Ada Lovelace"))
	r := must(s.AddRecipient(o, a.ID, RecipientSigner, &hannah.ID, nil, "Ada Lovelace"))
	must(s.PutAgreementFields(o.ID, a.ID, []AgreementField{{RecipientID: r.ID, Page: 2, Kind: "signature", X: 0.1, Y: 0.7, W: 0.3, H: 0.06}}))
	if err := s.TrySeal(o, a.ID); err != nil {
		t.Errorf("sealing a PDF the upload took: %v", err)
	}
}

// sealedDraft is a draft with two signers and their fields on a three-page document,
// one of the pages turned.
func sealedDraft(t *testing.T) (*Store, *Org, *Agreement) {
	t.Helper()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	hannah := must(s.QuickAddPerson(o, QuickAdd{Name: "Hannah Pryce", Company: "Larkmoor Yachts", Email: "hannah@larkmoor.example"}, "Ada Lovelace"))
	zoe := must(s.QuickAddPerson(o, QuickAdd{Name: "Zoë Łukasiewicz-Øster", Company: "Larkmoor Yachts", Email: "zoe@larkmoor.example"}, "Ada Lovelace"))
	a := must(s.AddAgreement(o.ID, *hannah.CompanyID, "Service agreement", "Ada Lovelace"))
	must(s.PutAgreementFile(o.ID, a.ID, "service.pdf", buildPDF("1.7", 3, "", "/Rotate 90 "), "Ada Lovelace"))
	h := must(s.AddRecipient(o, a.ID, RecipientSigner, &hannah.ID, nil, "Ada Lovelace"))
	z := must(s.AddRecipient(o, a.ID, RecipientSigner, &zoe.ID, nil, "Ada Lovelace"))
	must(s.PutAgreementFields(o.ID, a.ID, []AgreementField{
		{RecipientID: h.ID, Page: 3, Kind: "signature", X: 0.1, Y: 0.7, W: 0.3, H: 0.06},
		{RecipientID: h.ID, Page: 3, Kind: "date", X: 0.1, Y: 0.78, W: 0.2, H: 0.03},
		{RecipientID: z.ID, Page: 3, Kind: "signature", X: 0.55, Y: 0.7, W: 0.35, H: 0.06},
		{RecipientID: z.ID, Page: 1, Kind: "initials", X: 0.85, Y: 0.92, W: 0.08, H: 0.04},
		{RecipientID: z.ID, Page: 2, Kind: "tick", X: 0.1, Y: 0.1, W: 0.03, H: 0.022},
	}))
	return s, o, a
}

func TestADraftCanBeFilledInAndSealedBeforeItIsSent(t *testing.T) {
	t.Parallel()
	s, o, a := sealedDraft(t)
	if err := s.TrySeal(o, a.ID); err != nil {
		t.Fatalf("a dry run of sealing: %v", err)
	}
	var made []AgreementEvent
	if err := s.DB.Find(&made).Error; err != nil || len(made) != 0 {
		t.Errorf("a dry run left %d events (%v)", len(made), err)
	}

	// A name the fonts can't draw is found before anything goes out.
	han := must(s.QuickAddPerson(o, QuickAdd{Name: "王小明", Company: "Larkmoor Yachts", Email: "wang@larkmoor.example"}, "Ada Lovelace"))
	must(s.AddRecipient(o, a.ID, RecipientCopy, &han.ID, nil, "Ada Lovelace"))
	if err := s.TrySeal(o, a.ID); err == nil || !strings.Contains(err.Error(), "can't show") {
		t.Errorf("a dry run with a name the fonts can't draw: %v", err)
	}
}

func TestASealedAgreementIsFilledInCertifiedAndShowsAnyChange(t *testing.T) {
	t.Parallel()
	s, o, a := sealedDraft(t)
	_, doc := must2(s.AgreementDocument(o.ID, a.ID))
	when := time.Date(2026, 11, 27, 9, 31, 52, 0, time.UTC)
	c := Certificate{Org: o.Name, Agreement: a.Name, Ref: "AGR-0001", Pages: 3, SentBy: "Ada Lovelace", SentAt: when.Add(-24 * time.Hour), CompletedAt: when,
		Rounds:  []CertificateRound{{N: 1, Hash: a.FileHash, Outcome: "signed by everyone"}},
		Signers: []CertificateSigner{{Name: "Hannah Pryce", Email: "hannah@larkmoor.example", Via: "typed name · email code", Typed: "Hannah Pryce", IP: "203.0.113.24", Browser: "Firefox on macOS", SignedAt: when}},
		Events:  []CertificateEvent{{At: when, Text: "Signed by Hannah Pryce as \"Hannah Pryce\""}}, Consent: signingConsent}
	stamps := []Stamp{{Page: 3, X: 0.1, Y: 0.7, W: 0.3, H: 0.06, Kind: "signature", Value: "Hannah Pryce"}, {Page: 2, X: 0.1, Y: 0.1, W: 0.03, H: 0.022, Kind: "tick", Value: "✓"}}
	out := must(s.sealDocument(o, doc, stamps, c))

	pages, err := api.PageCount(context.Background(), bytes.NewReader(out.PDF), model.NewDefaultConfiguration())
	if err != nil || pages != 4 {
		t.Errorf("the sealed document has %d pages (%v), want the 3 sent and the certificate", pages, err)
	}
	_, cert := must2(s.sealKey(o))
	if err := checkSeal(out.PDF, cert); err != nil {
		t.Fatalf("the seal: %v", err)
	}
	// One byte early in the document, inside what the seal covers, changed.
	tampered := bytes.Clone(out.PDF)
	tampered[200] ^= 0x01
	if err := checkSeal(tampered, cert); err == nil {
		t.Error("a changed byte left the seal holding")
	}
	if err := checkSeal(append(bytes.Clone(out.PDF), []byte("\n1 0 obj (added afterwards) endobj\n%%EOF\n")...), cert); err == nil {
		t.Error("bytes added after the seal left it holding")
	}
	if !bytes.Contains(out.PDF, []byte("/P 1")) {
		t.Error("the seal allows changes after it")
	}

	// Text the fonts can't draw stops the seal rather than sealing as a blank.
	for what, change := range map[string]func(*Certificate, []Stamp){
		"a typed signature": func(_ *Certificate, st []Stamp) { st[0].Value = "王小明" },
		"the agreement":     func(c *Certificate, _ []Stamp) { c.Agreement = "合同" },
		"a signer's name":   func(c *Certificate, _ []Stamp) { c.Signers[0].Name = "سارا" },
	} {
		c2, st2 := c, slices.Clone(stamps)
		c2.Signers = slices.Clone(c.Signers)
		change(&c2, st2)
		if _, err := s.sealDocument(o, doc, st2, c2); err == nil || !strings.Contains(err.Error(), "can't show") {
			t.Errorf("%s the fonts can't draw: %v", what, err)
		}
	}
	_, theirs := must2(s.sealKey(mustOrg(t, s, "beta")))
	if err := checkSeal(out.PDF, theirs); err == nil {
		t.Error("the seal holds for another organisation's key")
	}
	if !strings.Contains(string(out.PDF), "/ETSI.CAdES.detached") || !strings.Contains(string(out.PDF), "/DocMDP") {
		t.Error("the seal isn't a certifying PAdES signature")
	}
}

// Where a field lands, measured by drawing the sealed pages with poppler where it is
// installed: on a turned page, a cropped one and one whose box starts off the origin, it
// lands where it was placed on the page as shown.
func TestAStampLandsWhereItsFieldWasPlaced(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("pdftoppm"); err != nil {
		t.Skip("pdftoppm isn't installed")
	}
	pages := []string{"", "/Rotate 90 ", "/Rotate 270 ", "/CropBox [50 100 545 742] ", "/CropBox [50 100 545 742] /Rotate 180 "}
	var stamps []Stamp
	for i := range pages {
		stamps = append(stamps, Stamp{Page: i + 1, X: 0.1, Y: 0.2, W: 0.3, H: 0.05, Kind: "tick", Value: "✓"})
	}
	doc := pagesPDF(pages)
	out, err := stampPDF(doc, stamps)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "stamped.pdf")
	if err := os.WriteFile(file, out, 0o600); err != nil {
		t.Fatal(err)
	}
	if b, err := exec.Command("pdftoppm", "-r", "72", "-gray", "-cropbox", file, filepath.Join(dir, "p")).CombinedOutput(); err != nil {
		t.Fatalf("pdftoppm: %v %s", err, b)
	}
	for i := range pages {
		w, h, x0, y0, x1, y1 := inked(t, filepath.Join(dir, fmt.Sprintf("p-%d.pgm", i+1)))
		// The tick is drawn inside its box, never outside it.
		if float64(x0) < 0.1*float64(w)-2 || float64(x1) > 0.4*float64(w)+2 || float64(y0) < 0.2*float64(h)-2 || float64(y1) > 0.25*float64(h)+2 || x1 <= x0 {
			t.Errorf("page %d (%s): ink at x %d–%d, y %d–%d of %dx%d, want within x %.0f–%.0f, y %.0f–%.0f",
				i+1, pages[i], x0, x1, y0, y1, w, h, 0.1*float64(w), 0.4*float64(w), 0.2*float64(h), 0.25*float64(h))
		}
	}
}

// pagesPDF is a PDF of A4 pages, each with its own extra entries.
func pagesPDF(extras []string) []byte {
	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n")
	var offsets []int
	obj := func(body string) {
		offsets = append(offsets, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", len(offsets), body)
	}
	obj("<< /Type /Catalog /Pages 2 0 R >>")
	kids := ""
	for i := range extras {
		kids += fmt.Sprintf("%d 0 R ", 3+i)
	}
	obj(fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", kids, len(extras)))
	for _, e := range extras {
		obj("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] " + e + ">>")
	}
	x := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(offsets)+1)
	for _, o := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets)+1, x)
	return b.Bytes()
}

// inked is a binary PGM's size and the box around its dark pixels.
func inked(t *testing.T, file string) (w, h, x0, y0, x1, y1 int) {
	t.Helper()
	f, err := os.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	r := bufio.NewReader(f)
	var maxval int
	if _, err := fmt.Fscanf(r, "P5\n%d %d\n%d\n", &w, &h, &maxval); err != nil {
		t.Fatalf("%s: %v", file, err)
	}
	x0, y0 = w, h
	for y := range h {
		for x := range w {
			v, err := r.ReadByte()
			if err != nil {
				t.Fatal(err)
			}
			if int(v) < maxval/2 {
				x0, y0, x1, y1 = min(x0, x), min(y0, y), max(x1, x), max(y1, y)
			}
		}
	}
	return w, h, x0, y0, x1, y1
}
