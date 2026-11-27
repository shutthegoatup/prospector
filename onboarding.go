package main

// OnboardingIn is what someone gives to start one.
type OnboardingIn struct {
	Company int64  `json:"company"`
	Name    string `json:"name"`
	Start   string `json:"start"`
}

// OnboardingRow is an onboarding with its company's name, its steps and how far it has got.
type OnboardingRow struct {
	Onboarding
	Company string
	Steps   []OnboardingStep
	Done    int
	// Late are the steps due before day and not done; DueToday those due on it.
	Late, DueToday int
	// Next is the first step not done, nil once all are.
	Next *OnboardingStep
	// LiveOn is the day the last step was done, once every step is.
	LiveOn string
	// Invites are the people let see it, Invitable those at the company who could be, and
	// MailOn whether a link can be sent at all. The pages fill these in; see customerControls.
	Invites   []CustomerInvite
	Invitable []Contact
	MailOn    bool
}

func (r OnboardingRow) Live() bool { return r.LiveOn != "" }
