package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// Signers who may sign in any order can sign at once, while others ask for codes: each
// signature counts, the record stays one chain, and the last hands it to be sealed.
func TestSignersAtOnceKeepOneRecord(t *testing.T) {
	x := newSigningSite(t)
	if _, err := x.s.ChangeAgreement(x.o.ID, x.a.ID, nil, new(false), "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	grace := must(x.s.CreateUser("grace@example.com", "Grace Hopper", "correct horse battery"))
	if err := x.s.AddMember(x.o.ID, grace.ID, "member", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	gr := must(x.s.AddRecipient(x.o, x.a.ID, RecipientSigner, nil, &grace.ID, "Ada Lovelace"))
	fields := must(x.s.AgreementFields(x.o.ID, x.a.ID))
	fields = append(fields, AgreementField{RecipientID: gr.ID, Page: 1, Kind: "signature", X: 0.6, Y: 0.1, W: 0.3, H: 0.05})
	must(x.s.PutAgreementFields(x.o.ID, x.a.ID, fields))
	must(x.s.SendAgreement(x.o, x.a.ID, SendIn{}, x.ada, "Ada Lovelace"))
	x.round()
	_, link := x.mailTo("hannah@larkmoor.example")
	session, g := must2(x.s.OpenSigning(strings.TrimPrefix(link, testSite+"/s/"), ""))

	who := Who{IP: "203.0.113.24", Browser: "Firefox on macOS"}
	var wg sync.WaitGroup
	errs := make(chan error, 3)
	for _, u := range []*User{x.ada, grace} {
		wg.Go(func() {
			_, err := x.s.MemberSign(x.o, x.a.ID, u, SignIn{Typed: u.Name, Consent: true}, who)
			errs <- err
		})
	}
	wg.Go(func() {
		_, err := x.s.SigningCode(g, session, who.IP, who.Browser, nil)
		errs <- err
	})
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("at once: %v", err)
		}
	}
	if _, err := x.s.AgreementEvents(x.o.ID, x.a.ID); err != nil {
		t.Fatalf("the record after writes at once: %v", err)
	}
	for _, r := range must(x.s.AgreementRecipients(x.o.ID, x.a.ID)) {
		if r.UserID != nil && r.Status != SignerSigned {
			t.Errorf("%s: %s", r.Name, r.Status)
		}
	}
}

// Guesses made at once still count one each: no more than five land on a code.
func TestGuessesAtOnceStillCount(t *testing.T) {
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
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() { _ = x.s.ProveSigning(must(x.s.SigningBySession(session)), session, wrong, "", "") })
	}
	wg.Wait()
	var sess SignerSession
	if err := x.s.db.Where("token_hash = ?", hashToken(session)).First(&sess).Error; err != nil || sess.CodeTries > codeTries {
		t.Errorf("tries counted: %d (%v)", sess.CodeTries, err)
	}
	if err := x.s.ProveSigning(must(x.s.SigningBySession(session)), session, code, "", ""); err == nil {
		t.Error("the right code was taken after a burst of wrong ones")
	}
}

// What can only be sealed when it is drawn is refused when it is sent: a sender's name
// the fonts can't show would leave a signed agreement unsealed.
func TestASenderWhoseNameCantBeDrawnCantSend(t *testing.T) {
	x := newSigningSite(t)
	if _, err := x.s.SendAgreement(x.o, x.a.ID, SendIn{}, x.ada, "王小明"); err == nil || !strings.Contains(err.Error(), "can't show") {
		t.Errorf("sent by someone whose name can't be drawn: %v", err)
	}
}

// Once withdrawn, the link asks for no code and opens no document.
func TestAWithdrawnAgreementOpensNothingMore(t *testing.T) {
	x := newSigningSite(t)
	must(x.s.SendAgreement(x.o, x.a.ID, SendIn{}, x.ada, "Ada Lovelace"))
	x.round()
	_, link := x.mailTo("hannah@larkmoor.example")
	proven, _ := x.proveAs(link, "hannah@larkmoor.example")
	fresh, _ := must2(x.s.OpenSigning(strings.TrimPrefix(link, testSite+"/s/"), ""))
	must(x.s.VoidAgreement(x.o.ID, x.a.ID, "", "Ada Lovelace"))
	if _, err := x.s.SigningCode(must(x.s.SigningBySession(fresh)), fresh, "", "", nil); err == nil {
		t.Error("a code for a withdrawn agreement")
	}
	if _, _, err := x.s.SignerDocument(must(x.s.SigningBySession(proven)), Who{}); err == nil {
		t.Error("the document of a withdrawn agreement")
	}
}

// Opening the link again in a browser that gave its code keeps that session.
func TestOpeningTheLinkAgainKeepsTheSession(t *testing.T) {
	x := newSigningSite(t)
	must(x.s.SendAgreement(x.o, x.a.ID, SendIn{}, x.ada, "Ada Lovelace"))
	x.round()
	_, link := x.mailTo("hannah@larkmoor.example")
	session, _ := x.proveAs(link, "hannah@larkmoor.example")
	again, g := must2(x.s.OpenSigning(strings.TrimPrefix(link, testSite+"/s/"), session))
	if again != session || !g.Session.Proven {
		t.Error("opening the link again started a new session")
	}
}

// A member who leaves before they've signed takes the agreement with them: it is
// withdrawn, not sent on to them as an outsider.
func TestAMemberWhoLeavesBeforeSigningWithdrawsIt(t *testing.T) {
	x := newSigningSite(t)
	must(x.s.SendAgreement(x.o, x.a.ID, SendIn{}, x.ada, "Ada Lovelace"))
	grace := must(x.s.CreateUser("grace@example.com", "Grace Hopper", "correct horse battery"))
	if err := x.s.AddMember(x.o.ID, grace.ID, "owner", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	if _, err := x.s.RemoveMember(x.o.ID, x.ada.ID, "Grace Hopper"); err != nil {
		t.Fatal(err)
	}
	if a := must(x.s.Agreement(x.o.ID, x.a.ID)); a.Status != AgreementVoided {
		t.Errorf("after the member who was to sign left: %s", a.Status)
	}
}

// Someone put on the never-contact list after it was sent is mailed nothing more.
func TestNeverContactHoldsForEveryMailNotOnlyAtSend(t *testing.T) {
	x := newSigningSite(t)
	must(x.s.SendAgreement(x.o, x.a.ID, SendIn{}, x.ada, "Ada Lovelace"))
	x.o.Settings.NeverContact = "hannah@larkmoor.example"
	if err := x.s.SaveOrg(x.o); err != nil {
		t.Fatal(err)
	}
	x.round()
	if to := x.box.to(); len(to) != 0 {
		t.Errorf("mail went to %v", to)
	}
	if problems := must(x.s.MailProblems(x.o.ID, x.a.ID)); problems[x.hRecip.ID] == "" {
		t.Error("the mail that didn't go isn't shown as a problem")
	}
}

// An agreement a key sent has no member behind it: a decline tells nobody by mistake.
func TestAnAgreementAKeySentMailsNoMemberByMistake(t *testing.T) {
	x := newSigningSite(t)
	must(x.s.SendAgreement(x.o, x.a.ID, SendIn{}, nil, "agent:robot"))
	x.round()
	_, link := x.mailTo("hannah@larkmoor.example")
	_, g := x.proveAs(link, "hannah@larkmoor.example")
	must(x.s.SignerDeclines(g, "No", Who{}))
	x.round()
	for _, to := range x.box.to() {
		if to == "ada@example.com" {
			t.Error("the decline went to the first member, who didn't send it")
		}
	}
}

// A database from before sending has agreements, recipients and fields without its
// columns. They come through as drafts that can be sent and signed.
func TestAgreementsFromBeforeSendingCanBeSentAfterTheUpgrade(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "old.sqlite")
	if pg := os.Getenv("PROSPECTOR_TEST_PG"); pg != "" {
		dsn = pg
	}
	old := must(Open(dsn))
	if err := old.db.Migrator().DropTable(models...); err != nil {
		t.Fatal(err)
	}
	if err := old.db.AutoMigrate(models...); err != nil {
		t.Fatal(err)
	}
	o := mustOrg(t, old, "alpha")
	ada := must(old.CreateUser("ada@example.com", "Ada Lovelace", "correct horse battery"))
	if err := old.AddMember(o.ID, ada.ID, "owner", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	hannah := must(old.QuickAddPerson(o, QuickAdd{Name: "Hannah Pryce", Company: "Larkmoor Yachts", Email: "hannah@larkmoor.example"}, "Ada Lovelace"))
	a := must(old.AddAgreement(o.ID, *hannah.CompanyID, "Service agreement", "Ada Lovelace"))
	must(old.PutAgreementFile(o.ID, a.ID, "service.pdf", testPDF(1, ""), "Ada Lovelace"))
	r := must(old.AddRecipient(o, a.ID, RecipientSigner, nil, &ada.ID, "Ada Lovelace"))
	must(old.PutAgreementFields(o.ID, a.ID, []AgreementField{{RecipientID: r.ID, Page: 1, Kind: "signature", X: 0.1, Y: 0.7, W: 0.3, H: 0.05}}))
	for model, columns := range map[any][]string{
		&Agreement{}:          {"sent_at", "sent_by", "sent_by_user", "sent_org", "expires_at", "subject", "message", "sealed_hash", "seal_problem", "completed_at"},
		&AgreementRecipient{}: {"sent_name", "sent_email", "status", "signed_at", "typed_name", "signed_via", "decline_reason", "erased"},
		&AgreementField{}:     {"label", "value"},
	} {
		for _, c := range columns {
			if err := old.db.Migrator().DropColumn(model, c); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, m := range []any{&AgreementEvent{}, &SealKey{}, &AgreementSealed{}, &SignerLink{}, &SignerSession{}, &AgreementMail{}} {
		if err := old.db.Migrator().DropTable(m); err != nil {
			t.Fatal(err)
		}
	}

	s := must(Open(dsn))
	s.keys = mustKeys(t, "1:"+testKey)
	if got := must(s.Agreement(o.ID, a.ID)); got.Status != AgreementDraft || got.SentBy != "" {
		t.Fatalf("the draft after the upgrade: %+v", got)
	}
	must(s.SendAgreement(o, a.ID, SendIn{}, ada, "Ada Lovelace"))
	if _, err := s.MemberSign(o, a.ID, ada, SignIn{Typed: "Ada Lovelace", Consent: true}, Who{}); err != nil {
		t.Fatalf("signing an agreement from before: %v", err)
	}
	if got := must(s.Agreement(o.ID, a.ID)); got.Status != AgreementSealing {
		t.Errorf("after its only signer signed: %s", got.Status)
	}
}

// One that can't be sealed can be withdrawn once a try has failed, so it isn't stuck.
func TestAnAgreementThatWontSealCanBeWithdrawn(t *testing.T) {
	x := newSigningSite(t)
	must(x.s.SendAgreement(x.o, x.a.ID, SendIn{}, x.ada, "Ada Lovelace"))
	if err := x.s.db.Model(&Agreement{}).Where("id = ?", x.a.ID).Update("status", AgreementSealing).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := x.s.VoidAgreement(x.o.ID, x.a.ID, "", "Ada Lovelace"); err == nil {
		t.Error("withdrawn while it was being sealed")
	}
	if err := x.s.db.Model(&Agreement{}).Where("id = ?", x.a.ID).Update("seal_problem", "the key wouldn't open").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := x.s.VoidAgreement(x.o.ID, x.a.ID, "", "Ada Lovelace"); err != nil {
		t.Errorf("withdrawing one that won't seal: %v", err)
	}
}

// Once sent, a draft's fields can't be changed under its signers.
func TestASentAgreementTakesNoMoreChanges(t *testing.T) {
	x := newSigningSite(t)
	must(x.s.SendAgreement(x.o, x.a.ID, SendIn{}, x.ada, "Ada Lovelace"))
	if _, err := x.s.PutAgreementFields(x.o.ID, x.a.ID, nil); err == nil {
		t.Error("its fields changed after it was sent")
	}
}

// A sender who has left the organisation hears nothing of a decline.
func TestADeclineIsntMailedToASenderWhoLeft(t *testing.T) {
	x := newSigningSite(t)
	must(x.s.SendAgreement(x.o, x.a.ID, SendIn{}, x.ada, "Ada Lovelace"))
	x.round()
	_, link := x.mailTo("hannah@larkmoor.example")
	_, g := x.proveAs(link, "hannah@larkmoor.example")
	must(x.s.SignerDeclines(g, "No", Who{}))
	grace := must(x.s.CreateUser("grace@example.com", "Grace Hopper", "correct horse battery"))
	if err := x.s.AddMember(x.o.ID, grace.ID, "owner", "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	if _, err := x.s.RemoveMember(x.o.ID, x.ada.ID, "Grace Hopper"); err != nil {
		t.Fatal(err)
	}
	x.round()
	for _, to := range x.box.to() {
		if to == "ada@example.com" {
			t.Error("the decline went to a sender who has left")
		}
	}
}
