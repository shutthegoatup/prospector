package app

import (
	"testing"

	"github.com/shutthegoatup/prospector/internal/mailer"
)

// TestMailSettingsThatCantBeReadAreNotNone: an organisation with no mail server of its
// own gets the site's, but one whose settings can't be read gets an error, not the site's
// server or none.
func TestMailSettingsThatCantBeReadAreNotNone(t *testing.T) {
	t.Parallel()
	s := ownStore(t)
	o := mustOrg(t, s, "alpha")
	site := &mailer.Server{Host: "smtp.site.example", Port: 587}
	a := &App{s: s, mail: site}
	if via, err := a.MailFor(o.ID); err != nil || via != site {
		t.Fatalf("with none of its own: %v %v", via, err)
	}
	if via, err := a.campaignMail(o.ID); err != nil || via != nil {
		t.Fatalf("campaigns with none of its own: %v %v", via, err)
	}
	if err := s.DB.Exec("drop table org_mails").Error; err != nil {
		t.Fatal(err)
	}
	if via, err := a.MailFor(o.ID); err == nil {
		t.Fatalf("settings that can't be read were taken as none: %v", via)
	}
	if via, err := a.campaignMail(o.ID); err == nil {
		t.Fatalf("campaigns: settings that can't be read were taken as none: %v", via)
	}
}
