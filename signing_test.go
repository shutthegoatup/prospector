package main

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"
)

// signingSite is a server whose organisation can send an agreement for signature: its
// own mail server and an https address, an agreement with a document, Hannah at the
// company and Ada (a member) to sign it in that order, and Tom to get a copy.
type signingSite struct {
	t       *testing.T
	app     *App
	s       *Store
	o       *Org
	box     *postbox
	ada     *User
	a       *Agreement
	hannah  *Contact
	tom     *Contact
	hRecip  *AgreementRecipient
	aRecip  *AgreementRecipient
	textFld int64
}

func newSigningSite(t *testing.T) *signingSite {
	t.Helper()
	box := &postbox{}
	var app *App
	s, _, _, _ := mailSite(t, func(a *App) { a.send, app = box.send, a })
	o := must(s.OrgBySlug("alpha"))
	if err := s.SaveOrgMail(&OrgMail{OrgID: o.ID, Host: "smtp.alpha.example", Port: 587, Username: "deals@alpha.example", Sender: "Alpha <deals@alpha.example>"}); err != nil {
		t.Fatal(err)
	}
	ada := must(s.UserByEmail("ada@example.com"))
	hannah := person(t, s, o, "Hannah Pryce", "Larkmoor Yachts", "hannah@larkmoor.example", false)
	tom := person(t, s, o, "Tom Ellery", "Larkmoor Yachts", "tom@larkmoor.example", false)
	a := must(s.AddAgreement(o.ID, *hannah.CompanyID, "Service agreement", "Ada Lovelace"))
	must(s.PutAgreementFile(o.ID, a.ID, "service.pdf", testPDF(2, ""), "Ada Lovelace"))
	h := must(s.AddRecipient(o, a.ID, RecipientSigner, &hannah.ID, nil, "Ada Lovelace"))
	m := must(s.AddRecipient(o, a.ID, RecipientSigner, nil, &ada.ID, "Ada Lovelace"))
	must(s.AddRecipient(o, a.ID, RecipientCopy, &tom.ID, nil, "Ada Lovelace"))
	fields := must(s.PutAgreementFields(o.ID, a.ID, []AgreementField{
		{RecipientID: h.ID, Page: 2, Kind: "signature", X: 0.1, Y: 0.7, W: 0.3, H: 0.06},
		{RecipientID: h.ID, Page: 2, Kind: "date", X: 0.1, Y: 0.78, W: 0.2, H: 0.03},
		{RecipientID: h.ID, Page: 1, Kind: "text", X: 0.1, Y: 0.2, W: 0.3, H: 0.03, Label: "Job title"},
		{RecipientID: m.ID, Page: 2, Kind: "signature", X: 0.55, Y: 0.7, W: 0.3, H: 0.06},
	}))
	return &signingSite{t: t, app: app, s: s, o: o, box: box, ada: ada, a: a, hannah: hannah, tom: tom, hRecip: h, aRecip: m, textFld: fields[2].ID}
}

// mailTo is the latest mail to an address, and the link in it.
func (x *signingSite) mailTo(address string) (Message, string) {
	x.t.Helper()
	x.box.mu.Lock()
	defer x.box.mu.Unlock()
	for i := len(x.box.sent) - 1; i >= 0; i-- {
		if m := x.box.sent[i]; m.To == address {
			return m, regexp.MustCompile(`https://prospector\.example/\S+`).FindString(m.Body)
		}
	}
	x.t.Fatalf("no mail to %s", address)
	return Message{}, ""
}

func (x *signingSite) round() { x.app.signingRound(context.Background(), time.Now()) }

// proveAs opens a link in a new browser and gives back the code mailed for it.
func (x *signingSite) proveAs(link, address string) (string, *Signing) {
	x.t.Helper()
	token := strings.TrimPrefix(link, testSite+"/s/")
	session, g, err := x.s.OpenSigning(token, "")
	if err != nil {
		x.t.Fatalf("opening %s: %v", link, err)
	}
	code, err := x.s.SigningCode(g, session, "203.0.113.24", "Firefox on macOS", nil)
	if err != nil {
		x.t.Fatal(err)
	}
	g = must(x.s.SigningBySession(session))
	if err := x.s.ProveSigning(g, session, code, "203.0.113.24", "Firefox on macOS"); err != nil {
		x.t.Fatal(err)
	}
	return session, must(x.s.SigningBySession(session))
}

func TestAnAgreementIsSentSignedInOrderAndSealed(t *testing.T) {
	x := newSigningSite(t)
	who := Who{IP: "203.0.113.24", Browser: "Firefox on macOS"}
	sent := must(x.s.SendAgreement(x.o, x.a.ID, SendIn{Message: "Hi Hannah, as discussed."}, x.ada, "Ada Lovelace"))
	if sent.Status != AgreementSent || sent.ExpiresAt == nil || sent.SentOrg != "Alpha" {
		t.Fatalf("sent: %+v", sent)
	}
	// Ada is second in order: nothing goes to her yet, and she can't sign first.
	if _, err := x.s.MemberSign(x.o, x.a.ID, x.ada, SignIn{Typed: "Ada Lovelace", Consent: true}, who); !errors.Is(err, errNotYourTurn) {
		t.Errorf("Ada signing before Hannah: %v", err)
	}
	x.round()
	mail, link := x.mailTo("hannah@larkmoor.example")
	if mail.Subject != "Please sign: Service agreement" || !strings.Contains(mail.Body, "> Hi Hannah, as discussed.") || !strings.HasPrefix(link, testSite+"/s/") || mail.ReplyTo != "ada@example.com" {
		t.Fatalf("Hannah's mail: %+v", mail)
	}
	if got := x.box.to(); len(got) != 1 {
		t.Errorf("mail at send went to %v, want Hannah alone", got)
	}

	// Opening the link does nothing until the code comes back.
	token := strings.TrimPrefix(link, testSite+"/s/")
	session, g := must2(x.s.OpenSigning(token, ""))
	if _, err := x.s.SignerSigns(g, SignIn{Typed: "Hannah Pryce", Consent: true}, who); err == nil {
		t.Error("signed without the emailed code")
	}
	code := must(x.s.SigningCode(g, session, who.IP, who.Browser, nil))
	if _, err := x.s.SigningCode(must(x.s.SigningBySession(session)), session, who.IP, who.Browser, nil); err == nil {
		t.Error("a second code went straight after the first")
	}
	g = must(x.s.SigningBySession(session))
	if err := x.s.ProveSigning(g, session, "000000", who.IP, who.Browser); err == nil && code != "000000" {
		t.Error("a wrong code was taken")
	}
	g = must(x.s.SigningBySession(session))
	if err := x.s.ProveSigning(g, session, code, who.IP, who.Browser); err != nil {
		t.Fatal(err)
	}
	g = must(x.s.SigningBySession(session))
	if _, doc := must2(x.s.SignerDocument(g, who)); len(doc) == 0 {
		t.Error("the document, after the code")
	}
	if _, err := x.s.SignerSigns(g, SignIn{Typed: "Hannah Pryce", Consent: true}, who); err == nil || !strings.Contains(err.Error(), "Job title") {
		t.Errorf("signing with the text field empty: %v", err)
	}
	if _, err := x.s.SignerSigns(g, SignIn{Typed: "Hannah Pryce"}, who); err == nil {
		t.Error("signed without agreeing to sign electronically")
	}
	if _, err := x.s.SignerSigns(g, SignIn{Typed: "Hannah Pryce", Consent: true, Values: map[int64]string{x.textFld: "Operations director"}}, who); err != nil {
		t.Fatalf("Hannah signing: %v", err)
	}
	if _, err := x.s.SignerSigns(g, SignIn{Typed: "Hannah Pryce", Consent: true, Values: map[int64]string{x.textFld: "Operations director"}}, who); !errors.Is(err, errAlreadyDone) {
		t.Errorf("Hannah signing twice: %v", err)
	}

	// Now it's Ada's turn: a member signs signed in, and nobody else can for her.
	x.round()
	if mail, link := x.mailTo("ada@example.com"); !strings.Contains(link, "/o/alpha/agreements/") || !strings.Contains(mail.Body, "Service agreement") {
		t.Errorf("Ada's mail: %s", mail.Body)
	}
	grace := must(x.s.CreateUser("grace@example.com", "Grace Hopper", "correct horse battery"))
	if err := x.s.AddMember(x.o.ID, grace.ID, "owner", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	if _, err := x.s.MemberSign(x.o, x.a.ID, grace, SignIn{Typed: "Ada Lovelace", Consent: true}, who); err == nil {
		t.Error("an owner signed in Ada's place")
	}
	if a := must(x.s.MemberSign(x.o, x.a.ID, x.ada, SignIn{Typed: "Ada Lovelace", Consent: true}, who)); a == nil {
		t.Fatal("Ada signing")
	}
	if a := must(x.s.Agreement(x.o.ID, x.a.ID)); a.Status != AgreementSealing {
		t.Fatalf("with every signature in: %s", a.Status)
	}
	if _, err := x.s.VoidAgreement(x.o.ID, x.a.ID, "", "Ada Lovelace"); err == nil {
		t.Error("withdrawn while being sealed")
	}

	// The loop seals it and sends everyone the sealed copy.
	x.round()
	a := must(x.s.Agreement(x.o.ID, x.a.ID))
	if a.Status != AgreementCompleted || a.SealedHash == "" {
		t.Fatalf("after sealing: %s %q", a.Status, a.SealedHash)
	}
	_, sealedPDF := must2(x.s.SealedDocument(x.o.ID, x.a.ID))
	_, cert := must2(x.s.sealKey(x.o))
	if err := checkSeal(sealedPDF, cert); err != nil {
		t.Errorf("the sealed copy: %v", err)
	}
	x.round()
	for _, to := range []string{"hannah@larkmoor.example", "ada@example.com", "tom@larkmoor.example"} {
		if mail, _ := x.mailTo(to); mail.Subject != "Signed: Service agreement" {
			t.Errorf("%s's last mail: %q", to, mail.Subject)
		}
	}
	events := must(x.s.AgreementEvents(x.o.ID, x.a.ID))
	var kinds []string
	for _, e := range events {
		kinds = append(kinds, e.Kind)
	}
	want := []string{EventSent, EventLinkSent, EventOpened, EventCodeSent, EventCodeEntered, EventDownloaded, EventConsented, EventSigned, EventLinkSent, EventConsented, EventSigned, EventSealed, EventCopySent, EventCopySent, EventCopySent}
	if strings.Join(kinds, " ") != strings.Join(want, " ") {
		t.Errorf("what happened:\n%v\nwant\n%v", kinds, want)
	}
	c := must(x.s.certificateFor(x.o, *a))
	if len(c.Signers) != 2 || c.Signers[0].Typed != "Hannah Pryce" || c.Signers[0].IP != "203.0.113.24" || c.Signers[1].Via != "typed name · Prospector sign-in" {
		t.Errorf("the certificate's signers: %+v", c.Signers)
	}
}

func TestASentAgreementCanBeWithdrawnDeclinedOrRunOut(t *testing.T) {
	who := Who{IP: "203.0.113.24", Browser: "Firefox on macOS"}

	t.Run("withdrawn", func(t *testing.T) {
		x := newSigningSite(t)
		must(x.s.SendAgreement(x.o, x.a.ID, SendIn{}, x.ada, "Ada Lovelace"))
		x.round()
		_, link := x.mailTo("hannah@larkmoor.example")
		_, g := x.proveAs(link, "hannah@larkmoor.example")
		must(x.s.VoidAgreement(x.o.ID, x.a.ID, "Wrong site", "Ada Lovelace"))
		if _, err := x.s.SignerSigns(g, SignIn{Typed: "Hannah Pryce", Consent: true, Values: map[int64]string{x.textFld: "Director"}}, who); !errors.Is(err, errNotSent) {
			t.Errorf("signing a withdrawn agreement: %v", err)
		}
		x.round()
		if mail, _ := x.mailTo("hannah@larkmoor.example"); mail.Subject != "Withdrawn: Service agreement" {
			t.Errorf("Hannah's last mail: %q", mail.Subject)
		}
	})

	t.Run("declined", func(t *testing.T) {
		x := newSigningSite(t)
		must(x.s.SendAgreement(x.o, x.a.ID, SendIn{}, x.ada, "Ada Lovelace"))
		x.round()
		_, link := x.mailTo("hannah@larkmoor.example")
		_, g := x.proveAs(link, "hannah@larkmoor.example")
		if a := must(x.s.SignerDeclines(g, "Wrong site address", who)); a.Status != AgreementSent {
			t.Logf("the agreement as locked: %s", a.Status)
		}
		if a := must(x.s.Agreement(x.o.ID, x.a.ID)); a.Status != AgreementDeclined {
			t.Errorf("after a decline: %s", a.Status)
		}
		x.round()
		if mail, _ := x.mailTo("ada@example.com"); mail.Subject != "Declined: Service agreement" || !strings.Contains(mail.Body, "> Wrong site address") {
			t.Errorf("the sender's mail: %+v", mail)
		}
	})

	t.Run("run out", func(t *testing.T) {
		x := newSigningSite(t)
		must(x.s.SendAgreement(x.o, x.a.ID, SendIn{Days: 1}, x.ada, "Ada Lovelace"))
		x.round()
		_, link := x.mailTo("hannah@larkmoor.example")
		_, g := x.proveAs(link, "hannah@larkmoor.example")
		x.app.signingRound(context.Background(), time.Now().Add(48*time.Hour))
		if a := must(x.s.Agreement(x.o.ID, x.a.ID)); a.Status != AgreementExpired {
			t.Errorf("past its end: %s", a.Status)
		}
		if _, err := x.s.SignerSigns(g, SignIn{Typed: "Hannah Pryce", Consent: true, Values: map[int64]string{x.textFld: "Director"}}, who); err == nil {
			t.Error("signed after it ran out")
		}
	})

	t.Run("erased", func(t *testing.T) {
		x := newSigningSite(t)
		must(x.s.SendAgreement(x.o, x.a.ID, SendIn{}, x.ada, "Ada Lovelace"))
		x.round()
		_, link := x.mailTo("hannah@larkmoor.example")
		// She signs, then asks to be erased before Ada has.
		_, g := x.proveAs(link, "hannah@larkmoor.example")
		must(x.s.SignerSigns(g, SignIn{Typed: "Hannah Pryce", Consent: true, Values: map[int64]string{x.textFld: "Operations director"}}, who))
		if err := x.s.ErasePerson(x.o.ID, x.hannah.ID, "Ada Lovelace"); err != nil {
			t.Fatal(err)
		}
		if a := must(x.s.Agreement(x.o.ID, x.a.ID)); a.Status != AgreementVoided {
			t.Errorf("after erasing a signer: %s", a.Status)
		}
		if _, _, err := x.s.OpenSigning(strings.TrimPrefix(link, testSite+"/s/"), ""); err == nil {
			t.Error("an erased signer's link still opens")
		}
		export := must(x.s.ExportOrg(x.o.ID, "Ada Lovelace"))
		for _, r := range export.Recipients {
			if strings.Contains(r.SentEmail+r.SentName+r.TypedName, "annah") {
				t.Errorf("the export names an erased person: %+v", r)
			}
		}
		for _, f := range export.Fields {
			if strings.Contains(f.Value, "annah") || f.Value == "Operations director" {
				t.Errorf("the export keeps what an erased person filled in: %+v", f)
			}
		}
		for _, e := range export.Events {
			if strings.Contains(e.Actor+e.Detail, "annah") || e.IP == "203.0.113.24" {
				t.Errorf("the export's events name an erased person: %+v", e)
			}
		}
		if must(x.s.AgreementEvents(x.o.ID, x.a.ID)) == nil {
			t.Error("the record of the agreement went with her")
		}
	})
}

// A code can be tried five times; a link takes thirty wrong codes in all.
func TestASignersCodeCantBeGuessed(t *testing.T) {
	x := newSigningSite(t)
	must(x.s.SendAgreement(x.o, x.a.ID, SendIn{}, x.ada, "Ada Lovelace"))
	x.round()
	_, link := x.mailTo("hannah@larkmoor.example")
	session, g := must2(x.s.OpenSigning(strings.TrimPrefix(link, testSite+"/s/"), ""))
	code := must(x.s.SigningCode(g, session, "", "", nil))
	wrong := "000000"
	if code == wrong {
		wrong = "111111"
	}
	for range codeTries {
		_ = x.s.ProveSigning(must(x.s.SigningBySession(session)), session, wrong, "", "")
	}
	if err := x.s.ProveSigning(must(x.s.SigningBySession(session)), session, code, "", ""); err == nil {
		t.Error("the right code was taken after five wrong ones")
	}
	if err := x.s.db.Model(&SignerLink{}).Where("recipient_id = ?", x.hRecip.ID).Update("failures", linkFailures).Error; err != nil {
		t.Fatal(err)
	}
	if _, _, err := x.s.OpenSigning(strings.TrimPrefix(link, testSite+"/s/"), ""); err == nil {
		t.Error("a link with thirty wrong codes still opens")
	}
}

// Sending refuses what couldn't be completed.
func TestAnAgreementIsSentOnlyWhenItCanBeCompleted(t *testing.T) {
	x := newSigningSite(t)
	if err := x.s.db.Model(&AgreementField{}).Where("id = ?", x.textFld).Update("label", "").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := x.s.SendAgreement(x.o, x.a.ID, SendIn{}, x.ada, "Ada Lovelace"); err == nil || !strings.Contains(err.Error(), "text field") {
		t.Errorf("sending with an unlabelled text field: %v", err)
	}
	if err := x.s.db.Model(&AgreementField{}).Where("id = ?", x.textFld).Update("label", "Job title").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := x.s.SendAgreement(x.o, x.a.ID, SendIn{Days: 90}, x.ada, "Ada Lovelace"); err == nil {
		t.Error("sent to be open for 90 days")
	}
	must(x.s.SendAgreement(x.o, x.a.ID, SendIn{}, x.ada, "Ada Lovelace"))
	if _, err := x.s.SendAgreement(x.o, x.a.ID, SendIn{}, x.ada, "Ada Lovelace"); !errors.Is(err, errNotDraft) {
		t.Errorf("sending twice: %v", err)
	}
	if _, err := x.s.PutAgreementFields(x.o.ID, x.a.ID, nil); !errors.Is(err, errNotDraft) {
		t.Errorf("changing fields once sent: %v", err)
	}
}
