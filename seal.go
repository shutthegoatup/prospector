package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"embed"
	"encoding/asn1"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/digitorus/pdfsign"
	"github.com/go-pdf/fpdf"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"golang.org/x/image/font/sfnt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Sealing an agreement once everyone has signed: what each signer filled in is drawn onto
// the pages they saw, a certificate of what happened is added as the last page, and the
// whole is signed with the organisation's own key, so any later change to it shows.

//go:embed sealfonts/*.ttf
var sealFonts embed.FS

// certificateLayout is the version of the certificate page's layout, kept with each seal:
// the same events drawn by the same layout give the same page.
const certificateLayout = 1

// documentSigning is the extended key usage for signing documents (RFC 9336).
var documentSigning = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 3, 36}

// SealKey is the key an organisation's agreements are sealed with: made the first time
// one is needed, its private half sealed under the server's secret key, its certificate
// in every PDF it seals.
type SealKey struct {
	OrgID     int64     `gorm:"primaryKey;autoIncrement:false"`
	Cert      []byte    `gorm:"not null"`
	Key       string    `gorm:"not null;default:''"`
	CreatedAt time.Time `gorm:"not null"`
}

// sealKey is the organisation's sealing key and certificate, made on first use.
func (s *Store) sealKey(o *Org) (*ecdsa.PrivateKey, *x509.Certificate, error) {
	var k SealKey
	err := s.db.Where("org_id = ?", o.ID).First(&k).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if k, err = s.makeSealKey(o); err != nil {
			return nil, nil, err
		}
	} else if err != nil {
		return nil, nil, err
	}
	plain, err := s.keys.open(k.Key)
	if err != nil {
		return nil, nil, fmt.Errorf("the organisation's sealing key: %w", err)
	}
	key, err := x509.ParseECPrivateKey([]byte(plain))
	if err != nil {
		return nil, nil, fmt.Errorf("the organisation's sealing key: %w", err)
	}
	cert, err := x509.ParseCertificate(k.Cert)
	if err != nil {
		return nil, nil, fmt.Errorf("the organisation's sealing certificate: %w", err)
	}
	return key, cert, nil
}

// makeSealKey makes an organisation's key and its self-signed certificate. Two made at
// once keep whichever was stored first.
func (s *Store) makeSealKey(o *Org) (SealKey, error) {
	if s.keys == nil {
		return SealKey{}, fmt.Errorf("sealing agreements needs a secret key to keep its own under: %w", errNoKeys)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return SealKey{}, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return SealKey{}, err
	}
	now := time.Now().UTC()
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: o.Name + " (Prospector)", Organization: []string{o.Name}},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.AddDate(20, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageContentCommitment,
		UnknownExtKeyUsage:    []asn1.ObjectIdentifier{documentSigning},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return SealKey{}, err
	}
	raw, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return SealKey{}, err
	}
	sealedKey, err := s.keys.seal(string(raw))
	if err != nil {
		return SealKey{}, err
	}
	k := SealKey{OrgID: o.ID, Cert: der, Key: sealedKey, CreatedAt: now}
	if err := s.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&k).Error; err != nil {
		return SealKey{}, err
	}
	return k, s.db.Where("org_id = ?", o.ID).First(&k).Error
}

// Stamp is what one field shows on the sealed document: where, as fractions of the page
// as shown from its top left, and what the signer gave.
type Stamp struct {
	Page       int
	X, Y, W, H float64
	Kind       string
	Value      string
}

type sealFont struct {
	name string
	data []byte
	sfnt *sfnt.Font
}

func loadSealFont(name string) sealFont {
	data, err := sealFonts.ReadFile("sealfonts/" + name + ".ttf")
	if err != nil {
		panic(err)
	}
	f, err := sfnt.Parse(data)
	if err != nil {
		panic(err)
	}
	return sealFont{name: name, data: data, sfnt: f}
}

var (
	textFont   = loadSealFont("NotoSans-Regular")
	boldFont   = loadSealFont("NotoSans-Bold")
	italicFont = loadSealFont("NotoSans-Italic")
	scriptFont = loadSealFont("GreatVibes-Regular")
)

// covers is whether a font has a glyph for every letter of s.
func (f sealFont) covers(s string) bool {
	var buf sfnt.Buffer
	for _, r := range s {
		if r == ' ' {
			continue
		}
		if i, err := f.sfnt.GlyphIndex(&buf, r); err != nil || i == 0 {
			return false
		}
	}
	return true
}

// drawable is whether text can be drawn on a sealed PDF at all: Noto Sans has every
// letter of it. A name it can't draw is refused before anything is sent.
func drawable(s string) error {
	if !textFont.covers(s) {
		return fmt.Errorf("%q has letters the sealed document can't show yet", s)
	}
	return nil
}

func newSealPDF() *fpdf.Fpdf {
	p := fpdf.NewCustom(&fpdf.InitType{UnitStr: "pt", Size: fpdf.SizeType{Wd: 595.28, Ht: 841.89}})
	p.SetCompression(true)
	p.SetAutoPageBreak(false, 0)
	for _, f := range []sealFont{textFont, boldFont, italicFont, scriptFont} {
		p.AddUTF8FontFromBytes(f.name, "", f.data)
	}
	p.SetCreator("Prospector", true)
	return p
}

// fit is the font size at which s fills a box h high without running past w.
func fit(p *fpdf.Fpdf, s string, w, h, share float64) float64 {
	size := h * share
	p.SetFontSize(size)
	if width := p.GetStringWidth(s); width > w*0.94 && width > 0 {
		size *= w * 0.94 / width
	}
	return size
}

// drawStamp draws one field's value into its box on an overlay page.
func drawStamp(p *fpdf.Fpdf, st Stamp, pw, ph float64) {
	x, y, w, h := st.X*pw, st.Y*ph, st.W*pw, st.H*ph
	p.SetTextColor(20, 24, 40)
	switch st.Kind {
	case "tick":
		if st.Value == "" {
			return
		}
		p.SetDrawColor(20, 24, 40)
		p.SetLineWidth(h * 0.12)
		p.SetLineCapStyle("round")
		// Two strokes, not a polygon, which would close itself into a triangle.
		p.MoveTo(x+w*0.18, y+h*0.55)
		p.LineTo(x+w*0.42, y+h*0.8)
		p.LineTo(x+w*0.85, y+h*0.2)
		p.DrawPath("D")
		return
	case "signature", "initials":
		font := scriptFont
		if !font.covers(st.Value) {
			font = italicFont
		}
		p.SetFont(font.name, "", 12)
		size := fit(p, st.Value, w, h, 0.8)
		p.SetFontSize(size)
		p.Text(x+w*0.03, y+h*0.72, st.Value)
	default:
		p.SetFont(textFont.name, "", 12)
		size := fit(p, st.Value, w, h, 0.7)
		p.SetFontSize(size)
		p.Text(x+w*0.03, y+h*0.75, st.Value)
	}
}

// readForSealing reads a document as uploads are read: not validated, since real PDFs
// often break a rule of their own version that doesn't stop them being filled in.
func readForSealing(doc []byte) (*model.Context, error) {
	rc, err := api.ReadContext(context.Background(), bytes.NewReader(doc), model.NewDefaultConfiguration())
	if err != nil {
		return nil, fmt.Errorf("reading the document to fill it in: %w", err)
	}
	if err := rc.EnsurePageCount(); err != nil {
		return nil, err
	}
	return rc, nil
}

// stampPDF draws each stamp onto its page, as fillIn does, and writes the document out.
func stampPDF(doc []byte, stamps []Stamp) ([]byte, error) {
	rc, err := readForSealing(doc)
	if err != nil {
		return nil, err
	}
	if err := fillIn(rc, stamps); err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := api.WriteContext(context.Background(), rc, &out); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// fillIn draws each stamp onto its page of a document read for sealing. The fields'
// places are fractions of the page as a reader shows it, its crop box turned by its
// rotation, which is how they were placed.
func fillIn(rc *model.Context, stamps []Stamp) error {
	if len(stamps) == 0 {
		return nil
	}
	ctx := context.Background()
	bounds, err := rc.PageBoundaries(ctx, nil)
	if err != nil {
		return err
	}
	byPage := map[int][]Stamp{}
	for _, st := range stamps {
		if st.Page < 1 || st.Page > len(bounds) {
			return fmt.Errorf("a field is on page %d of a document with %d", st.Page, len(bounds))
		}
		byPage[st.Page] = append(byPage[st.Page], st)
	}
	overlay := newSealPDF()
	order := map[int]int{}
	for page := 1; page <= len(bounds); page++ {
		if len(byPage[page]) == 0 {
			continue
		}
		box := bounds[page-1].CropBox()
		w, h := box.Width(), box.Height()
		if bounds[page-1].Rot%180 != 0 {
			w, h = h, w
		}
		overlay.AddPageFormat("P", fpdf.SizeType{Wd: w, Ht: h})
		order[page] = len(order) + 1
		for _, st := range byPage[page] {
			drawStamp(overlay, st, w, h)
		}
	}
	var drawn bytes.Buffer
	if err := overlay.Output(&drawn); err != nil {
		return err
	}
	marks := map[int]*model.Watermark{}
	for page, n := range order {
		wm, err := api.PDFWatermarkForReadSeeker(ctx, bytes.NewReader(drawn.Bytes()), n, "scale:1 abs, pos:c, rot:0", true, false, types.POINTS)
		if err != nil {
			return err
		}
		marks[page] = wm
	}
	if err := pdfcpu.AddWatermarksMap(ctx, rc, marks); err != nil {
		return fmt.Errorf("filling the document in: %w", err)
	}
	return nil
}

// Certificate is what the last page of a sealed agreement says: drawn only from the
// agreement, its rounds and its events, never typed in.
type Certificate struct {
	Org, Agreement, Ref string
	Pages               int
	SentBy              string
	SentAt, CompletedAt time.Time
	Rounds              []CertificateRound
	Signers             []CertificateSigner
	Events              []CertificateEvent
	Consent             string
	// LastEvent is the hash of the agreement's last event when it was sealed: the log can't
	// lose events off its end without this showing it.
	LastEvent string
}

type CertificateRound struct {
	N             int
	Hash, Outcome string
}

type CertificateSigner struct {
	Name, Email, Via, Typed, IP, Browser string
	SignedAt                             time.Time
}

type CertificateEvent struct {
	At   time.Time
	Text string
}

func stamp(t time.Time) string { return t.UTC().Format("2 Jan 2006 15:04:05 UTC") }

// texts is every piece of text a seal draws: the certificate's and the stamps'.
func (c Certificate) texts(stamps []Stamp) []string {
	out := []string{c.Org, c.Agreement, c.Ref, c.SentBy, c.Consent, c.LastEvent}
	for _, r := range c.Rounds {
		out = append(out, r.Hash, r.Outcome)
	}
	for _, s := range c.Signers {
		out = append(out, s.Name, s.Email, s.Via, s.Typed, s.IP, s.Browser)
	}
	for _, e := range c.Events {
		out = append(out, e.Text)
	}
	for _, st := range stamps {
		if st.Kind != "tick" {
			out = append(out, st.Value)
		}
	}
	return out
}

// drawCertificate draws the certificate as pages of its own, to go after the document's.
func drawCertificate(c Certificate) ([]byte, error) {
	p := newSealPDF()
	p.SetMargins(56, 56, 56)
	p.SetAutoPageBreak(true, 64)
	p.SetFooterFunc(func() {
		p.SetY(-48)
		p.SetFont(textFont.name, "", 7.5)
		p.SetTextColor(94, 86, 70)
		p.MultiCell(0, 10, "This PDF is sealed with a digital signature made with "+c.Org+"'s key in Prospector. Any change after sealing breaks the seal; "+
			"a PDF reader's signature panel shows whether it is intact. These are simple electronic signatures: an emailed code or a sign-in, and a typed name. "+
			fmt.Sprintf("Prospector didn't read the document's contents. Certificate layout %d.", certificateLayout), "", "L", false)
	})
	p.AddPage()
	label := func(s string) {
		p.Ln(8)
		p.SetFont(boldFont.name, "", 8)
		p.SetTextColor(94, 86, 70)
		p.CellFormat(0, 12, strings.ToUpper(s), "", 1, "L", false, 0, "")
		p.SetTextColor(28, 24, 16)
	}
	row := func(k, v string) {
		p.SetFont(boldFont.name, "", 9)
		p.CellFormat(130, 13, k, "", 0, "L", false, 0, "")
		p.SetFont(textFont.name, "", 9)
		p.MultiCell(0, 13, v, "", "L", false)
	}
	p.SetFont(boldFont.name, "", 20)
	p.SetTextColor(28, 24, 16)
	p.CellFormat(0, 26, "Certificate of signing", "", 1, "L", false, 0, "")
	p.SetFont(textFont.name, "", 9)
	p.SetTextColor(94, 86, 70)
	p.CellFormat(0, 12, c.Org+" · "+c.Ref, "", 1, "L", false, 0, "")
	p.SetTextColor(28, 24, 16)

	label("Document")
	row("Name", c.Agreement)
	row("Pages", fmt.Sprintf("%d, then this certificate", c.Pages))
	row("Sent", stamp(c.SentAt)+" by "+c.SentBy)
	row("Completed", stamp(c.CompletedAt))
	if len(c.Rounds) > 0 {
		label("Rounds")
		for _, r := range c.Rounds {
			row(fmt.Sprintf("Round %d", r.N), "SHA-256 "+r.Hash+" · "+r.Outcome)
		}
	}
	label("Signers")
	for i, s := range c.Signers {
		p.SetFont(boldFont.name, "", 10)
		p.CellFormat(0, 14, fmt.Sprintf("%d · %s", i+1, s.Name), "", 1, "L", false, 0, "")
		font := scriptFont
		if !font.covers(s.Typed) {
			font = italicFont
		}
		p.SetFont(font.name, "", 18)
		p.CellFormat(0, 22, s.Typed, "", 1, "L", false, 0, "")
		p.SetFont(textFont.name, "", 8.5)
		p.SetTextColor(94, 86, 70)
		p.MultiCell(0, 11, s.Email+" · "+s.Via+" · signed "+stamp(s.SignedAt)+"\nIP "+s.IP+" · "+s.Browser, "", "L", false)
		p.SetTextColor(28, 24, 16)
		p.Ln(4)
	}
	label("What happened, in order")
	for _, e := range c.Events {
		p.SetFont(textFont.name, "", 8.5)
		p.CellFormat(150, 12, stamp(e.At), "", 0, "L", false, 0, "")
		p.MultiCell(0, 12, e.Text, "", "L", false)
	}
	if c.LastEvent != "" {
		label("Record")
		row("Last event", "SHA-256 "+c.LastEvent)
	}
	if c.Consent != "" {
		label("Wording each signer agreed to")
		p.SetFont(textFont.name, "", 9)
		p.MultiCell(0, 13, "“"+c.Consent+"”", "", "L", false)
	}
	var out bytes.Buffer
	if err := p.Output(&out); err != nil {
		return nil, fmt.Errorf("drawing the certificate: %w", err)
	}
	return out.Bytes(), nil
}

// sealPDF signs a document with the organisation's key: a certifying PAdES signature
// that allows no changes after it, so filling in a form field left in the document breaks
// it as plainly as any other edit.
func sealPDF(doc []byte, key *ecdsa.PrivateKey, cert *x509.Certificate, org string) ([]byte, error) {
	d, err := pdfsign.Open(bytes.NewReader(doc), int64(len(doc)))
	if err != nil {
		return nil, fmt.Errorf("opening the document to seal it: %w", err)
	}
	d.Sign(key, cert).Type(pdfsign.CertificationSignature).Permission(pdfsign.NoChanges).Format(pdfsign.PAdES_B).
		SignerName(org).Reason("Sealed by Prospector when the last signer signed")
	var out bytes.Buffer
	if _, err := d.Write(&out); err != nil {
		return nil, fmt.Errorf("sealing the document: %w", err)
	}
	return out.Bytes(), nil
}

// checkSeal is whether a sealed document is intact and sealed with this certificate.
func checkSeal(doc []byte, cert *x509.Certificate) error {
	d, err := pdfsign.Open(bytes.NewReader(doc), int64(len(doc)))
	if err != nil {
		return err
	}
	roots := x509.NewCertPool()
	roots.AddCert(cert)
	r := d.Verify().TrustedRoots(roots).ExternalChecks(false).SkipRevocationCheck(true)
	if !r.Valid() || r.Count() != 1 {
		return fmt.Errorf("the seal doesn't hold: %v", r.Err())
	}
	if !r.Signatures()[0].Certificate.Equal(cert) {
		return errors.New("the document is sealed with another key")
	}
	// A valid signature still leaves room for an update written after it: what it covers
	// has to run to the document's last byte.
	if end, ok := byteRangeEnd(doc); !ok || end != len(doc) {
		return errors.New("the document was added to after it was sealed")
	}
	return nil
}

// byteRangeEnd is where the first signature's byte range ends: the second of its two
// ranges' offset plus length.
func byteRangeEnd(doc []byte) (int, bool) {
	at := bytes.Index(doc, []byte("/ByteRange"))
	if at < 0 {
		return 0, false
	}
	rest := doc[at+len("/ByteRange"):]
	open, shut := bytes.IndexByte(rest, '['), bytes.IndexByte(rest, ']')
	if open < 0 || shut < open {
		return 0, false
	}
	var r [4]int
	if n, err := fmt.Sscan(string(rest[open+1:shut]), &r[0], &r[1], &r[2], &r[3]); err != nil || n != 4 {
		return 0, false
	}
	return r[2] + r[3], true
}

// Sealed is a sealed agreement document and the hash it is kept with.
type Sealed struct {
	PDF  []byte
	Hash string
}

// seal fills an agreement's document in, adds its certificate and seals the whole with
// the organisation's key, then checks the seal holds before handing it back.
func (s *Store) seal(o *Org, doc []byte, stamps []Stamp, c Certificate) (*Sealed, error) {
	// Text the fonts can't draw would seal as empty space, so it stops the seal instead.
	for _, text := range c.texts(stamps) {
		if err := drawable(text); err != nil {
			return nil, err
		}
	}
	key, cert, err := s.sealKey(o)
	if err != nil {
		return nil, err
	}
	rc, err := readForSealing(doc)
	if err != nil {
		return nil, err
	}
	if err := fillIn(rc, stamps); err != nil {
		return nil, err
	}
	page, err := drawCertificate(c)
	if err != nil {
		return nil, err
	}
	certificate, err := api.ReadAndValidate(context.Background(), bytes.NewReader(page), model.NewDefaultConfiguration())
	if err != nil {
		return nil, fmt.Errorf("reading the certificate back: %w", err)
	}
	rc.EnsureVersionForWriting()
	if err := pdfcpu.MergeXRefTables(context.Background(), "certificate", certificate, rc, false, false); err != nil {
		return nil, fmt.Errorf("adding the certificate: %w", err)
	}
	var whole bytes.Buffer
	if err := api.WriteContext(context.Background(), rc, &whole); err != nil {
		return nil, err
	}
	out, err := sealPDF(whole.Bytes(), key, cert, o.Name)
	if err != nil {
		return nil, err
	}
	if err := checkSeal(out, cert); err != nil {
		return nil, err
	}
	sum := sha256.Sum256(out)
	return &Sealed{PDF: out, Hash: hex.EncodeToString(sum[:])}, nil
}

// TrySeal goes through sealing a draft as it is set out, with stand-in values in every
// field and a stand-in certificate, and keeps nothing. Sending runs it first, so a
// document that can't be filled in and sealed is found before anyone signs it.
func (s *Store) TrySeal(o *Org, id int64, sentBy string) error {
	a, doc, err := s.AgreementDocument(o.ID, id)
	if err != nil {
		return err
	}
	fields, err := s.AgreementFields(o.ID, id)
	if err != nil {
		return err
	}
	parties, err := s.AgreementRecipients(o.ID, id)
	if err != nil {
		return err
	}
	pages, err := s.agreementPages(o.ID, a)
	if err != nil {
		return err
	}
	names := map[int64]string{}
	c := Certificate{Org: o.Name, Agreement: a.Name, Ref: fmt.Sprintf("AGR-%04d", a.ID), Pages: pages, SentBy: sentBy, SentAt: time.Now(), CompletedAt: time.Now(),
		Rounds: []CertificateRound{{N: a.Round, Hash: a.FileHash, Outcome: "signed by everyone"}}, Consent: signingConsent}
	for _, r := range parties {
		if err := drawable(r.Name); err != nil {
			return err
		}
		names[r.ID] = r.Name
		if r.Role == RecipientSigner {
			c.Signers = append(c.Signers, CertificateSigner{Name: r.Name, Email: r.Email, Via: "typed name · email code", Typed: r.Name, IP: "203.0.113.1", Browser: "a browser", SignedAt: time.Now()})
		}
	}
	stamps := make([]Stamp, 0, len(fields))
	for _, f := range fields {
		stamps = append(stamps, Stamp{Page: f.Page, X: f.X, Y: f.Y, W: f.W, H: f.H, Kind: f.Kind, Value: standIn(f.Kind, names[f.RecipientID])})
	}
	_, err = s.seal(o, doc, stamps, c)
	return err
}

// standIn is what a dry run puts in a field: the signer's own name where they would type
// it, so a name the fonts can't draw is caught too.
func standIn(kind, name string) string {
	switch kind {
	case "date":
		return time.Now().UTC().Format("2 Jan 2006")
	case "tick":
		return "✓"
	case "initials":
		var out []rune
		for _, word := range strings.Fields(name) {
			out = append(out, []rune(word)[0])
		}
		return string(out)
	case "text":
		return "Some text"
	}
	return name
}

// signingConsent is what a signer agrees to by signing, shown on the page and kept on
// the certificate word for word. The certificate names its version.
const signingConsent = "I've read the document. I agree to sign it electronically, and that typing my name here is my signature."
