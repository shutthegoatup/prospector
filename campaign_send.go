package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// CampaignRecipient is one mail of a campaign: whom it is to, and how it went. The rows
// are made when the campaign is sent, so who it was for is fixed then, and each is
// claimed just before its mail goes and never sent again: a server stopped mid-send
// leaves at most that one mail uncertain, and never mails anyone twice.
type CampaignRecipient struct {
	ID         int64  `gorm:"primaryKey" json:"id"`
	OrgID      int64  `gorm:"index;not null" json:"-"`
	CampaignID int64  `gorm:"uniqueIndex:idx_campaign_address,priority:1;index:idx_campaign_status,priority:1;not null" json:"campaign_id"`
	Address    string `gorm:"uniqueIndex:idx_campaign_address,priority:2;not null" json:"address"`
	// ContactID is who it was for, until they are erased or a restore drops them.
	ContactID *int64 `gorm:"index" json:"contact_id"`
	Name      string `gorm:"not null;default:''" json:"name"`
	Status    string `gorm:"index:idx_campaign_status,priority:2;not null;default:'queued'" json:"status"`
	// Reason says why one wasn't sent, or why it is unknown whether it went.
	Reason    string     `gorm:"not null;default:''" json:"reason"`
	ClaimedAt *time.Time `json:"-"`
	SentAt    *time.Time `json:"sent_at"`
}

const (
	MailQueued    = "queued"
	MailSending   = "sending"
	MailSent      = "sent"
	MailFailed    = "failed"
	MailUnknown   = "unknown"
	MailSkipped   = "skipped"
	MailCancelled = "cancelled"

	// sendEach is how many of one campaign's mails go in one round, and sendEvery how
	// often a round runs: about ninety a minute, a pace a mail server takes kindly.
	sendEach  = 30
	sendEvery = 20 * time.Second
	// claimStale is when a mail claimed and never settled is given up as unknown.
	claimStale = 10 * time.Minute
	// failuresToPause is how many sends in a row may fail to reach the mail server
	// before the campaign waits for someone to look.
	failuresToPause = 3
	// campaignDailyCap is how many campaign mails one organisation sends in a day, and
	// roundBudget how long one campaign's round may take.
	campaignDailyCap = 2000
	roundBudget      = time.Minute
	postalLimit      = 300
)

var (
	errCampaignMail       = errors.New("a campaign goes through your organisation's own mail server, so an owner sets one in Settings first; the server's own is for sign-in mail")
	errCampaignSubmission = errors.New("a campaign goes through a mail server you sign in to, with a username and password, on port 587, 465 or 2525; not straight to a recipient's server on port 25")
)

// campaignMail is the organisation's own mail server, or nil. Never the server's
// fallback: anyone can make an organisation, and the host's mail isn't theirs to send
// marketing through. It is built from the one row read here, so a server removed
// meanwhile can't turn into the host's. And it must be a submission server signed in
// to: one on port 25 with no sign-in would have this host deliver straight to anyone's
// mail exchanger, as a relay.
func (a *App) campaignMail(org int64) (*MailServer, error) {
	m, err := a.s.OrgMail(org)
	if err != nil {
		return nil, nil
	}
	if m.Username == "" || m.Port == 25 {
		return nil, errCampaignSubmission
	}
	via := &MailServer{Host: m.Host, Port: m.Port, Username: m.Username, From: m.Sender}
	if m.Password != "" {
		if via.Password, err = a.s.keys.open(m.Password); err != nil {
			return nil, fmt.Errorf("this organisation's mail password can't be read: %w", err)
		}
	}
	return via, nil
}

// sendable is why a campaign can't be sent from this server for this organisation.
func (a *App) sendable(o *Org) error {
	via, err := a.campaignMail(o.ID)
	if err != nil {
		return err
	}
	if via == nil {
		return errCampaignMail
	}
	if u, err := url.Parse(a.base); a.base == "" || err != nil || u.Scheme != "https" {
		return errors.New("a campaign's unsubscribe link needs this server's public https address (PROSPECTOR_URL)")
	}
	if strings.TrimSpace(o.Settings.PostalAddress) == "" {
		return errors.New("a campaign has to say where you can be written to: an owner sets the postal address in Settings")
	}
	return nil
}

// SendCampaign fixes who a draft goes to, as its audience stands now, and sets it
// going. Asked twice at once, it is sent once.
func (s *Store) SendCampaign(o *Org, id int64, actor string) (*Campaign, error) {
	var c Campaign
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("org_id = ? and id = ?", o.ID, id).First(&c).Error; err != nil {
			return missing(err, "no campaign %d", id)
		}
		if c.Status != CampaignDraft {
			return errors.New("it has been sent already")
		}
		if strings.TrimSpace(c.Subject) == "" || strings.TrimSpace(c.Body) == "" {
			return errors.New("a campaign needs a subject and something to say before it is sent")
		}
		now := time.Now()
		flipped := tx.Model(&Campaign{}).Where("org_id = ? and id = ? and status = ?", o.ID, id, CampaignDraft).
			Updates(map[string]any{"status": CampaignSending, "queued_by": actor, "queued_at": now, "paused": "", "failures": 0})
		if flipped.Error != nil {
			return flipped.Error
		}
		if flipped.RowsAffected == 0 {
			return errors.New("it has been sent already")
		}
		w, err := s.audienceWork(o)
		if err != nil {
			return err
		}
		people, err := s.audience(o, c, w)
		if err != nil {
			return err
		}
		var rows []CampaignRecipient
		for _, p := range people {
			if p.LeftOut != "" {
				continue
			}
			contact := p.ContactID
			rows = append(rows, CampaignRecipient{OrgID: o.ID, CampaignID: id, Address: p.Address, ContactID: &contact, Name: p.Name, Status: MailQueued})
		}
		if len(rows) == 0 {
			return errors.New("nobody is left for it to go to")
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(rows, 200).Error; err != nil {
			return err
		}
		return tx.Where("org_id = ? and id = ?", o.ID, id).First(&c).Error
	})
	return &c, err
}

// CancelCampaign stops what hasn't gone. What went stays recorded.
func (s *Store) CancelCampaign(org, id int64) (*Campaign, error) {
	var c Campaign
	err := s.db.Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&Campaign{}).Where("org_id = ? and id = ? and status in ?", org, id, []string{CampaignSending, CampaignPaused}).
			Update("status", CampaignCancelled)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			if err := tx.Where("org_id = ? and id = ?", org, id).First(&c).Error; err != nil {
				return missing(err, "no campaign %d", id)
			}
			return fmt.Errorf("a campaign that is %s can't be cancelled", c.Status)
		}
		if err := tx.Model(&CampaignRecipient{}).Where("org_id = ? and campaign_id = ? and status = ?", org, id, MailQueued).
			Update("status", MailCancelled).Error; err != nil {
			return err
		}
		return tx.Where("org_id = ? and id = ?", org, id).First(&c).Error
	})
	return &c, err
}

// ResumeCampaign sets a paused campaign going again, once someone has looked.
func (s *Store) ResumeCampaign(org, id int64) (*Campaign, error) {
	res := s.in(org).Model(&Campaign{}).Where("id = ? and status = ?", id, CampaignPaused).
		Updates(map[string]any{"status": CampaignSending, "paused": "", "failures": 0})
	if res.Error != nil {
		return nil, res.Error
	}
	c, err := s.Campaign(org, id)
	if err != nil {
		return nil, err
	}
	if res.RowsAffected == 0 {
		return nil, fmt.Errorf("only a paused campaign can be resumed, and this one is %s", c.Status)
	}
	return c, nil
}

// CampaignTally counts a campaign's mails by how they went.
func (s *Store) CampaignTally(org int64, ids []int64) (map[int64]map[string]int, error) {
	out := map[int64]map[string]int{}
	if len(ids) == 0 {
		return out, nil
	}
	var rows []struct {
		CampaignID int64
		Status     string
		N          int
	}
	if err := s.in(org).Model(&CampaignRecipient{}).Select("campaign_id, status, count(*) as n").Where("campaign_id in ?", ids).
		Group("campaign_id, status").Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		if out[r.CampaignID] == nil {
			out[r.CampaignID] = map[string]int{}
		}
		out[r.CampaignID][r.Status] = r.N
	}
	return out, nil
}

func (s *Store) CampaignRecipients(org, campaign int64) ([]CampaignRecipient, error) {
	if _, err := s.Campaign(org, campaign); err != nil {
		return nil, err
	}
	out := []CampaignRecipient{}
	return out, s.in(org).Where("campaign_id = ?", campaign).Order("id").Find(&out).Error
}

// keepSending runs a round of every campaign being sent, as long as the server runs.
func (a *App) keepSending(ctx context.Context, every time.Duration) {
	for {
		a.sendRound(ctx, time.Now())
		if a.pause(ctx, every) != nil {
			return
		}
	}
}

// sendRound gives up on mails claimed too long ago, then sends up to sendEach of each
// campaign that is going. It is safe beside another server doing the same.
func (a *App) sendRound(ctx context.Context, now time.Time) {
	stored(a.s.db.Model(&CampaignRecipient{}).Where("status = ? and claimed_at < ?", MailSending, now.Add(-claimStale)).
		Updates(map[string]any{"status": MailUnknown, "reason": "the server stopped while it was being sent"}))
	var going []Campaign
	if !stored(a.s.db.Where("status = ?", CampaignSending).Order("id").Find(&going)) {
		return
	}
	for _, c := range going {
		a.sendSome(ctx, c, now)
	}
}

// stored is whether a query went through, logging it when it didn't.
func stored(res *gorm.DB) bool { return logged(res) == nil }

func (a *App) sendSome(ctx context.Context, c Campaign, now time.Time) {
	o, err := a.s.OrgByID(c.OrgID)
	if err != nil {
		log.Printf("campaign %d: %v", c.ID, err)
		return
	}
	if err := a.sendable(o); err != nil {
		a.pauseCampaign(c, err.Error())
		return
	}
	via, err := a.campaignMail(o.ID)
	if err != nil || via == nil {
		a.pauseCampaign(c, errCampaignMail.Error())
		return
	}
	gate, err := a.s.gate(a.s.db, o)
	if err != nil {
		log.Printf("campaign %d: %v", c.ID, err)
		return
	}
	// One organisation's day, and one round's time, are bounded: a host carries many,
	// and a mail server that hangs mustn't hold up the rest.
	var today int64
	if !stored(a.s.db.Model(&CampaignRecipient{}).Where("org_id = ? and status = ? and sent_at >= ?", o.ID, MailSent, now.Add(-24*time.Hour)).Count(&today)) {
		return
	}
	started := time.Now()
	for range min(sendEach, max(0, campaignDailyCap-int(today))) {
		if ctx.Err() != nil || time.Since(started) > roundBudget {
			return
		}
		var r CampaignRecipient
		next := a.s.db.Where("org_id = ? and campaign_id = ? and status = ?", c.OrgID, c.ID, MailQueued).Order("id").Limit(1).Find(&r)
		if !stored(next) {
			return
		}
		if next.RowsAffected == 0 {
			a.finishIfDone(c)
			return
		}
		claimed := a.s.db.Model(&CampaignRecipient{}).Where("id = ? and status = ?", r.ID, MailQueued).
			Updates(map[string]any{"status": MailSending, "claimed_at": time.Now()})
		if !stored(claimed) {
			return
		}
		if claimed.RowsAffected == 0 {
			continue
		}
		if !a.sendOne(ctx, o, c, via, gate, r) {
			return
		}
	}
}

// sendOne sends one claimed mail and settles it. It reports whether the round should go
// on: not once the mail server can't be reached, nor once the campaign stops.
func (a *App) sendOne(ctx context.Context, o *Org, c Campaign, via *MailServer, gate *mailGate, r CampaignRecipient) bool {
	settle := func(status, reason string) bool {
		updates := map[string]any{"status": status, "reason": reason}
		if status == MailSent {
			updates["sent_at"] = time.Now()
		}
		if status == MailQueued {
			updates["claimed_at"] = nil
		}
		return stored(a.s.db.Model(&CampaignRecipient{}).Where("id = ?", r.ID).Updates(updates))
	}
	// Stopped since the round began: this one goes back with the rest.
	var now Campaign
	if !stored(a.s.db.Where("id = ?", c.ID).First(&now)) || now.Status != CampaignSending {
		if now.Status == CampaignCancelled {
			settle(MailCancelled, "")
		} else {
			settle(MailQueued, "")
		}
		return false
	}
	// Checked again as it goes: someone may have unsubscribed since it was sent.
	var link string
	if r.ContactID != nil {
		var who Contact
		if a.s.db.Where("org_id = ? and id = ?", o.ID, *r.ContactID).Limit(1).Find(&who).Error == nil {
			link = deref(who.Linkedin)
		}
	}
	// An address made unreadable since it was queued, as erasure does, is nobody's.
	if _, err := bareAddress(r.Address); err != nil {
		return settle(MailSkipped, "no longer one email address")
	}
	if why := gate.whyFor(c.Kind, r.Address, link); why != nil {
		because, known := leftOutBecause(why)
		if !known {
			log.Printf("campaign %d: checking a mail: %v", c.ID, why)
			settle(MailQueued, "")
			return false
		}
		return settle(MailSkipped, because)
	}
	foot := c.Body + "\n\n-- \n" + o.Name + "\n" + strings.TrimSpace(o.Settings.PostalAddress) + "\n\n"
	var token, unsubscribe, body string
	if c.Kind == KindService {
		// About what they have, so not something to unsubscribe from: the foot says why it came.
		body = foot + "You are getting this as a customer of " + o.Name + ": it is about the service you have with them, so it reaches you even if you unsubscribed from their marketing.\n"
	} else {
		var err error
		if token, err = a.s.unsubscribeLink(a.s.db, o.ID, r.Address); err != nil {
			log.Printf("campaign %d: %v", c.ID, err)
			settle(MailQueued, "")
			return false
		}
		unsubscribe = a.base + "/u/" + token
		body = foot + "You are getting this because you are in touch with " + o.Name + ". To stop these mails: " + unsubscribe + "\n"
	}
	sendCtx, cancel := context.WithTimeout(ctx, sendLimit)
	defer cancel()
	sendErr := a.deliver(sendCtx, *via, Message{To: r.Address, Subject: c.Subject, Body: body, Unsubscribe: unsubscribe})
	switch {
	case sendErr == nil:
		stored(a.s.db.Model(&Campaign{}).Where("id = ?", c.ID).Updates(map[string]any{"failures": 0, "sent_at": time.Now()}))
		return settle(MailSent, "")
	case errors.Is(sendErr, errRefusedRecipient):
		// One refused address is that address; several in a row is the server refusing
		// the campaign, which pauses it before the whole list is used up.
		a.s.dropUnsubscribeLink(token)
		log.Printf("campaign %d: %v", c.ID, sendErr)
		settled := settle(MailFailed, "the mail server refused this address")
		return a.failedToSend(c) && settled
	case errors.Is(sendErr, errUnsent):
		// Nothing went, so it waits for the next round, and the round stops: the server
		// is likely to refuse the next one too.
		a.s.dropUnsubscribeLink(token)
		log.Printf("campaign %d: %v", c.ID, sendErr)
		settle(MailQueued, "")
		a.failedToSend(c)
		return false
	default:
		log.Printf("campaign %d: %v", c.ID, sendErr)
		settled := settle(MailUnknown, "the mail server didn't confirm it while it was being sent, so it may have gone")
		return a.failedToSend(c) && settled
	}
}

// failedToSend counts a send that didn't plainly go, and pauses the campaign after
// several in a row. It reports whether the campaign is still sending.
func (a *App) failedToSend(c Campaign) bool {
	stored(a.s.db.Model(&Campaign{}).Where("id = ?", c.ID).Update("failures", gorm.Expr("failures + 1")))
	var now Campaign
	if stored(a.s.db.Where("id = ?", c.ID).First(&now)) && now.Failures >= failuresToPause {
		a.pauseCampaign(c, "the mail server couldn't be reached, refused, or didn't confirm several mails in a row")
		return false
	}
	return true
}

func (a *App) pauseCampaign(c Campaign, why string) {
	stored(a.s.db.Model(&Campaign{}).Where("id = ? and status = ?", c.ID, CampaignSending).
		Updates(map[string]any{"status": CampaignPaused, "paused": why}))
}

// finishIfDone marks a campaign sent once nothing of it is waiting or on its way.
func (a *App) finishIfDone(c Campaign) {
	var left int64
	if !stored(a.s.db.Model(&CampaignRecipient{}).Where("campaign_id = ? and status in ?", c.ID, []string{MailQueued, MailSending}).Count(&left)) || left > 0 {
		return
	}
	stored(a.s.db.Model(&Campaign{}).Where("id = ? and status = ?", c.ID, CampaignSending).Update("status", CampaignSent))
}

func (s *Store) dropUnsubscribeLink(token string) {
	if token != "" {
		stored(s.db.Where("token_hash = ?", hashToken(token)).Delete(&UnsubscribeLink{}))
	}
}
