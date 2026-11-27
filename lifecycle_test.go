package main

import (
	"fmt"
	"slices"
	"testing"
	"time"
)

// everyContact is one contact per combination of the fields a contact's standing is read from.
func everyContact() []Contact {
	var out []Contact
	for i := 0; i < 1<<8; i++ {
		on := func(bit int) bool { return i&(1<<bit) != 0 }
		// An empty mark counts as set, as it does to "is null" in SQL.
		opt := func(set bool, v string) *string {
			if !set {
				return nil
			}
			if on(7) {
				v = ""
			}
			return &v
		}
		c := Contact{Key: fmt.Sprintf("p%03d", i), File: "test", Person: str("Person"), Note: str("Hello"),
			Hold: opt(on(0), "2020-01-01"), Replied: opt(on(1), "2020-01-02"), Declined: opt(on(2), "2020-01-03"),
			EmailSent: opt(on(3), "2020-01-04")}
		if on(4) {
			c.Status = ContactSuggested
		}
		if on(5) {
			c.Stage = "meeting"
		}
		if on(6) {
			c.Key = "org:" + c.Key
		}
		out = append(out, c)
	}
	return out
}

func TestContactStageNames(t *testing.T) {
	t.Parallel()
	d := str("2020-01-01")
	for _, tc := range []struct {
		c    Contact
		want string
	}{
		{Contact{}, "not contacted"},
		{Contact{LinkedinAdded: d}, "in outreach"},
		{Contact{EmailSent: d}, "in outreach"},
		{Contact{Called: d}, "in outreach"},
		{Contact{Accepted: d}, "not contacted"},
		{Contact{EmailSent: d, Hold: d}, "hold"},
		{Contact{EmailSent: d, Replied: d}, "replied"},
		{Contact{Replied: d, Stage: "proposal"}, "proposal"},
		{Contact{Replied: d, Hold: d}, "replied"},
		{Contact{Replied: d, Stage: "meeting", Declined: d}, "declined"},
		{Contact{Status: ContactSuggested, Replied: d, Declined: d}, "suggested"},
	} {
		if got := contactStage(tc.c); got != tc.want {
			t.Errorf("contactStage(%+v) = %q, want %q", tc.c, got, tc.want)
		}
	}
}

func TestPipelineTakesTheFurthestContact(t *testing.T) {
	t.Parallel()
	d := str("2020-01-01")
	for _, tc := range []struct {
		name   string
		c      Company
		people []Contact
		want   string
	}{
		{"nobody", Company{}, nil, "research"},
		{"rejected", Company{Stage: CompanyRejected}, nil, "rejected"},
		{"untouched", Company{}, []Contact{{}}, "research"},
		{"touched", Company{}, []Contact{{Called: d}}, "outreach"},
		{"rejected but touched", Company{Stage: CompanyRejected}, []Contact{{Called: d}}, "outreach"},
		{"replied", Company{}, []Contact{{Called: d}, {Replied: d}}, "replied"},
		{"furthest wins", Company{}, []Contact{{Replied: d, Stage: "proposal"}, {Replied: d, Stage: "meeting"}}, "proposal"},
		{"lost ranks with outreach", Company{}, []Contact{{Replied: d, Stage: "lost"}, {Replied: d}}, "replied"},
		{"held still counts", Company{}, []Contact{{EmailSent: d, Hold: d}}, "outreach"},
		{"a reply later declined is no conversation", Company{}, []Contact{{Replied: d, Stage: "meeting", Declined: d}}, "outreach"},
		{"declined before anyone reached them", Company{Stage: CompanyRejected}, []Contact{{Declined: d}}, "rejected"},
		{"declined beside a live conversation", Company{}, []Contact{{Replied: d, Stage: "proposal", Declined: d}, {Replied: d}}, "replied"},
	} {
		if got := pipeline(tc.c, tc.people); got != tc.want {
			t.Errorf("%s: pipeline = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// The queues and the board are SQL; the names shown for a contact are Go. Both must
// read a contact's standing the same way.
func TestQueriesAgreeWithPhase(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	all := everyContact()
	for i := range all {
		all[i].OrgID = o.ID
	}
	if err := s.DB.CreateInBatches(all, 50).Error; err != nil {
		t.Fatal(err)
	}
	keys := func(rows []Contact) []string {
		var out []string
		for _, r := range rows {
			out = append(out, r.Key)
		}
		slices.Sort(out)
		return out
	}
	var wantDue, wantBoard []Contact
	for _, c := range all {
		if c.Open() {
			wantDue = append(wantDue, c)
		}
		if c.Phase() == PhaseConversation {
			wantBoard = append(wantBoard, c)
		}
	}
	if len(wantDue) == 0 || len(wantBoard) == 0 {
		t.Fatal("the combinations cover no open contact or no conversation")
	}

	due, err := s.Due(o.ID, "linkedin", time.Now(), o.Settings)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := keys(due), keys(wantDue); !slices.Equal(got, want) {
		t.Errorf("the LinkedIn queue holds %v, but the open contacts are %v", got, want)
	}

	board, err := s.Conversations(o.ID)
	if err != nil {
		t.Fatal(err)
	}
	var onBoard []Contact
	for stage, list := range board {
		for _, c := range list {
			if c.ConversationStage() != stage {
				t.Errorf("%s is under %q but its stage is %q", c.Key, stage, c.ConversationStage())
			}
		}
		onBoard = append(onBoard, list...)
	}
	if got, want := keys(onBoard), keys(wantBoard); !slices.Equal(got, want) {
		t.Errorf("the board holds %v, but the conversations are %v", got, want)
	}
}

func TestRejectedCompanysPeopleLeaveTheQueues(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	co := Company{OrgID: o.ID, Name: "Larkmoor", Stage: CompanyQualified}
	if err := s.DB.Create(&co).Error; err != nil {
		t.Fatal(err)
	}
	old := str("2020-01-01")
	people := []Contact{
		{OrgID: o.ID, Key: "new", File: "test", Person: str("Hannah Pryce"), Note: str("Hello"), CompanyID: &co.ID},
		{OrgID: o.ID, Key: "accepted", File: "test", Person: str("Jonas Weber"), LinkedinAdded: old, Accepted: old, CompanyID: &co.ID},
		{OrgID: o.ID, Key: "call", File: "test", Person: str("Nia Okafor"), Phone: str("+44 20 7946 0000"), CompanyID: &co.ID},
		{OrgID: o.ID, Key: "email", File: "test", Person: str("Max Muster"), LinkedinAdded: old, CompanyID: &co.ID},
		{OrgID: o.ID, Key: "followup", File: "test", Person: str("Ines Duarte"), EmailSent: old, CompanyID: &co.ID},
		{OrgID: o.ID, Key: "elsewhere", File: "test", Person: str("Tomas Reed"), Note: str("Hello")},
	}
	if err := s.DB.Create(&people).Error; err != nil {
		t.Fatal(err)
	}
	queued := func() int {
		n := 0
		for _, step := range []string{"linkedin", "accepted", "call", "email", "followup"} {
			rows, err := s.Due(o.ID, step, time.Now(), o.Settings)
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range rows {
				if r.CompanyID != nil {
					n++
				} else if r.Key != "elsewhere" {
					t.Fatalf("%s is queued with no company", r.Key)
				}
			}
		}
		return n
	}
	before := queued()
	if before < 5 {
		t.Fatalf("only %d queue entries before rejecting: the test covers too little", before)
	}
	if err := s.SetCompanyStage(o.ID, co.ID, CompanyRejected, "", "ada"); err != nil {
		t.Fatal(err)
	}
	if n := queued(); n != 0 {
		t.Fatalf("%d people at a rejected company are still queued", n)
	}
	if rows, _ := s.Due(o.ID, "linkedin", time.Now(), o.Settings); len(rows) != 1 || rows[0].Key != "elsewhere" {
		t.Fatalf("rejecting one company took someone with no company out of the queue: %+v", rows)
	}
	listed := *o
	listed.Settings.NeverContact = "larkmoor.example"
	if err := s.DB.Model(&Contact{}).Where("key = ?", "new").Update("email", "hannah.pryce@larkmoor.example").Error; err != nil {
		t.Fatal(err)
	}
	if names := s.NeverContactCovers(&listed); len(names) != 0 {
		t.Fatalf("the never-contact list is said to hold back %v, whose company is rejected anyway", names)
	}
	if err := s.SetCompanyStage(o.ID, co.ID, CompanyQualified, "", "ada"); err != nil {
		t.Fatal(err)
	}
	if names := s.NeverContactCovers(&listed); len(names) != 1 {
		t.Fatalf("the never-contact list holds back %v once the company is qualified again", names)
	}
	if n := queued(); n != before {
		t.Fatalf("%d queued after qualifying the company again, %d before it was rejected", n, before)
	}
}

func TestStageIsOneOfTheBoardsColumns(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	c := Contact{OrgID: o.ID, Key: "hannah", File: "test", Person: str("Hannah Pryce")}
	if err := s.DB.Create(&c).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.Update(o.ID, c.ID, "stage", "meeting", "test"); err == nil {
		t.Fatal("someone who hasn't replied was given a conversation stage")
	}
	if err := s.Mark(o.ID, c.ID, "replied", "2020-01-02", "test"); err != nil {
		t.Fatal(err)
	}
	if err := s.Update(o.ID, c.ID, "stage", "nearly", "test"); err == nil {
		t.Fatal("a stage the board has no column for was saved")
	}
	for _, st := range Stages {
		if err := s.Update(o.ID, c.ID, "stage", st.Key, "test"); err != nil {
			t.Fatalf("stage %q refused: %v", st.Key, err)
		}
	}
	if err := s.Update(o.ID, c.ID, "stage", "replied", "test"); err != nil {
		t.Fatalf("a lost conversation couldn't be reopened: %v", err)
	}
	var events int64
	s.InOrg(o.ID).Model(&Event{}).Where("action = ?", "set:stage").Count(&events)
	if int(events) != len(Stages)+1 {
		t.Fatalf("%d stage changes logged, want %d: a refused one left a history line", events, len(Stages)+1)
	}
}

func TestMarkChecksApplyToEveryCaller(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	suggested := Contact{OrgID: o.ID, Key: "jonas", File: "test", Person: str("Jonas Weber"), Status: ContactSuggested}
	picked := Contact{OrgID: o.ID, Key: "hannah", File: "test", Person: str("Hannah Pryce")}
	for _, c := range []*Contact{&suggested, &picked} {
		if err := s.DB.Create(c).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Mark(o.ID, suggested.ID, "linkedin", today(), "test"); err == nil {
		t.Fatal("a suggestion nobody picked was marked as contacted")
	}
	for _, bad := range []string{"", "yesterday", "02/01/2020", "2999-01-01"} {
		if err := s.Mark(o.ID, picked.ID, "linkedin", bad, "test"); err == nil {
			t.Fatalf("mark accepted the date %q", bad)
		}
	}
	if err := s.Mark(o.ID, picked.ID, "linkedin", today(), "test"); err != nil {
		t.Fatal(err)
	}
}

func TestMarkDateIsJudgedInLocalTime(t *testing.T) {
	t.Parallel()
	east := time.FixedZone("east", 2*60*60)
	justAfterMidnight := time.Date(2024, 3, 10, 0, 30, 0, 0, east)
	if err := markDate("2024-03-10", justAfterMidnight); err != nil {
		t.Fatalf("today was refused just after midnight east of UTC: %v", err)
	}
	if err := markDate("2024-03-11", justAfterMidnight); err == nil {
		t.Fatal("tomorrow was accepted")
	}
}

func TestPickingSomeoneQualifiesTheirCompanyOnTheRecord(t *testing.T) {
	t.Parallel()
	s := tempStore(t)
	o := mustOrg(t, s, "alpha")
	co := Company{OrgID: o.ID, Name: "Larkmoor", Stage: CompanyReview}
	if err := s.DB.Create(&co).Error; err != nil {
		t.Fatal(err)
	}
	people := []Contact{
		{OrgID: o.ID, Key: "hannah", File: "test", Person: str("Hannah Pryce"), Status: ContactSuggested, CompanyID: &co.ID},
		{OrgID: o.ID, Key: "jonas", File: "test", Person: str("Jonas Weber"), Status: ContactSuggested, CompanyID: &co.ID},
	}
	if err := s.DB.Create(&people).Error; err != nil {
		t.Fatal(err)
	}
	qualified := func() int64 {
		var n int64
		s.InOrg(o.ID).Model(&Event{}).Where("company_id = ? and action = ? and \"user\" = ?", co.ID, "company:"+CompanyQualified, "ada").Count(&n)
		return n
	}
	if err := s.PickContact(o.ID, o, people[0].ID, "ada"); err != nil {
		t.Fatal(err)
	}
	back, err := s.CompanyByID(o.ID, co.ID)
	if err != nil || back.Stage != CompanyQualified {
		t.Fatalf("company stage is %q after a pick (%v)", back.Stage, err)
	}
	if qualified() != 1 {
		t.Fatal("the company was qualified by a pick with no line in its history")
	}
	if err := s.PickContact(o.ID, o, people[1].ID, "ada"); err != nil {
		t.Fatal(err)
	}
	if qualified() != 1 {
		t.Fatal("picking a second person logged the company as qualified again")
	}
}
