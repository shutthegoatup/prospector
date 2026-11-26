package main

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/shutthegoatup/prospector/api"
)

func settingsOut(o *Org) api.Settings {
	set := o.Settings.withDefaults()
	return api.Settings{Name: o.Name, Color: o.Color, EmailAfterDays: int32(set.EmailAfterDays), FollowupAfterDays: int32(set.FollowupAfterDays),
		NoteTemplate: set.NoteTemplate, Pitch: set.Pitch, NeverContact: set.NeverContact, LookFor: set.LookFor, AgentGuidance: set.AgentGuidance,
		EnquirySites: set.EnquirySites, OnboardingSteps: set.OnboardingSteps, Currency: set.Currency, PostalAddress: set.PostalAddress,
		IncorporatedOnly: set.IncorporatedOnly}
}

func (s apiServer) GetSettings(ctx context.Context, _ api.GetSettingsRequestObject) (api.GetSettingsResponseObject, error) {
	o, err := s.a.s.OrgBySlug(reqFrom(ctx).Org.Slug)
	if err != nil {
		return nil, err
	}
	return api.GetSettings200JSONResponse(settingsOut(o)), nil
}

func (s apiServer) ChangeSettings(ctx context.Context, in api.ChangeSettingsRequestObject) (api.ChangeSettingsResponseObject, error) {
	if in.Body == nil {
		return nil, errors.New("send what changes, such as {\"enquiry_sites\": \"https://www.example.com\"}")
	}
	q := reqFrom(ctx)
	// Read afresh, so what this leaves out is what is saved now rather than what the
	// request's copy held when it arrived.
	o, err := s.a.s.OrgBySlug(q.Org.Slug)
	if err != nil {
		return nil, err
	}
	c, set := in.Body, &o.Settings
	var changed []string
	put := func(name string, to *string, v *string) {
		if v != nil && *v != *to {
			*to, changed = *v, append(changed, name)
		}
	}
	putInt := func(name string, to *int, v *int32) {
		if v != nil && int(*v) != *to {
			*to, changed = int(*v), append(changed, name)
		}
	}
	put("name", &o.Name, c.Name)
	put("color", &o.Color, c.Color)
	putInt("email_after_days", &set.EmailAfterDays, c.EmailAfterDays)
	putInt("followup_after_days", &set.FollowupAfterDays, c.FollowupAfterDays)
	put("note_template", &set.NoteTemplate, c.NoteTemplate)
	put("pitch", &set.Pitch, c.Pitch)
	put("never_contact", &set.NeverContact, c.NeverContact)
	put("look_for", &set.LookFor, c.LookFor)
	put("agent_guidance", &set.AgentGuidance, c.AgentGuidance)
	put("enquiry_sites", &set.EnquirySites, c.EnquirySites)
	put("onboarding_steps", &set.OnboardingSteps, c.OnboardingSteps)
	put("currency", &set.Currency, c.Currency)
	put("postal_address", &set.PostalAddress, c.PostalAddress)
	if c.IncorporatedOnly != nil && *c.IncorporatedOnly != set.IncorporatedOnly {
		set.IncorporatedOnly, changed = *c.IncorporatedOnly, append(changed, "incorporated_only")
	}
	if err := checkOrg(o); err != nil {
		return nil, err
	}
	who := Actor{Name: q.Actor()}
	if q.Key != nil {
		who.KeyID = q.Key.ID
	}
	if err := s.a.s.ChangeSettings(o, who, changed); err != nil {
		return nil, err
	}
	if len(changed) > 0 {
		s.a.dropMCPServers()
	}
	return api.ChangeSettings200JSONResponse(settingsOut(o)), nil
}

// checkOrg refuses settings that can't be worked with, and tidies the name and currency.
func checkOrg(o *Org) error {
	set := &o.Settings
	if o.Name = strings.TrimSpace(o.Name); o.Name == "" {
		return errors.New("the organisation needs a name")
	}
	if set.EmailAfterDays < 0 || set.FollowupAfterDays < 0 || set.EmailAfterDays > 60 || set.FollowupAfterDays > 60 {
		return errors.New("waits are between 0 and 60 days")
	}
	if _, err := enquiryOrigins(set.EnquirySites); err != nil {
		return err
	}
	if _, err := set.Assets(); err != nil {
		return err
	}
	if _, err := set.Onboarding(); err != nil {
		return err
	}
	if utf8.RuneCountInString(set.PostalAddress) > 300 {
		return errors.New("a postal address is 300 characters at most")
	}
	if set.Currency = strings.ToUpper(strings.TrimSpace(set.Currency)); set.Currency != "" && !currencyRe.MatchString(set.Currency) {
		return errors.New("a currency is three letters, such as GBP, EUR or USD")
	}
	return nil
}
