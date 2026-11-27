package store

import (
	"errors"
	"fmt"
	"strings"
)

// Phase is where a contact stands. It is never stored: the dated marks on a contact
// are facts that can arrive in any order, and the phase is read from them.
type Phase string

const PhaseSuggested Phase = "suggested"

const PhaseDeclined Phase = "declined"

const PhaseConversation Phase = "conversation"

const PhaseHold Phase = "hold"

const PhaseOutreach Phase = "in outreach"

const PhaseNew Phase = "not contacted"

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

// Open is someone the outreach queues may still show, unless their company is rejected.
func (c Contact) Open() bool {
	p := c.Phase()
	return (p == PhaseNew || p == PhaseOutreach) && !strings.HasPrefix(c.Key, "org:")
}

func (c Contact) ConversationStage() string {
	if c.Stage == "" {
		return Stages[0].Key
	}
	return c.Stage
}

func ValidStage(stage string) bool {
	for _, s := range Stages {
		if s.Key == stage {
			return true
		}
	}
	return false
}

// A conversation may move between any two stages: deals reopen and get corrected.
func (c Contact) CanEnterStage(stage string) error {
	if !ValidStage(stage) {
		return fmt.Errorf("unknown stage %q", stage)
	}
	if c.Replied == nil {
		return errors.New("a stage is for someone who has replied; mark the reply first")
	}
	return nil
}

func (c Contact) CanMark() error {
	if c.Status == ContactSuggested {
		return errors.New("this contact is still a suggestion in Research; a person picks them first")
	}
	return nil
}
