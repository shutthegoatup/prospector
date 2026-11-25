package main

import (
	"errors"
	"net/http"
	"net/mail"
	"strings"
	"testing"
	"time"
)

func TestAReplyToAnEnquiryIsEmailedAndKept(t *testing.T) {
	s, srv, member, box := mailSite(t, nil)
	o := must(s.OrgBySlug("alpha"))
	// Whoever filled the form in chose every field of it.
	e := must(s.AddEnquiry(o.ID, "form", EnquiryIn{Name: "Hannah\r\nBcc: victim@elsewhere.example", Email: "hannah@larkmoor.example",
		Company: "Larkmoor Yachts", Message: "BUY CHEAP WATCHES at https://spam.example\r\nBcc: victim@elsewhere.example"}))
	base := srv.URL + "/o/alpha/api/v1/enquiries/" + itoa(e.ID)
	state := func() Enquiry {
		t.Helper()
		var now Enquiry
		if err := s.db.First(&now, e.ID).Error; err != nil {
			t.Fatal(err)
		}
		return now
	}
	rows := func() int64 {
		t.Helper()
		var n int64
		if err := s.db.Model(&EnquiryMessage{}).Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		return n
	}

	if _, page := call(t, member, "GET", srv.URL+"/o/alpha/enquiries", "", nil); !strings.Contains(page, `data-form="api/v1/enquiries/`+itoa(e.ID)+`/replies"`) {
		t.Error("the enquiries page has no form to reply with")
	}
	for why, text := range map[string]string{"nothing": "  \n ", "too much": strings.Repeat("a", replyLimit+1)} {
		if code, body := call(t, member, "POST", base+"/replies", "application/json", map[string]any{"text": text}); code != http.StatusBadRequest {
			t.Errorf("a reply of %s: %d %s", why, code, body)
		}
	}
	box.take(t, 0)

	code, body := call(t, member, "POST", base+"/replies", "application/json", map[string]any{"text": "Thanks for writing.\nWe can do Tuesday."})
	if code != http.StatusCreated || !strings.Contains(body, `"ours":true`) {
		t.Fatalf("replying: %d %s", code, body)
	}
	got := box.take(t, 1)[0]
	if got.To != "hannah@larkmoor.example" || got.ReplyTo != "ada@example.com" || got.Body != "Thanks for writing.\nWe can do Tuesday.\n" {
		t.Errorf("the reply as mailed: %+v", got.Message)
	}
	// Nothing they sent us is in what we send: a form filled in with someone else's
	// address can't be made to carry the sender's words to them.
	wire := string(compose(&mail.Address{Address: "site@prospector.example"}, &mail.Address{Address: got.To}, got.Message, time.Now()))
	for _, theirs := range []string{"WATCHES", "spam.example", "victim", "Hannah", "Bcc"} {
		if strings.Contains(wire, theirs) {
			t.Errorf("the mail carries %q from the enquiry:\n%s", theirs, wire)
		}
	}
	if !strings.Contains(wire, "\r\nReply-To: <ada@example.com>\r\n") {
		t.Errorf("the mail doesn't say where to answer:\n%s", wire)
	}
	if now := state(); now.State != EnquiryWaiting || now.Owner != "Ada Lovelace" {
		t.Errorf("after replying the enquiry is %q, owned by %q", now.State, now.Owner)
	}

	// The same words again are a second press of the button, not a second mail.
	if code, body := call(t, member, "POST", base+"/replies", "application/json", map[string]any{"text": "Thanks for writing.\nWe can do Tuesday."}); code != http.StatusBadRequest || !strings.Contains(body, "just been sent") {
		t.Errorf("the same reply twice: %d %s", code, body)
	}
	box.take(t, 0)

	// Their answer is recorded, sends nothing, and makes it ours to answer again.
	if code, body := call(t, member, "POST", base+"/answers", "application/json", map[string]any{"text": "Tuesday is fine."}); code != http.StatusCreated || !strings.Contains(body, `"ours":false`) {
		t.Fatalf("recording their answer: %d %s", code, body)
	}
	box.take(t, 0)
	if now := state(); now.State != EnquiryOpen {
		t.Errorf("after their answer the enquiry is %q", now.State)
	}
	code, body = call(t, member, "GET", base+"/messages", "", nil)
	if code != http.StatusOK || strings.Index(body, "We can do Tuesday") < 0 || strings.Index(body, "We can do Tuesday") > strings.Index(body, "Tuesday is fine") {
		t.Errorf("the thread: %d %s", code, body)
	}
	if _, page := call(t, member, "GET", srv.URL+"/o/alpha/enquiries", "", nil); !strings.Contains(page, "We can do Tuesday.") || !strings.Contains(page, "Tuesday is fine.") {
		t.Error("the enquiries page doesn't show the thread")
	}

	// A send that fails records nothing and changes nothing.
	before := rows()
	box.mu.Lock()
	box.fail = errors.New("the mail server is away")
	box.mu.Unlock()
	if code, body := call(t, member, "POST", base+"/replies", "application/json", map[string]any{"text": "Are you still there?"}); code != http.StatusBadGateway || !strings.Contains(body, "nothing was recorded") {
		t.Errorf("a reply the mail server refused: %d %s", code, body)
	}
	box.mu.Lock()
	box.fail = nil
	box.mu.Unlock()
	if now := state(); rows() != before || now.State != EnquiryOpen {
		t.Errorf("a failed send left %d rows and the enquiry %q", rows()-before, now.State)
	}
	// And doesn't count as having just been sent.
	if code, body := call(t, member, "POST", base+"/replies", "application/json", map[string]any{"text": "Are you still there?"}); code != http.StatusCreated {
		t.Errorf("the same reply once the server was back: %d %s", code, body)
	}
	box.take(t, 1)

	// Their words are in what the person can ask for, and go when the enquiry goes.
	c := must(s.ConverseEnquiry(o, e.ID, "Ada Lovelace"))
	if export := must(s.ExportPerson(o.ID, c.ID, "Ada Lovelace")); len(export.EnquiryThread) != 3 {
		t.Errorf("the person's export has %d of the 3 messages", len(export.EnquiryThread))
	}
	if export := must(s.ExportOrg(o.ID, "Ada Lovelace")); len(export.EnquiryThread) != 3 {
		t.Errorf("the organisation's export has %d of the 3 messages", len(export.EnquiryThread))
	}
	if err := s.ErasePerson(o.ID, c.ID, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	if rows() != 0 {
		t.Errorf("erasing them left %d messages", rows())
	}
}

func TestWhoCanBeRepliedToAndByWhom(t *testing.T) {
	s, srv, member, box := mailSite(t, nil)
	o := must(s.OrgBySlug("alpha"))
	other := mustOrg(t, s, "beta")
	key := func(org int64, scopes ...string) string {
		secret, _, err := s.CreateKey(org, "robot "+strings.Join(scopes, "+"), scopes, "Ada Lovelace")
		if err != nil {
			t.Fatal(err)
		}
		return secret
	}
	reader, replier, outsider := key(o.ID, "read", "enquiries:write"), key(o.ID, "enquiries:reply"), key(other.ID, "read", "enquiries:reply")
	real := must(s.AddEnquiry(o.ID, "form", EnquiryIn{Name: "Hannah Pryce", Email: "hannah@larkmoor.example", Message: "Do you cover Leith?"}))
	base := srv.URL + "/o/alpha/api/v1/enquiries/"
	text := map[string]any{"text": "Yes, we do."}

	if code, _ := asKey(t, "POST", base+itoa(real.ID)+"/replies", "", text); code != http.StatusUnauthorized {
		t.Errorf("nobody at all replying: %d", code)
	}
	for _, path := range []string{"/replies", "/answers"} {
		if code, body := asKey(t, "POST", base+itoa(real.ID)+path, reader, text); code != http.StatusForbidden || !strings.Contains(body, "enquiries:reply") {
			t.Errorf("a key without enquiries:reply posting to %s: %d %s", path, code, body)
		}
	}
	// Another organisation's key, here and against its own address with our enquiry's number.
	if code, _ := asKey(t, "POST", base+itoa(real.ID)+"/replies", outsider, text); code == http.StatusCreated {
		t.Error("another organisation's key replied to ours")
	}
	for _, path := range []string{"/replies", "/answers", "/messages"} {
		method := "POST"
		if path == "/messages" {
			method = "GET"
		}
		if code, body := asKey(t, method, srv.URL+"/o/beta/api/v1/enquiries/"+itoa(real.ID)+path, outsider, text); code != http.StatusNotFound {
			t.Errorf("another organisation reaching our enquiry by number at %s: %d %s", path, code, body)
		}
	}
	box.take(t, 0)

	// A key's reply goes out, with no mailbox of its own to be answered to, and doesn't
	// make itself the owner.
	if code, body := asKey(t, "POST", base+itoa(real.ID)+"/replies", replier, text); code != http.StatusCreated {
		t.Fatalf("a key with the scope replying: %d %s", code, body)
	}
	if got := box.take(t, 1)[0]; got.To != "hannah@larkmoor.example" || got.ReplyTo != "" {
		t.Errorf("the key's reply as mailed: %+v", got.Message)
	}
	var now Enquiry
	if err := s.db.First(&now, real.ID).Error; err != nil || now.Owner != "" || now.State != EnquiryWaiting {
		t.Errorf("after a key's reply: %+v (%v)", now, err)
	}

	// Not spam, not an address that isn't one, not someone on the never-contact list.
	spam := must(s.AddEnquiry(o.ID, "form", EnquiryIn{Name: "Bot", Email: "bot@spam.example", Message: "hello", Trap: "caught"}))
	if !spam.Spam {
		if err := s.db.Model(&Enquiry{}).Where("id = ?", spam.ID).Update("spam", true).Error; err != nil {
			t.Fatal(err)
		}
	}
	two := must(s.AddEnquiry(o.ID, "form", EnquiryIn{Name: "Two", Email: "one@larkmoor.example", Message: "hello"}))
	if err := s.db.Model(&Enquiry{}).Where("id = ?", two.ID).Update("email", "one@larkmoor.example, two@elsewhere.example").Error; err != nil {
		t.Fatal(err)
	}
	never := must(s.AddEnquiry(o.ID, "form", EnquiryIn{Name: "Rival", Email: "someone@rival.example", Message: "hello"}))
	o.Settings.NeverContact = "rival.example"
	if err := s.SaveOrg(o); err != nil {
		t.Fatal(err)
	}
	for why, id := range map[string]int64{"spam": spam.ID, "two addresses": two.ID, "the never-contact list": never.ID, "an enquiry that isn't there": 99999} {
		if code, body := call(t, member, "POST", base+itoa(id)+"/replies", "application/json", text); code == http.StatusCreated {
			t.Errorf("a reply went to %s: %s", why, body)
		}
	}
	box.take(t, 0)
	var kept int64
	if err := s.db.Model(&EnquiryMessage{}).Where("enquiry_id <> ?", real.ID).Count(&kept).Error; err != nil || kept != 0 {
		t.Errorf("refused replies left %d rows (%v)", kept, err)
	}

	// Deleting an enquiry takes its thread.
	if err := s.DeleteEnquiry(o.ID, real.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.db.Model(&EnquiryMessage{}).Count(&kept).Error; err != nil || kept != 0 {
		t.Errorf("deleting the enquiry left %d messages (%v)", kept, err)
	}
}

func TestNoReplyWithoutAMailServer(t *testing.T) {
	s, srv, member, box := mailSite(t, func(a *App) { a.mail = nil })
	o := must(s.OrgBySlug("alpha"))
	e := must(s.AddEnquiry(o.ID, "form", EnquiryIn{Name: "Hannah Pryce", Email: "hannah@larkmoor.example", Message: "Do you cover Leith?"}))
	base := srv.URL + "/o/alpha/api/v1/enquiries/" + itoa(e.ID)
	if code, body := call(t, member, "POST", base+"/replies", "application/json", map[string]any{"text": "Yes."}); code != http.StatusBadRequest || !strings.Contains(body, "mail isn't set up") {
		t.Errorf("replying with no mail server: %d %s", code, body)
	}
	box.take(t, 0)
	_, page := call(t, member, "GET", srv.URL+"/o/alpha/enquiries", "", nil)
	if strings.Contains(page, "/replies") || !strings.Contains(page, "/answers") || !strings.Contains(page, "sets a mail server") {
		t.Error("with no mail server the page offers to send, or doesn't offer to record their answer")
	}
	// Their answer can still be recorded: it sends nothing.
	if code, body := call(t, member, "POST", base+"/answers", "application/json", map[string]any{"text": "Never mind."}); code != http.StatusCreated {
		t.Errorf("recording an answer with no mail server: %d %s", code, body)
	}
}

func TestAReplyToAddressCannotAddAHeader(t *testing.T) {
	from, to := &mail.Address{Address: "site@prospector.example"}, &mail.Address{Address: "hannah@larkmoor.example"}
	for _, answer := range []string{"ada@example.com\r\nBcc: victim@elsewhere.example", "not an address", ""} {
		wire := string(compose(from, to, Message{Subject: "Your message", Body: "Hello", ReplyTo: answer}, time.Now()))
		if strings.Contains(wire, "Bcc") || strings.Contains(wire, "Reply-To") {
			t.Errorf("a reply-to of %q made it into the mail:\n%s", answer, wire)
		}
	}
}

// What the review of replies found: an address dressed up to dodge erasure, replies
// without end, a reply that never left showing in an export, and the mail server's own
// words in an answer.
func TestRepliesKeepToTheirLimits(t *testing.T) {
	s, srv, member, box := mailSite(t, nil)
	o := must(s.OrgBySlug("alpha"))
	for _, dressed := range []string{"Bob<bob@larkmoor.example>", "bob@larkmoor.example(c)", "\"Bob\" <bob@larkmoor.example>", "a@larkmoor.example,b@larkmoor.example"} {
		if _, err := s.AddEnquiry(o.ID, "form", EnquiryIn{Name: "Bob", Email: dressed, Message: "hello"}); err == nil {
			t.Errorf("an enquiry was taken from %q, which is more than an address", dressed)
		}
	}
	e := must(s.AddEnquiry(o.ID, "form", EnquiryIn{Name: "Hannah Pryce", Email: "Hannah@Larkmoor.example", Message: "Do you cover Leith?"}))
	base := srv.URL + "/o/alpha/api/v1/enquiries/" + itoa(e.ID)
	reply := func(text string) (int, string) {
		return call(t, member, "POST", base+"/replies", "application/json", map[string]any{"text": text})
	}

	// A reply that never left is in neither export nor the thread.
	if err := s.db.Create(&EnquiryMessage{OrgID: o.ID, EnquiryID: e.ID, Ours: true, Text: "never left", By: "Ada Lovelace", CreatedAt: time.Now().Add(-time.Hour)}).Error; err != nil {
		t.Fatal(err)
	}
	if export := must(s.ExportOrg(o.ID, "Ada Lovelace")); len(export.EnquiryThread) != 0 {
		t.Errorf("the export carries a reply that never left: %+v", export.EnquiryThread)
	}

	// The mail server's own words stay in the log.
	box.mu.Lock()
	box.fail = errors.New("reach smtp.internal.example:587: dial tcp 10.0.0.9: refused")
	box.mu.Unlock()
	if code, body := reply("Yes."); code != http.StatusBadGateway || strings.Contains(body, "smtp.internal") || strings.Contains(body, "10.0.0.9") {
		t.Errorf("a refused send answered %d %s", code, body)
	}
	box.mu.Lock()
	box.fail = nil
	box.mu.Unlock()

	// Three in a row, abandoned sends not counted, and then no more until they answer.
	for i, text := range []string{"Yes.", "Did that reach you?", "One last try."} {
		if code, body := reply(text); code != http.StatusCreated {
			t.Fatalf("reply %d: %d %s", i+1, code, body)
		}
	}
	box.take(t, 3)
	if code, body := reply("And again."); code != http.StatusBadRequest || !strings.Contains(body, "no answer recorded") {
		t.Errorf("a fourth reply with no answer: %d %s", code, body)
	}
	box.take(t, 0)
	if code, body := call(t, member, "POST", base+"/answers", "application/json", map[string]any{"text": "Got them all, thanks."}); code != http.StatusCreated {
		t.Fatalf("recording their answer: %d %s", code, body)
	}
	if code, body := reply("Good."); code != http.StatusCreated {
		t.Errorf("a reply after their answer: %d %s", code, body)
	}
	box.take(t, 1)

	// Someone erased is not written to, though their enquiry was never matched to them.
	bob := must(s.QuickAddPerson(o, QuickAdd{Name: "Bob Carr", Company: "Larkmoor Yachts", Email: "bob@larkmoor.example"}, "Ada Lovelace"))
	if err := s.ErasePerson(o.ID, bob.ID, "Ada Lovelace"); err != nil {
		t.Fatal(err)
	}
	if err := s.db.Create(&Enquiry{OrgID: o.ID, Kind: "other", Email: "Bob <bob@larkmoor.example>", Message: "from before addresses were checked", Via: "form", State: EnquiryOpen, CreatedAt: time.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	var old Enquiry
	if err := s.db.Where("message like ?", "from before%").First(&old).Error; err != nil {
		t.Fatal(err)
	}
	if code, body := call(t, member, "POST", srv.URL+"/o/alpha/api/v1/enquiries/"+itoa(old.ID)+"/replies", "application/json", map[string]any{"text": "Hello Bob."}); code == http.StatusCreated || !strings.Contains(body, "erased") {
		t.Errorf("a reply to someone erased: %d %s", code, body)
	}
	box.take(t, 0)
}
