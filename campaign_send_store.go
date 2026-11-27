package main

import (
	"time"

	"gorm.io/gorm"
)

// FreeStaleMail gives up, in every organisation, on mails claimed before a time: their
// server stopped while sending them, so whether they went isn't known.
func (s *Store) FreeStaleMail(before time.Time) error {
	return s.db.Model(&CampaignRecipient{}).Where("status = ? and claimed_at < ?", MailSending, before).
		Updates(map[string]any{"status": MailUnknown, "reason": "the server stopped while it was being sent"}).Error
}

// CampaignsSending is every organisation's campaigns being sent.
func (s *Store) CampaignsSending() ([]Campaign, error) {
	var going []Campaign
	return going, s.db.Where("status = ?", CampaignSending).Order("id").Find(&going).Error
}

func (s *Store) MailGate(o *Org) (*mailGate, error) { return s.gate(s.db, o) }

func (s *Store) UnsubscribeLink(org int64, address string) (string, error) {
	return s.unsubscribeLink(s.db, org, address)
}

// MailSentSince counts the campaign mails an organisation has sent since a time.
func (s *Store) MailSentSince(org int64, since time.Time) (int64, error) {
	var n int64
	return n, s.in(org).Model(&CampaignRecipient{}).Where("status = ? and sent_at >= ?", MailSent, since).Count(&n).Error
}

// NextQueued is the campaign's next mail to send, or nil when none is left.
func (s *Store) NextQueued(org, campaign int64) (*CampaignRecipient, error) {
	var r CampaignRecipient
	res := s.in(org).Where("campaign_id = ? and status = ?", campaign, MailQueued).Order("id").Limit(1).Find(&r)
	if res.Error != nil || res.RowsAffected == 0 {
		return nil, res.Error
	}
	return &r, nil
}

// SkipMail leaves out a mail still queued.
func (s *Store) SkipMail(org, id int64, reason string) error {
	return s.in(org).Model(&CampaignRecipient{}).Where("id = ? and status = ?", id, MailQueued).
		Updates(map[string]any{"status": MailSkipped, "reason": reason}).Error
}

// ClaimMail holds a mail for sending only while it is still queued to the address that
// was checked, so an erasure or a cancel that lands first leaves nothing to send.
func (s *Store) ClaimMail(org, id int64, address string) (bool, error) {
	res := s.in(org).Model(&CampaignRecipient{}).Where("id = ? and status = ? and address = ?", id, MailQueued, address).
		Updates(map[string]any{"status": MailSending, "claimed_at": time.Now()})
	return res.RowsAffected > 0, res.Error
}

// SettleMail records how a claimed mail went; one put back in the queue is no longer claimed.
func (s *Store) SettleMail(org, id int64, status, reason string) error {
	updates := map[string]any{"status": status, "reason": reason}
	if status == MailSent {
		updates["sent_at"] = time.Now()
	}
	if status == MailQueued {
		updates["claimed_at"] = nil
	}
	return s.in(org).Model(&CampaignRecipient{}).Where("id = ?", id).Updates(updates).Error
}

// CampaignWent records a mail that went, which ends any run of failures.
func (s *Store) CampaignWent(org, id int64) error {
	return s.in(org).Model(&Campaign{}).Where("id = ?", id).Updates(map[string]any{"failures": 0, "sent_at": time.Now()}).Error
}

// CampaignFailed counts a mail that didn't plainly go, and says how many have in a row.
func (s *Store) CampaignFailed(org, id int64) (int, error) {
	if err := s.in(org).Model(&Campaign{}).Where("id = ?", id).Update("failures", gorm.Expr("failures + 1")).Error; err != nil {
		return 0, err
	}
	c, err := s.Campaign(org, id)
	if err != nil {
		return 0, err
	}
	return c.Failures, nil
}

func (s *Store) PauseCampaign(org, id int64, why string) error {
	return s.in(org).Model(&Campaign{}).Where("id = ? and status = ?", id, CampaignSending).
		Updates(map[string]any{"status": CampaignPaused, "paused": why}).Error
}

// FinishIfDone marks a campaign sent once nothing of it is waiting or on its way.
func (s *Store) FinishIfDone(org, id int64) error {
	var left int64
	if err := s.in(org).Model(&CampaignRecipient{}).Where("campaign_id = ? and status in ?", id, []string{MailQueued, MailSending}).Count(&left).Error; err != nil || left > 0 {
		return err
	}
	return s.in(org).Model(&Campaign{}).Where("id = ? and status = ?", id, CampaignSending).Update("status", CampaignSent).Error
}
