package main

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/shutthegoatup/prospector/api"
	"github.com/shutthegoatup/prospector/internal/say"
)

func settingsOut(o *Org) api.Settings {
	set := o.Settings.WithDefaults()
	return api.Settings{Name: o.Name, Color: o.Color, EmailAfterDays: int32(set.EmailAfterDays), FollowupAfterDays: int32(set.FollowupAfterDays),
		NoteTemplate: set.NoteTemplate, Pitch: set.Pitch, NeverContact: set.NeverContact, LookFor: set.LookFor, AgentGuidance: set.AgentGuidance,
		EnquirySites: set.EnquirySites, OnboardingSteps: set.OnboardingSteps, Currency: set.Currency, PostalAddress: set.PostalAddress,
		IncorporatedOnly: set.IncorporatedOnly, LookingFor: lookingFor(o.Settings)}
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
		return nil, say.New("send what changes, such as {\"enquiry_sites\": \"https://www.example.com\"}")
	}
	q := reqFrom(ctx)
	// Read afresh, so what this leaves out is what is saved now rather than what the
	// request's copy held when it arrived.
	o, err := s.a.s.OrgBySlug(q.Org.Slug)
	if err != nil {
		return nil, err
	}
	before := settingsOut(o)
	c, set := in.Body, &o.Settings
	put(&o.Name, c.Name)
	put(&o.Color, c.Color)
	putInt(&set.EmailAfterDays, c.EmailAfterDays)
	putInt(&set.FollowupAfterDays, c.FollowupAfterDays)
	put(&set.NoteTemplate, c.NoteTemplate)
	put(&set.Pitch, c.Pitch)
	put(&set.NeverContact, c.NeverContact)
	put(&set.LookFor, c.LookFor)
	put(&set.AgentGuidance, c.AgentGuidance)
	put(&set.EnquirySites, c.EnquirySites)
	put(&set.OnboardingSteps, c.OnboardingSteps)
	put(&set.Currency, c.Currency)
	put(&set.PostalAddress, c.PostalAddress)
	if c.IncorporatedOnly != nil {
		set.IncorporatedOnly = *c.IncorporatedOnly
	}
	if err := checkOrg(o); err != nil {
		return nil, err
	}
	// Compared as they take effect, so the page sending back the defaults it showed, or
	// "gbp" for GBP, isn't a change.
	changed := settingsDiff(before, settingsOut(o))
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

func put(to, v *string) {
	if v != nil {
		*to = *v
	}
}

func putInt(to *int, v *int32) {
	if v != nil {
		*to = int(*v)
	}
}

// settingsDiff names the fields, as the API spells them, that differ between a and b.
func settingsDiff(a, b api.Settings) []string {
	var was, now map[string]any
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	_ = json.Unmarshal(ja, &was)
	_ = json.Unmarshal(jb, &now)
	var changed []string
	for k, v := range now {
		if !reflect.DeepEqual(was[k], v) {
			changed = append(changed, k)
		}
	}
	slices.Sort(changed)
	return changed
}

// checkOrg refuses settings that can't be worked with, and tidies the name and currency.
func checkOrg(o *Org) error {
	set := &o.Settings
	if o.Name = strings.TrimSpace(o.Name); o.Name == "" {
		return say.New("the organisation needs a name")
	}
	if set.EmailAfterDays < 0 || set.FollowupAfterDays < 0 || set.EmailAfterDays > 60 || set.FollowupAfterDays > 60 {
		return say.New("waits are between 0 and 60 days")
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
		return say.New("a postal address is 300 characters at most")
	}
	if set.Currency = strings.ToUpper(strings.TrimSpace(set.Currency)); set.Currency != "" && !currencyRe.MatchString(set.Currency) {
		return say.New("a currency is three letters, such as GBP, EUR or USD")
	}
	return nil
}
