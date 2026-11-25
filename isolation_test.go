package main

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
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
	// These take a row that already names its organisation, which their callers read with it.
	"SaveOrgMail": true, "SaveSSO": true,
}

// theirs marks everything the first organisation holds, so it shows if it turns up elsewhere.
const theirs = "zzalpha"

// words are tried in every text argument: an id, and the names a method checks before
// it looks anything up, so that some call gets as far as the look-up.
var tryWords = []string{"1", "phone", "replied", "accepted", "note", "skipped", "qualified", "2020-01-02", "linkedin"}

type intruder struct {
	org  *Org
	word string
	// spoken are the words for a method's own text arguments, in order.
	spoken []string
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
		v.SetBool(true)
	case reflect.String:
		word := in.word
		if own {
			word, in.spoken = in.spoken[0], in.spoken[1:]
		}
		switch {
		case strings.Contains(name, "URL") || name == "Linkedin":
			word = "https://intruder.example/" + word
		case name == "Email":
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

func (in *intruder) call(s *Store, m reflect.Method) (said string) {
	defer func() {
		// A method given nonsense may fall over; what matters is what it touched first.
		recover()
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
	return said
}

// each runs do for every way of filling a method's own text arguments from tryWords.
func each(n int, do func(spoken []string)) {
	spoken := make([]string, n)
	var fill func(i int)
	fill = func(i int) {
		if i == n {
			do(append([]string(nil), spoken...))
			return
		}
		for _, w := range tryWords {
			spoken[i] = w
			fill(i + 1)
		}
	}
	fill(0)
}

// holdings is every row an organisation has, table by table.
func holdings(t *testing.T, s *Store, org int64) string {
	t.Helper()
	var out strings.Builder
	for _, m := range models {
		sch := tableOf(t, s, m)
		if sch.LookUpField("org_id") == nil {
			continue
		}
		var rows []map[string]any
		if err := s.db.Table(sch.Table).Where("org_id = ?", org).Order(sch.PrioritizedPrimaryField.DBName).Find(&rows).Error; err != nil {
			t.Fatal(err)
		}
		text, _ := json.Marshal(rows)
		fmt.Fprintf(&out, "%s %s\n", sch.Table, text)
	}
	return out.String()
}

func TestNoMethodReachesIntoAnotherOrganisation(t *testing.T) {
	s := tempStore(t)
	first := mustOrg(t, s, "alpha")
	// Made first, in an empty database, so each of its rows has id 1 in its table.
	owner := must(s.CreateUser("owner@alpha.example", "Owner One", "a long enough password"))
	scout := Actor{Name: "agent:scout", KeyID: 1}
	steps := []func() error{
		func() error { return s.AddMember(first.ID, owner.ID, "owner", "") },
		func() error {
			_, _, err := s.CreateKey(first.ID, theirs+" key", []string{"read"}, "Owner One")
			return err
		},
		func() error {
			_, err := s.AgentAddCompany(first.ID, scout, AddCompanyIn{Name: theirs + " Yachts", Domain: theirs + ".example", Why: theirs,
				Facts: []FactIn{{Field: "press", Value: theirs + " won", SourceURL: "https://" + theirs + ".example/news"}}})
			return err
		},
		func() error {
			_, err := s.AgentAddContact(first, scout, AddContactIn{Company: "1", Name: theirs + " Vale", Title: "Head of Digital",
				Email: "vale@" + theirs + ".example", SourceURL: "https://" + theirs + ".example/team"})
			return err
		},
		func() error { return s.PickContact(first.ID, first, 1, "Owner One") },
		func() error {
			_, err := s.AgentAddContact(first, scout, AddContactIn{Company: "1", Name: theirs + " Vale", Phone: "+44 20 7946 0000",
				SourceURL: "https://" + theirs + ".example/team"})
			return err
		},
		func() error { return s.Mark(first.ID, 1, "replied", "2020-01-02", "Owner One") },
		func() error {
			_, err := s.CreateTask(first.ID, theirs+" task", theirs+" brief", "Owner One")
			return err
		},
		func() error {
			_, err := s.AgentNote(first.ID, scout, NoteIn{Task: 1, Company: "1", Kind: "skipped", Text: theirs + " note"})
			return err
		},
		func() error {
			_, err := s.AddEnquiry(first.ID, "form", EnquiryIn{Name: theirs, Email: "vale@" + theirs + ".example", Message: theirs + " asks"})
			return err
		},
	}
	for i, step := range steps {
		if err := step(); err != nil {
			t.Fatalf("setting the first organisation up, step %d: %v", i, err)
		}
	}
	before := holdings(t, s, first.ID)
	for _, table := range []string{"companies", "facts", "contacts", "suggestions", "tasks", "task_requests", "task_notes", "enquiries", "events", "api_keys", "memberships"} {
		if !strings.Contains(before, table+` [{`) {
			t.Fatalf("the first organisation has nothing in %s, so nothing there could be reached", table)
		}
	}

	second := mustOrg(t, s, "beta")
	store := reflect.TypeOf(s)
	tried, leaked := 0, map[string]bool{}
	for i := range store.NumMethod() {
		m := store.Method(i)
		if acrossOrganisations[m.Name] {
			continue
		}
		if m.Type.NumIn() < 2 || (m.Type.In(1).Kind() != reflect.Int64 && m.Type.In(1) != reflect.TypeOf(&Org{})) {
			t.Errorf("Store.%s doesn't take its organisation first: give it one, or name it in acrossOrganisations if it isn't about one", m.Name)
			continue
		}
		own := 0
		for j := 1; j < m.Type.NumIn(); j++ {
			if m.Type.In(j).Kind() == reflect.String {
				own++
			}
		}
		for _, word := range tryWords {
			each(own, func(spoken []string) {
				tried++
				in := &intruder{org: second, word: word, spoken: spoken}
				if said := in.call(s, m); strings.Contains(said, theirs) && !leaked[m.Name] {
					leaked[m.Name] = true
					t.Errorf("Store.%s, called for another organisation, returned the first one's: %.300s", m.Name, said)
				}
			})
			if own > 2 {
				// The method's own arguments have been through every word already.
				break
			}
		}
		if after := holdings(t, s, first.ID); after != before {
			t.Fatalf("Store.%s, called for another organisation, changed what the first one holds:\nbefore %s\nafter  %s", m.Name, before, after)
		}
	}
	if tried < 500 {
		t.Fatalf("only %d calls were tried", tried)
	}

	// The same calls made as the first organisation do reach its rows, so the test above
	// isn't passing for want of getting anywhere.
	reached := map[string]bool{}
	for i := range store.NumMethod() {
		m := store.Method(i)
		if acrossOrganisations[m.Name] || m.Type.NumIn() < 2 {
			continue
		}
		in := &intruder{org: first, word: "1", spoken: []string{"1", "1", "1", "1", "1", "1"}}
		if strings.Contains(in.call(s, m), theirs) {
			reached[m.Name] = true
		}
	}
	if len(reached) < 10 {
		t.Fatalf("called as the first organisation, only %d methods returned anything of its own: %v", len(reached), reached)
	}
}
