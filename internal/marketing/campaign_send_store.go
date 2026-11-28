package marketing

import (
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/shutthegoatup/prospector/internal/say"
	"github.com/shutthegoatup/prospector/internal/store"
)

// FreeStaleMail gives up, in every organisation, on mails claimed before a time: their
// server stopped while sending them, so whether they went isn't known.
func (s *OutreachStore) FreeStaleMail(before time.Time) error {
	return s.DB.Model(&store.CampaignRecipient{}).Where("status = ? and claimed_at < ?", MailSending, before).
		Updates(map[string]any{"status": MailUnknown, "reason": "the server stopped while it was being sent"}).Error
}

// CampaignsSending is every organisation's campaigns being sent.
func (s *OutreachStore) CampaignsSending() ([]store.Campaign, error) {
	var going []store.Campaign
	return going, s.DB.Where("status = ?", CampaignSending).Order("id").Find(&going).Error
}

func (s *OutreachStore) MailGate(o *store.Org) (*MailGate, error) { return s.Suppression.Gate(s.DB, o) }

func (s *OutreachStore) UnsubscribeLink(org int64, address string) (string, error) {
	return s.Suppression.MintUnsubscribeLink(s.DB, org, address)
}

// MailSentSince counts the campaign mails an organisation has sent since a time.
func (s *OutreachStore) MailSentSince(org int64, since time.Time) (int64, error) {
	var n int64
	return n, s.InOrg(org).Model(&store.CampaignRecipient{}).Where("status = ? and sent_at >= ?", MailSent, since).Count(&n).Error
}

// NextQueued is the campaign's next mail to send, or nil when none is left.
func (s *OutreachStore) NextQueued(org, campaign int64) (*store.CampaignRecipient, error) {
	var r store.CampaignRecipient
	res := s.InOrg(org).Where("campaign_id = ? and status = ?", campaign, MailQueued).Order("id").Limit(1).Find(&r)
	if res.Error != nil || res.RowsAffected == 0 {
		return nil, res.Error
	}
	return &r, nil
}

// SkipMail leaves out a mail still queued.
func (s *OutreachStore) SkipMail(org, id int64, reason string) error {
	return s.InOrg(org).Model(&store.CampaignRecipient{}).Where("id = ? and status = ?", id, MailQueued).
		Updates(map[string]any{"status": MailSkipped, "reason": reason}).Error
}

// ClaimMail holds a mail for sending only while it is still queued to the address that
// was checked, so an erasure or a cancel that lands first leaves nothing to send.
func (s *OutreachStore) ClaimMail(org, id int64, address string) (bool, error) {
	res := s.InOrg(org).Model(&store.CampaignRecipient{}).Where("id = ? and status = ? and address = ?", id, MailQueued, address).
		Updates(map[string]any{"status": MailSending, "claimed_at": time.Now()})
	return res.RowsAffected > 0, res.Error
}

// SettleMail records how a claimed mail went; one put back in the queue is no longer claimed.
func (s *OutreachStore) SettleMail(org, id int64, status, reason string) error {
	updates := map[string]any{"status": status, "reason": reason}
	if status == MailSent {
		updates["sent_at"] = time.Now()
	}
	if status == MailQueued {
		updates["claimed_at"] = nil
	}
	return s.InOrg(org).Model(&store.CampaignRecipient{}).Where("id = ?", id).Updates(updates).Error
}

// CampaignWent records a mail that went, which ends any run of failures.
func (s *OutreachStore) CampaignWent(org, id int64) error {
	return s.InOrg(org).Model(&store.Campaign{}).Where("id = ?", id).Updates(map[string]any{"failures": 0, "sent_at": time.Now()}).Error
}

// CampaignFailed counts a mail that didn't plainly go, and says how many have in a row.
func (s *OutreachStore) CampaignFailed(org, id int64) (int, error) {
	if err := s.InOrg(org).Model(&store.Campaign{}).Where("id = ?", id).Update("failures", gorm.Expr("failures + 1")).Error; err != nil {
		return 0, err
	}
	c, err := s.Campaign(org, id)
	if err != nil {
		return 0, err
	}
	return c.Failures, nil
}

func (s *OutreachStore) PauseCampaign(org, id int64, why string) error {
	return s.InOrg(org).Model(&store.Campaign{}).Where("id = ? and status = ?", id, CampaignSending).
		Updates(map[string]any{"status": CampaignPaused, "paused": why}).Error
}

// FinishIfDone marks a campaign sent once nothing of it is waiting or on its way.
func (s *OutreachStore) FinishIfDone(org, id int64) error {
	var left int64
	if err := s.InOrg(org).Model(&store.CampaignRecipient{}).Where("campaign_id = ? and status in ?", id, []string{MailQueued, MailSending}).Count(&left).Error; err != nil || left > 0 {
		return err
	}
	return s.InOrg(org).Model(&store.Campaign{}).Where("id = ? and status = ?", id, CampaignSending).Update("status", CampaignSent).Error
}

// SendCampaign fixes who a draft goes to, as its audience stands now, and sets it
// going. Asked twice at once, it is sent once.
func (s *OutreachStore) SendCampaign(o *store.Org, id int64, actor string) (*store.Campaign, error) {
	var c store.Campaign
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("org_id = ? and id = ?", o.ID, id).First(&c).Error; err != nil {
			return store.Missing(err, "no campaign %d", id)
		}
		if c.Status != CampaignDraft {
			return say.New("it has been sent already")
		}
		if strings.TrimSpace(c.Subject) == "" || strings.TrimSpace(c.Body) == "" {
			return say.New("a campaign needs a subject and something to say before it is sent")
		}
		now := time.Now()
		flipped := tx.Model(&store.Campaign{}).Where("org_id = ? and id = ? and status = ?", o.ID, id, CampaignDraft).
			Updates(map[string]any{"status": CampaignSending, "queued_by": actor, "queued_at": now, "paused": "", "failures": 0})
		if flipped.Error != nil {
			return flipped.Error
		}
		if flipped.RowsAffected == 0 {
			return say.New("it has been sent already")
		}
		// Read again now it is ours: a change that landed between the first read and the
		// flip, or a row an older release wrote, must not send a service notice to anyone
		// but customers.
		if err := tx.Where("org_id = ? and id = ?", o.ID, id).First(&c).Error; err != nil {
			return err
		}
		if err := c.Fits(); err != nil {
			return err
		}
		w, err := s.audienceWork(o)
		if err != nil {
			return err
		}
		people, err := s.audience(o, c, w)
		if err != nil {
			return err
		}
		var rows []store.CampaignRecipient
		for _, p := range people {
			if p.LeftOut != nil {
				continue
			}
			contact := p.ContactID
			rows = append(rows, store.CampaignRecipient{OrgID: o.ID, CampaignID: id, Address: p.Address, ContactID: &contact, Name: p.Name, Status: MailQueued})
		}
		if len(rows) == 0 {
			return say.New("nobody is left for it to go to")
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(rows, 200).Error; err != nil {
			return err
		}
		return tx.Where("org_id = ? and id = ?", o.ID, id).First(&c).Error
	})
	return &c, err
}

// CancelCampaign stops what hasn't gone. What went stays recorded.
func (s *OutreachStore) CancelCampaign(org, id int64) (*store.Campaign, error) {
	var c store.Campaign
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&store.Campaign{}).Where("org_id = ? and id = ? and status in ?", org, id, []string{CampaignSending, CampaignPaused}).
			Update("status", CampaignCancelled)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			if err := tx.Where("org_id = ? and id = ?", org, id).First(&c).Error; err != nil {
				return store.Missing(err, "no campaign %d", id)
			}
			if t, ok := notCancellable[c.Status]; ok {
				return t
			}
			return say.Errorf("a campaign that is %s can't be cancelled", c.Status)
		}
		if err := tx.Model(&store.CampaignRecipient{}).Where("org_id = ? and campaign_id = ? and status = ?", org, id, MailQueued).
			Update("status", MailCancelled).Error; err != nil {
			return err
		}
		return tx.Where("org_id = ? and id = ?", org, id).First(&c).Error
	})
	return &c, err
}

// ResumeCampaign sets a paused campaign going again, once someone has looked.
func (s *OutreachStore) ResumeCampaign(org, id int64) (*store.Campaign, error) {
	res := s.InOrg(org).Model(&store.Campaign{}).Where("id = ? and status = ?", id, CampaignPaused).
		Updates(map[string]any{"status": CampaignSending, "paused": "", "failures": 0})
	if res.Error != nil {
		return nil, res.Error
	}
	c, err := s.Campaign(org, id)
	if err != nil {
		return nil, err
	}
	if res.RowsAffected == 0 {
		return nil, say.Errorf("only a paused campaign can be resumed, and this one is %s", c.Status)
	}
	return c, nil
}

// CampaignTally counts a campaign's mails by how they went.
func (s *OutreachStore) CampaignTally(org int64, ids []int64) (map[int64]map[string]int, error) {
	out := map[int64]map[string]int{}
	if len(ids) == 0 {
		return out, nil
	}
	var rows []struct {
		CampaignID int64
		Status     string
		N          int
	}
	if err := s.InOrg(org).Model(&store.CampaignRecipient{}).Select("campaign_id, status, count(*) as n").Where("campaign_id in ?", ids).
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

func (s *OutreachStore) CampaignRecipients(org, campaign int64) ([]store.CampaignRecipient, error) {
	if _, err := s.Campaign(org, campaign); err != nil {
		return nil, err
	}
	out := []store.CampaignRecipient{}
	return out, s.InOrg(org).Where("campaign_id = ?", campaign).Order("id").Find(&out).Error
}

func (s *OutreachStore) DropUnsubscribeLink(token string) {
	if token != "" {
		stored(s.DB.Where("token_hash = ?", store.HashToken(token)).Delete(&store.UnsubscribeLink{}))
	}
}

// notCancellable are whole sentences, so each status is worded in the reader's language.
var notCancellable = map[string]*say.Text{
	CampaignDraft:     say.New("a campaign that is draft can't be cancelled"),
	CampaignSent:      say.New("a campaign that is sent can't be cancelled"),
	CampaignCancelled: say.New("a campaign that is cancelled can't be cancelled"),
}
