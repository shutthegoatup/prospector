package main

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Phase is where a contact stands. It is never stored: the dated marks on a contact
// are facts that can arrive in any order, and the phase is read from them.
type Phase string

const (
	PhaseSuggested    Phase = "suggested"
	PhaseDeclined     Phase = "declined"
	PhaseConversation Phase = "conversation"
	PhaseHold         Phase = "hold"
	PhaseOutreach     Phase = "in outreach"
	PhaseNew          Phase = "not contacted"
)

// Phase picks the one that wins when several marks are set. openContacts and
// Conversations say the same thing in SQL; TestQueriesAgreeWithPhase holds them together.
func (c Contact) Phase() Phase {
	switch {
	case c.Status == ContactSuggested:
		return PhaseSuggested
	case c.Declined != nil:
		return PhaseDeclined
	case c.Replied != nil:
		return PhaseConversation
	case c.Hold != nil:
		return PhaseHold
	case c.Touched():
		return PhaseOutreach
	}
	return PhaseNew
}

func (c Contact) Touched() bool {
	return c.EmailSent != nil || c.LinkedinAdded != nil || c.Called != nil
}

// Open is someone the outreach queues may still show.
func (c Contact) Open() bool {
	p := c.Phase()
	return (p == PhaseNew || p == PhaseOutreach) && !strings.HasPrefix(c.Key, "org:")
}

const openContacts = "hold is null and replied is null and declined is null and status <> 'suggested' and key not like 'org:%'"

// A rejected company's people leave the queues without being marked, so qualifying
// it again brings them back as they were.
const companyNotRejected = "(company_id is null or company_id not in (select id from companies where stage = '" + CompanyRejected + "'))"

const inConversation = "replied is not null and declined is null and status <> 'suggested'"

func (c Contact) ConversationStage() string {
	if c.Stage == "" {
		return Stages[0].Key
	}
	return c.Stage
}

func validStage(stage string) bool {
	for _, s := range Stages {
		if s.Key == stage {
			return true
		}
	}
	return false
}

// A conversation may move between any two stages: deals reopen and get corrected.
func (c Contact) canEnterStage(stage string) error {
	if !validStage(stage) {
		return fmt.Errorf("unknown stage %q", stage)
	}
	if c.Replied == nil {
		return errors.New("a stage is for someone who has replied; mark the reply first")
	}
	return nil
}

func (c Contact) canMark() error {
	if c.Status == ContactSuggested {
		return errors.New("this contact is still a suggestion in Research; a person picks them first")
	}
	return nil
}

// markDate compares days as text, in the zone today() uses: parsing the day as UTC
// midnight would call today "the future" for the first hours of it east of UTC.
func markDate(date string, now time.Time) error {
	if _, err := time.Parse("2006-01-02", date); err != nil {
		return errors.New("date is YYYY-MM-DD")
	}
	if date > now.Format("2006-01-02") {
		return errors.New("date is in the future")
	}
	return nil
}
