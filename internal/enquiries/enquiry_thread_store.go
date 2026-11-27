package enquiries

import (
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/shutthegoatup/prospector/internal/prospecting"
	"github.com/shutthegoatup/prospector/internal/say"
	"github.com/shutthegoatup/prospector/internal/store"
)

// EnquiryThread is what has been said in answering one enquiry, oldest first.
func (s *EnquiryStore) EnquiryThread(org, enquiry int64) ([]store.EnquiryMessage, error) {
	if err := s.InOrg(org).Where("id = ?", enquiry).First(&store.Enquiry{}).Error; err != nil {
		return nil, store.Missing(err, "no enquiry %d", enquiry)
	}
	threads, err := s.EnquiryThreads(org, []int64{enquiry})
	if threads[enquiry] == nil {
		return []store.EnquiryMessage{}, err
	}
	return threads[enquiry], err
}

func (s *EnquiryStore) EnquiryThreads(org int64, enquiries []int64) (map[int64][]store.EnquiryMessage, error) {
	out := map[int64][]store.EnquiryMessage{}
	if len(enquiries) == 0 {
		return out, nil
	}
	var all []store.EnquiryMessage
	if err := s.InOrg(org).Where("enquiry_id in ?", enquiries).Where(InThread).Order("id").Find(&all).Error; err != nil {
		return nil, err
	}
	for _, m := range all {
		out[m.EnquiryID] = append(out[m.EnquiryID], m)
	}
	return out, nil
}

// StartReply keeps a reply before it is sent, so two presses of the button are one
// reply. It is not in the thread until settleReply says it went.
func (s *EnquiryStore) StartReply(o *store.Org, enquiry int64, text, actor string) (*store.EnquiryMessage, string, error) {
	text, err := threadText(text)
	if err != nil {
		return nil, "", err
	}
	var m *store.EnquiryMessage
	var to string
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		if err := store.Hold(tx, &store.Enquiry{}, o.ID, enquiry); err != nil {
			return err
		}
		var e store.Enquiry
		if err := tx.Where("org_id = ? and id = ?", o.ID, enquiry).First(&e).Error; err != nil {
			return store.Missing(err, "no enquiry %d", enquiry)
		}
		if to, err = answerable(o, e); err != nil {
			return err
		}
		if err := prospecting.ErasedIn(tx, o.ID, "", "", to, ""); err != nil {
			return say.New("that address belongs to someone who was erased on request, so nothing is sent to it")
		}
		// Replies in a row with no answer between: a card pressed again and again, or a
		// key in a loop, shouldn't be able to fill someone's inbox from here.
		var last store.EnquiryMessage
		heard := tx.Where("org_id = ? and enquiry_id = ? and ours = ?", o.ID, enquiry, false).Order("id desc").Limit(1).Find(&last)
		if heard.Error != nil {
			return heard.Error
		}
		var unanswered int64
		if err := tx.Model(&store.EnquiryMessage{}).Where("org_id = ? and enquiry_id = ? and ours = ? and id > ? and (sent_at is not null or created_at > ?)", o.ID, enquiry, true, last.ID, time.Now().Add(-replyStale)).Count(&unanswered).Error; err != nil {
			return err
		}
		if unanswered >= repliesUnanswered {
			return say.Errorf("%d replies have gone with no answer recorded; record what they said, or leave it there", repliesUnanswered)
		}
		now := time.Now()
		mine := tx.Where("org_id = ? and enquiry_id = ? and ours = ?", o.ID, enquiry, true)
		if err := mine.Where("sent_at is null and created_at < ?", now.Add(-replyStale)).Delete(&store.EnquiryMessage{}).Error; err != nil {
			return err
		}
		var same int64
		if err := tx.Model(&store.EnquiryMessage{}).Where("org_id = ? and enquiry_id = ? and ours = ? and text = ? and created_at > ?", o.ID, enquiry, true, text, now.Add(-replyGap)).Count(&same).Error; err != nil {
			return err
		}
		if same > 0 {
			return say.New("that reply has just been sent; change it to send another")
		}
		m = &store.EnquiryMessage{OrgID: o.ID, EnquiryID: enquiry, Ours: true, Text: text, By: actor, CreatedAt: now}
		return tx.Create(m).Error
	})
	return m, to, err
}

// SettleReply records how sending went. A reply that went joins the thread, and the
// enquiry waits on them, with an owner if it had none. One that didn't is forgotten,
// and the enquiry stays as it was.
func (s *EnquiryStore) SettleReply(org int64, m *store.EnquiryMessage, sent bool, actor string) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		if !sent {
			return tx.Where("org_id = ? and id = ? and sent_at is null", org, m.ID).Delete(&store.EnquiryMessage{}).Error
		}
		var e store.Enquiry
		if err := tx.Where("org_id = ? and id = ?", org, m.EnquiryID).First(&e).Error; err != nil {
			// Deleted while the mail was on its way: what was sent has nothing to belong to.
			return tx.Where("org_id = ? and id = ?", org, m.ID).Delete(&store.EnquiryMessage{}).Error
		}
		now := time.Now()
		m.SentAt = &now
		if err := tx.Model(&store.EnquiryMessage{}).Where("org_id = ? and id = ?", org, m.ID).Update("sent_at", now).Error; err != nil {
			return err
		}
		updates := map[string]any{"state": EnquiryWaiting}
		if e.Owner == "" && !strings.HasPrefix(actor, "agent:") {
			updates["owner"] = actor
		}
		if err := tx.Model(&store.Enquiry{}).Where("org_id = ? and id = ?", org, e.ID).Updates(updates).Error; err != nil {
			return err
		}
		return s.logEnquiry(tx, e, actor, "enquiry:reply")
	})
}

// logEnquiry puts a turn in the history of whoever wrote in, when they are held here.
// It says that it happened, not what was said: the words are in the thread.
func (s *EnquiryStore) logEnquiry(tx *gorm.DB, e store.Enquiry, actor, action string) error {
	if e.ContactID == nil && e.CompanyID == nil {
		return nil
	}
	var contact int64
	if e.ContactID != nil {
		contact = *e.ContactID
	}
	return s.Log(tx, e.OrgID, contact, e.CompanyID, actor, action, nil)
}

// HearEnquiry records what they said back, pasted in by whoever read it. Nothing is
// sent. The enquiry is ours to answer again.
func (s *EnquiryStore) HearEnquiry(org, enquiry int64, text, actor string) (*store.EnquiryMessage, error) {
	text, err := threadText(text)
	if err != nil {
		return nil, err
	}
	var m *store.EnquiryMessage
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		var e store.Enquiry
		if err := tx.Where("org_id = ? and id = ?", org, enquiry).First(&e).Error; err != nil {
			return store.Missing(err, "no enquiry %d", enquiry)
		}
		if e.Spam {
			return say.New("that was marked as spam, so there is nothing to follow")
		}
		m = &store.EnquiryMessage{OrgID: org, EnquiryID: enquiry, Text: text, By: actor, CreatedAt: time.Now()}
		if err := tx.Create(m).Error; err != nil {
			return err
		}
		if err := tx.Model(&store.Enquiry{}).Where("org_id = ? and id = ?", org, enquiry).Update("state", EnquiryOpen).Error; err != nil {
			return err
		}
		return s.logEnquiry(tx, e, actor, "enquiry:heard")
	})
	return m, err
}
