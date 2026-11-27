package main

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"time"

	"gorm.io/gorm"
)

// Methods that aren't about one organisation: people and their sessions, making
// organisations, and looking one up to begin with. Every other exported method of
// the store has to take its organisation first, and is tried below as another one.
var acrossOrganisations = map[string]bool{
	"CreateUser": true, "InviteUser": true, "SetPassword": true, "ChangePassword": true, "UserByEmail": true,
	"Login": true, "NewSession": true, "AddProof": true, "SessionFor": true, "SessionUser": true, "Logout": true,
	"ResetToken": true, "InviteToken": true, "PasswordTokenUser": true, "RedeemPasswordToken": true,
	"CreateOrg": true, "CreateOrgWithOwner": true, "EnsureFrontOrg": true, "OrgByID": true, "OrgBySlug": true, "OrgsFor": true,
	"KeyFor": true, "Reseal": true, "SSOOrgsFor": true, "SocialUser": true, "SSOUser": true,
	"KnownDevice": true, "DropInvites": true,
	// Finds a customer by the secret in their link or cookie, which is all they have.
	"CustomerByToken": true, "EndCustomerSession": true, "DropCustomerLink": true,
	// An unsubscribe link names its organisation itself; whoever holds it has no session.
	"UnsubscribeByToken": true, "DropUnsubscribeLink": true,
	// A webhook names its organisation by slug, and proves it with the secret.
	"BounceHookOrg": true,
	// Whether a domain takes mail is the same for everyone who writes to it.
	"TakesMail": true, "NoMailDomains": true, "CheckSomeMail": true, "CheckMailDomains": true,
	// What the server does as it starts.
	"ResealAtStart": true, "FrontFormOff": true,
	// These take a row that already names its organisation, which their callers read with it.
	"SaveOrgMail": true, "SaveSSO": true,
	// The server's own sweeps, over every organisation; what they find is then handled
	// by methods that take its organisation.
	"LapsedRuns": true, "CampaignsSending": true, "FreeStaleMail": true, "Users": true,
	// The search indexes, built at start-up for everyone's rows.
	"IndexSearch": true,
}

// theirs marks what the first organisation holds, so it shows if it turns up elsewhere.
// findable is in the same rows and is searched for: a method that only says back what
// it was asked then says findable, not theirs.
const (
	theirs      = "zzalpha"
	findable    = "quillon"
	theirEmail  = "vale@quillon.example"
	theirDomain = "quillon.example"
)

// tryWords go into every text argument: an id, the names a method checks before it
// looks anything up, and what would find the first organisation's rows by search.
var tryWords = []string{"1", "", findable, theirEmail, theirDomain, "phone", "replied", "accepted", "note", "skipped", "qualified", "review", "2020-01-02", "linkedin"}

type intruder struct {
	org  *Org
	word string
	// spoken are the words for a method's own text arguments, in order.
	spoken  []string
	yes     bool
	sawBool bool
}

func (in *intruder) value(t reflect.Type, name string, own bool) reflect.Value {
	v := reflect.New(t).Elem()
	switch {
	case t == reflect.TypeOf(&Org{}):
		return reflect.ValueOf(in.org)
	case t == reflect.TypeOf(time.Time{}):
		return reflect.ValueOf(time.Now())
	case t == reflect.TypeOf([]Contact{}):
		// A person from the first organisation, as a page might hand back.
		return reflect.ValueOf([]Contact{{ID: 1, OrgID: 1}})
	}
	switch t.Kind() {
	case reflect.Int64, reflect.Int:
		v.SetInt(1)
	case reflect.Bool:
		in.sawBool = true
		v.SetBool(in.yes)
	case reflect.String:
		word := in.word
		if own {
			word, in.spoken = in.spoken[0], in.spoken[1:]
		}
		switch {
		case strings.Contains(name, "URL") || name == "Linkedin":
			word = "https://intruder.example/" + word
		case name == "Email" && !strings.Contains(word, "@"):
			word = "someone@intruder.example"
		case name == "Date":
			word = "2020-01-02"
		}
		v.SetString(word)
	case reflect.Pointer:
		p := reflect.New(t.Elem())
		p.Elem().Set(in.value(t.Elem(), name, false))
		return p
	case reflect.Struct:
		for i := range t.NumField() {
			if f := t.Field(i); f.IsExported() {
				v.Field(i).Set(in.value(f.Type, f.Name, false))
			}
		}
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Int64 {
			return reflect.ValueOf([]int64{1})
		}
	}
	return v
}

func (in *intruder) call(s *Store, m reflect.Method) (said string, fell bool) {
	defer func() {
		if recover() != nil {
			fell = true
		}
	}()
	args := []reflect.Value{reflect.ValueOf(s)}
	for i := 1; i < m.Type.NumIn(); i++ {
		arg := in.value(m.Type.In(i), "", true)
		if i == 1 && m.Type.In(i).Kind() == reflect.Int64 {
			arg = reflect.ValueOf(in.org.ID)
		}
		args = append(args, arg)
	}
	for _, out := range m.Func.Call(args) {
		if _, isErr := out.Interface().(error); isErr {
			continue
		}
		if text, err := json.Marshal(out.Interface()); err == nil {
			said += string(text)
		} else {
			said += fmt.Sprintf("%+v", out.Interface())
		}
	}
	return said, false
}

// everyWay calls a method as org with each way of filling its text arguments from
// tryWords and its yes-or-noes, until do says to stop.
func everyWay(s *Store, undo bool, m reflect.Method, org *Org, do func(said string, fell bool) (stop bool)) {
	own, nested := 0, false
	for j := 1; j < m.Type.NumIn(); j++ {
		switch k := m.Type.In(j); {
		case k.Kind() == reflect.String:
			own++
		case k.Kind() == reflect.Struct || k.Kind() == reflect.Pointer && k != reflect.TypeOf(&Org{}):
			nested = true
		}
	}
	// The first two of a method's own are what it looks things up by; any after are who
	// did it or a second filter, left empty to keep the number of calls down.
	spoken := make([]string, own)
	var fill func(i int, word string) bool
	fill = func(i int, word string) bool {
		if i < min(own, 2) {
			for _, w := range tryWords {
				spoken[i] = w
				if fill(i+1, word) {
					return true
				}
			}
			return false
		}
		for _, yes := range []bool{true, false} {
			in := &intruder{org: org, word: word, spoken: slices.Clone(spoken), yes: yes}
			on := s
			if undo {
				on = s.through(s.DB.Begin())
			}
			said, fell := in.call(on, m)
			if undo {
				on.DB.Rollback()
			}
			if do(said, fell) {
				return true
			}
			if !in.sawBool {
				break
			}
		}
		return false
	}
	for _, word := range tryWords {
		// The word only changes anything for a method that takes a struct of text.
		if fill(0, word) || !nested {
			return
		}
	}
}

func orgTables(t *testing.T, s *Store) (tables []string) {
	t.Helper()
	for _, m := range models {
		if sch := tableOf(t, s, m); sch.LookUpField("org_id") != nil {
			tables = append(tables, sch.Table)
		}
	}
	return tables
}

// holdings is every row an organisation has, table by table.
func holdings(t *testing.T, s *Store, org int64) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, m := range models {
		sch := tableOf(t, s, m)
		if sch.LookUpField("org_id") == nil {
			continue
		}
		var rows []map[string]any
		if err := s.DB.Table(sch.Table).Where("org_id = ?", org).Order(sch.PrioritizedPrimaryField.DBName).Find(&rows).Error; err != nil {
			t.Fatal(err)
		}
		if len(rows) > 0 {
			text, _ := json.Marshal(rows)
			out[sch.Table] = string(text)
		}
	}
	return out
}

// seedFirst gives the first organisation, in an empty database, a row in every table
// an organisation has, each with id 1 where the table numbers its rows.
func seedFirst(t *testing.T, s *Store) *Org {
	t.Helper()
	first := mustOrg(t, s, "alpha")
	owner := must(s.CreateUser("owner@alpha.example", "Owner One", "a long enough password"))
	scout := Actor{Name: "agent:" + theirs, KeyID: 1}
	steps := []func() error{
		func() error { return s.AddMember(first.ID, owner.ID, "owner", "") },
		func() error { return s.SaveVoice(first.ID, owner.ID, theirs+" caller", theirs+" signature") },
		func() error {
			_, _, err := s.CreateKey(first.ID, theirs+" key", []string{"read"}, "Owner One")
			return err
		},
		func() error {
			_, err := s.AgentAddCompany(first.ID, scout, AddCompanyIn{Name: theirs + " " + findable, Domain: theirDomain, Segment: theirs, Why: theirs,
				Facts: []FactIn{{Field: "press", Value: theirs + " won", SourceURL: "https://" + theirDomain + "/news"}}})
			return err
		},
		func() error {
			_, err := s.AgentAddContact(first, scout, AddContactIn{Company: "1", Name: theirs + " " + findable, Title: "Head of Digital",
				Email: theirEmail, SourceURL: "https://" + theirDomain + "/team"})
			return err
		},
		func() error { return s.PickContact(first.ID, first, 1, "Owner One") },
		func() error {
			_, err := s.AgentAddContact(first, scout, AddContactIn{Company: "1", Name: theirs + " " + findable, Phone: "+44 20 7946 0000",
				SourceURL: "https://" + theirDomain + "/team"})
			return err
		},
		func() error {
			// Someone still due, so the queues have one of theirs in them.
			_, err := s.QuickAddPerson(first, QuickAdd{Name: theirs + " Due", Company: theirs + " " + findable, Note: theirs + " hello"}, "Owner One")
			return err
		},
		func() error { return s.Mark(first.ID, 1, "replied", "2020-01-02", "Owner One") },
		func() error {
			// Made directly: an owner has to be a member's name, which wouldn't carry the mark.
			return s.DB.Create(&Deal{OrgID: first.ID, ContactID: 1, Kind: DealNew, Owner: theirs + " owner"}).Error
		},
		func() error {
			return s.DB.Create(&Deal{OrgID: first.ID, ContactID: 1, Seq: 1, Kind: "renewal", Stage: "replied", Owner: theirs + " owner"}).Error
		},
		func() error {
			_, err := s.AddSubscription(first.ID, SubscriptionIn{Company: 1, Name: theirs + " plan", Amount: "100", Start: "2020-01-02", TermMonths: 12}, "Owner One")
			return err
		},
		func() error {
			first.Settings.OnboardingSteps = "them: " + theirs + " step, 3 days"
			if err := s.SaveOrg(first); err != nil {
				return err
			}
			_, err := s.StartOnboarding(first.ID, OnboardingIn{Company: 1, Name: theirs + " site", Start: "2020-01-02"}, "Owner One")
			return err
		},
		func() error {
			// A real draft with a real document, so the calls on agreements made as another
			// organisation are refused for being theirs, not for not being drafts.
			a, err := s.AddAgreement(first.ID, 1, theirs+" agreement", "Owner One")
			if err != nil {
				return err
			}
			if _, err = s.PutAgreementFile(first.ID, a.ID, theirs+".pdf", testPDF(1, theirs), "Owner One"); err != nil {
				return err
			}
			// The picked contact, so who signs carries the mark.
			contact := int64(1)
			r, err := s.AddRecipient(first, a.ID, RecipientSigner, &contact, nil, "Owner One")
			if err != nil {
				return err
			}
			if _, err = s.PutAgreementFields(first.ID, a.ID, []AgreementField{{RecipientID: r.ID, Page: 1, Kind: "signature", X: 0.1, Y: 0.1, W: 0.3, H: 0.05}}); err != nil {
				return err
			}
			if _, _, err := s.SealKey(first); err != nil {
				return err
			}
			return s.DB.Transaction(func(tx *gorm.DB) error {
				_, err := recordAgreementEvent(tx, AgreementEvent{OrgID: first.ID, AgreementID: a.ID, Round: 1, Kind: EventSent, Actor: "Owner One", Detail: theirs + " sent", IP: "203.0.113.9"})
				return err
			})
		},
		func() error {
			_, err := s.CreateTask(first.ID, theirs+" task", theirs+" brief", "Owner One")
			return err
		},
		func() error {
			_, err := s.AgentNote(first.ID, scout, NoteIn{Task: 1, Company: "1", Kind: "skipped", Text: theirs + " note"})
			return err
		},
		func() error {
			_, err := s.AddEnquiry(first.ID, "form", EnquiryIn{Name: theirs, Email: theirEmail, Message: theirs + " asks"})
			return err
		},
		func() error {
			// One still waiting in Research, which the first is no longer.
			_, err := s.AgentAddCompany(first.ID, scout, AddCompanyIn{Name: theirs + " waiting", Why: theirs})
			return err
		},
	}
	for i, step := range steps {
		if err := step(); err != nil {
			t.Fatalf("setting the first organisation up, step %d: %v", i, err)
		}
	}
	// Whatever table the steps above left empty gets a row made directly, its text marked.
	have := holdings(t, s, first.ID)
	for _, m := range models {
		sch := tableOf(t, s, m)
		if sch.LookUpField("org_id") == nil || have[sch.Table] != "" {
			continue
		}
		row := reflect.New(sch.ModelType)
		for _, f := range sch.Fields {
			var v any
			switch f.FieldType.Kind() {
			case reflect.String:
				v = theirs + " " + f.DBName
			case reflect.Int64, reflect.Int:
				v = int64(1)
				if strings.Contains(f.DBName, "until") || strings.Contains(f.DBName, "expires") {
					v = time.Now().Add(time.Hour).UnixMilli()
				}
			default:
				continue
			}
			if err := f.Set(s.DB.Statement.Context, row, v); err != nil {
				t.Fatal(err)
			}
		}
		if err := s.DB.Create(row.Interface()).Error; err != nil {
			t.Fatalf("a row for the first organisation in %s: %v", sch.Table, err)
		}
	}
	return first
}

// Called as the organisation that owns them, these return nothing of its rows for the
// test to see: they change something and answer with a yes, a count, a number or only
// what they were given.
var saysNothingBack = map[string]bool{
	"AddMember": true, "RemoveMember": true, "SetRole": true, "SetAreas": true, "SaveVoice": true, "SaveOrg": true, "ChangeSettings": true, "RevokeKey": true, "SetKeyScopes": true, "DeleteOrgMail": true,
	// A webhook secret is new random words, and the others say yes or no of the caller's own.
	"NewBounceHook": true, "DropBounceHook": true, "HasBounceHook": true,
	"Mark": true, "MarkAs": true, "Claim": true, "Withheld": true, "Update": true,
	// Needs a task read for it; TestAnotherOrganisationCantClaimThroughATaskItWasHanded tries it.
	"ClaimNext": true,
	// A key and certificate of its own, made on first use; seal_test.go checks another organisation gets its own.
	"SealKey": true, "SealDocument": true,

	// Only listed with suggestions waiting; TestTheDetailsCountIsTheDetailsList tries both.
	"DetailsToCheck": true, "DetailsCount": true,
	"SetCompanyStage": true, "PickContact": true, "PickContactFor": true, "ToReview": true, "DecideSuggestion": true,
	"AgentDraft": true, "AgentMark": true, "AgentFinish": true, "AgentAddFact": true, "AgentAddContact": true,
	"AskTask": true, "SetRequestDone": true, "SetTaskClosed": true, "DeleteNote": true, "DeleteEnquiry": true,
	"StartDeal": true, "ChangeDeal": true, "RemoveDeal": true, "JudgeCompany": true, "RemoveOffering": true,
	// AddOffering and ChangeOffering answer with the offering the call gave them.
	"AddOffering": true, "ChangeOffering": true,
	// AddRole and AgentAddRole answer with the role the call gave them, or an id.
	"AddRole": true, "AgentAddRole": true,
	"AddSubscription": true, "ChangeSubscription": true, "ChangeSubscriptionFields": true, "RemoveSubscription": true, "StartRenewal": true, "SetEnquiry": true, "ConverseEnquiry": true, "SetEnquiryFields": true, "ChangeDealFields": true, "SetDealFields": true, "HearEnquiry": true,
	"StartOnboarding": true, "SetOnboardingStep": true, "RemoveOnboarding": true, "InviteCustomer": true, "RevokeCustomer": true, "Suppress": true, "Unsuppress": true, "AddCampaign": true, "Audience": true, "RemoveCampaign": true, "ResumeCampaign": true, "AddAgreement": true, "PutAgreementFile": true, "RemoveAgreement": true,
	// Recipients and fields come back as ids, places and kinds; who they are is AgreementRecipients'.
	"AddRecipient": true, "ChangeRecipient": true, "TrySeal": true, "RemoveRecipient": true, "PutAgreementFields": true, "AgreementFields": true, "Readiness": true,
	"ErasePerson": true, "Import": true, "ImportLegacy": true, "Restore": true,
	"ResearchCount": true, "ResearchCounts": true,
	// A cursor of 1 is a place before every event; AgentEvents reads the same rows from the start.
	"AgentEventsBefore": true, "EnquiryCounts": true, "Week": true, "AgentMarks": true, "Touches": true,
	"AddEnquiry": true, "AgentNote": true, "CreateKey": true, "CreateTask": true, "CreateTaskFor": true, "CreateTaskWanting": true, "QuickAddPerson": true,
	// FindOne answers only when one person matches, and Find makes its query. NeverContactCovers reads the organisation it is handed.
	"FindOne": true, "NeverContactCovers": true,
	// A vendor saved or removed, and a count, carry nothing of the organisation's data.
	"SaveEnricher": true, "DeleteEnricher": true, "EnricherUsed": true, "CompanyCount": true, "TaskCounts": true,
	"ChangeContactFields": true, "KeepFind": true, "LogEnrich": true, "Erased": true, "FreshCustomerLink": true,
	"AddRun": true, "RequiresSSO": true, "AddSSODomain": true, "UnsubscribeLink": true, "MailGate": true, "RenameKey": true, "DeleteAgent": true,
	// These look for a run, a mail or a domain in a state the rows made above aren't in,
	// or one held by a server they name: TestRunsAndMailsStayInTheirOrganisation tries them.
	"SSODomain": true, "VerifySSODomain": true, "RemoveSSODomain": true, "AgentNameTaken": true, "CompanyNotes": true, "TaskOpen": true, "RunsOn": true, "AgentRunsGoing": true, "AskRunStop": true,
	"RenewRun": true, "HoldRun": true, "EndRun": true, "StopLapsedRun": true, "TakeOverRun": true,
	"DropKeyClaims": true, "KeyRevoked": true, "RequestDone": true,
	"MailSentSince": true, "NextQueued": true, "SkipMail": true, "ClaimMail": true, "SettleMail": true,
	"CampaignWent": true, "CampaignFailed": true, "PauseCampaign": true, "FinishIfDone": true,
	"AgentNamed": true, "SaveAgentRow": true, "OnTask": true, "TaskBriefsFor": true, "HistoryCount": true, "CustomerCompanies": true,
	"AroundEnquiries": true, "ResearchList": true, "StartReply": true, "SettleReply": true, "MailFailed": true, "CustomerSetStep": true, "RedeemCustomerLink": true,
	// Each works from what it is handed: settings, rows already read, a vendor, or a page
	// filter the fuzzing's words don't make.
	"Allowed": true, "WithVerdicts": true, "Spend": true, "AskedLately": true, "ResearchCountsFor": true,
}

func TestNoMethodReachesIntoAnotherOrganisation(t *testing.T) {
	t.Parallel()
	store := reflect.TypeOf(&Store{})
	var methods []reflect.Method
	for i := range store.NumMethod() {
		m := store.Method(i)
		// Not queries of their own but what the store's code writes them with (withinTheStore).
		if acrossOrganisations[m.Name] || withinTheStore[m.Name] {
			continue
		}
		if m.Type.NumIn() < 2 || (m.Type.In(1).Kind() != reflect.Int64 && m.Type.In(1) != reflect.TypeOf(&Org{})) {
			t.Errorf("Store.%s doesn't take its organisation first: give it one, or name it in acrossOrganisations if it isn't about one", m.Name)
			continue
		}
		methods = append(methods, m)
	}
	for i := range isolationShares {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			t.Parallel()
			tryMethodsAsAnotherOrganisation(t, share(methods, i))
		})
	}
}

// tryMethodsAsAnotherOrganisation tries the methods on a database of its own.
func tryMethodsAsAnotherOrganisation(t *testing.T, methods []reflect.Method) {
	s := tempStore(t)
	first := seedFirst(t, s)
	before := holdings(t, s, first.ID)
	for _, table := range orgTables(t, s) {
		if before[table] == "" {
			t.Fatalf("the first organisation has nothing in %s, so nothing there could be reached", table)
		}
	}
	second := mustOrg(t, s, "beta")

	for _, m := range methods {
		calls, falls := 0, 0
		everyWay(s, false, m, second, func(said string, fell bool) bool {
			calls++
			if fell {
				falls++
			}
			if strings.Contains(said, theirs) {
				t.Errorf("Store.%s, called for another organisation, returned the first one's: %.300s", m.Name, said)
				return true
			}
			return false
		})
		if falls == calls {
			t.Errorf("Store.%s fell over on every one of %d calls, so nothing was learnt about it", m.Name, calls)
		}
		after := holdings(t, s, first.ID)
		for table, was := range before {
			if now := after[table]; now != was {
				t.Fatalf("Store.%s, called for another organisation, changed the first one's %s:\nbefore %s\nafter  %s", m.Name, table, was, now)
			}
		}
	}

	// The same calls made as the first organisation do return its rows, so nothing above
	// passed for want of getting anywhere. Each is undone before the next.
	for _, m := range methods {
		if saysNothingBack[m.Name] {
			continue
		}
		reached := false
		everyWay(s, true, m, first, func(said string, _ bool) bool {
			reached = strings.Contains(said, theirs)
			return reached
		})
		if !reached {
			t.Errorf("Store.%s, called as the first organisation, returned nothing of its own, so the calls made as another proved nothing: "+
				"give the test's words or rows what it needs, or name it in saysNothingBack", m.Name)
		}
	}
}
