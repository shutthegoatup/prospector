package main

import (
	"context"
	"errors"
	"net/http"
	"time"
)

type Nav struct {
	*Req
	Active                                    string
	Today, Research, Conversations, Companies int
	// Enquiries is how many are waiting for an answer from us.
	Enquiries int
}

func (a *App) dueByStep(q *Req, now time.Time) map[string][]Contact {
	if q.due != nil {
		return q.due
	}
	out := map[string][]Contact{}
	q.due = out
	for _, s := range Steps {
		if s.Key == "next" {
			continue
		}
		out[s.Key], _ = a.s.Due(q.Org.ID, s.Key, now, q.Org.Settings)
	}
	convs, _ := a.s.Conversations(q.Org.ID)
	day := now.Format("2006-01-02")
	for _, list := range convs {
		for _, c := range list {
			if NeedsNextStep(c, day) {
				out["next"] = append(out["next"], c)
			}
		}
	}
	return out
}

func (a *App) nav(q *Req, active string) Nav {
	n := Nav{Req: q, Active: active}
	for _, list := range a.dueByStep(q, time.Now()) {
		n.Today += len(list)
	}
	n.Research = a.s.ResearchCount(q.Org.ID, "review")
	n.Enquiries = a.s.EnquiryCounts(q.Org.ID)["state:"+EnquiryOpen]
	convs, _ := a.s.Conversations(q.Org.ID)
	for k, list := range convs {
		if k != "won" && k != "lost" {
			n.Conversations += len(list)
		}
	}
	others, _ := a.s.OtherDeals(q.Org.ID)
	for k, list := range others {
		if k != "won" && k != "lost" {
			n.Conversations += len(list)
		}
	}
	var companies int64
	logged(a.s.in(q.Org.ID).Model(&Company{}).Count(&companies))
	n.Companies = int(companies)
	return n
}

// TodayData is the work Today points to on other pages, and the last seven days.
type TodayData struct {
	// Review is the companies waiting in Research.
	Review int
	// Deals are the renewals and other further deals with no next step, or one that's due.
	Deals int
	// Subs are the subscriptions with a day to cancel by, a renewal or an end coming soon.
	Subs int
	// Enquiries are the ones nobody has answered yet.
	Enquiries int
	// Steps are the onboarding steps late or due today, ours and the customers alike.
	Steps int
	Week  Stats
}

// todayElsewhere is the work Today points to on other pages, and the last seven days.
func (a *App) todayElsewhere(org int64, now time.Time, d *TodayData) error {
	d.Review, d.Week = a.s.ResearchCount(org, "review"), a.s.Week(org, now)
	others, err := a.s.OtherDeals(org)
	if err != nil {
		return err
	}
	for _, list := range others {
		for _, o := range list {
			if o.needsNextStep(now.Format("2006-01-02")) {
				d.Deals++
			}
		}
	}
	subs, err := a.s.Subscriptions(org, 0, now.Format(dayLayout))
	if err != nil {
		return err
	}
	d.Enquiries = a.s.EnquiryCounts(org)["state:"+EnquiryOpen]
	onboard, err := a.s.Onboardings(org, 0, now.Format(dayLayout))
	if err != nil {
		return err
	}
	for _, ob := range onboard {
		d.Steps += ob.Late + ob.DueToday
	}
	for _, sub := range subs {
		if sub.Soon(now.Format(dayLayout)) {
			d.Subs++
		}
	}
	return nil
}

// bookmarklet sends the LinkedIn profile you're looking at to the add form. It reads only
// the page's title and headline, on your click.
func bookmarklet(site string, q *Req, page string) string {
	base := site + q.Base() + page + "?popup=1"
	return `javascript:(()=>{const h=(document.querySelector('.text-body-medium')||{}).innerText||'';` +
		`window.open('` + base + `&url='+encodeURIComponent(location.href)+'&name='+encodeURIComponent(document.title)+'&headline='+encodeURIComponent(h),'prospect','width=520,height=760')})()`
}

func (a *App) baseURL(r *http.Request) string {
	if a.base != "" {
		return a.base
	}
	scheme := "http"
	if a.secure(r) {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

type memberIn struct {
	Email string `json:"email"`
	Name  string `json:"name"`
	Role  string `json:"role"`
}

// addMember puts someone on the team, making them an account if they have none, and
// says how they get in.
func (a *App) addMember(ctx context.Context, q *Req, in memberIn) (string, error) {
	if in.Role != "owner" && in.Role != "member" {
		in.Role = "member"
	}
	u, err := a.s.UserByEmail(in.Email)
	password := ""
	if err != nil {
		password = randomToken(12)
		u, err = a.s.InviteUser(in.Email, in.Name, password)
	}
	if err == nil {
		err = a.s.AddMember(q.Org.ID, u.ID, in.Role, u.Name)
	}
	if err != nil {
		return "", err
	}
	msg := u.Email + " added to " + q.Org.Name + "."
	// Someone who never chose a password is sent the link again, so a lost one isn't the end.
	if password != "" || u.MustChangePassword {
		sent, err := a.invite(ctx, q, u, password != "")
		if sent {
			return msg + " They've been emailed a link to choose their password; it lasts 7 days.", nil
		}
		if errors.Is(err, errTooSoon) {
			msg += " They were emailed a link to choose their password a moment ago, so no other was sent."
		} else if err != nil {
			msg += " The invitation couldn't be emailed (" + err.Error() + ")."
		}
	} else if !u.HasPassword() {
		msg += " They have no password: they sign in through a provider, so they can open this organisation only by one it accepts."
	}
	if password != "" {
		msg += " Their first password, shown once: " + password + " (they choose their own when they first sign in)"
	}
	return msg, nil
}
