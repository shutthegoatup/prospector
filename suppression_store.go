package main

import (
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/shutthegoatup/prospector/internal/paging"
)

// suppressionStore is the suppression area's queries: addresses no marketing mail goes to, and their unsubscribe links.
type suppressionStore struct{ *core }

// Suppress puts an address on the list. Asking twice changes nothing, and an address
// someone unsubscribed stays theirs even if a member adds it too.
func (s *suppressionStore) Suppress(org int64, address, actor string) (*Suppression, error) {
	addr, err := bareAddress(address)
	if err != nil {
		return nil, err
	}
	return s.suppress(s.DB, org, addr, SuppressAdded, actor)
}

func (s *suppressionStore) suppress(tx *gorm.DB, org int64, addr, reason, actor string) (*Suppression, error) {
	// A row a member is taking off at the same moment is put back, not lost: each round
	// either finds the row and makes it stronger, or creates it.
	for range 3 {
		row := Suppression{OrgID: org, Address: addr, Reason: reason, By: actor, CreatedAt: time.Now()}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
			return nil, err
		}
		var kept Suppression
		found := tx.Where("org_id = ? and address = ?", org, addr).Limit(1).Find(&kept)
		if found.Error != nil {
			return nil, found.Error
		}
		if found.RowsAffected == 0 {
			continue
		}
		if suppressRank[reason] <= suppressRank[kept.Reason] {
			return &kept, nil
		}
		made := tx.Model(&Suppression{}).Where("id = ? and reason = ?", kept.ID, kept.Reason).Updates(map[string]any{"reason": reason, "by": actor})
		if made.Error != nil {
			return nil, made.Error
		}
		if made.RowsAffected == 1 {
			kept.Reason, kept.By = reason, actor
			return &kept, nil
		}
	}
	return nil, errors.New("the list was changing too fast to record that; try again")
}

// Unsuppress takes off an address a member or key added, or one that bounced, as when
// it was mended. One the person unsubscribed or complained about can't be taken off
// from here: only they can ask again.
func (s *suppressionStore) Unsuppress(org, id int64, actor string) error {
	var row Suppression
	if err := s.InOrg(org).Where("id = ?", id).First(&row).Error; err != nil {
		return missing(err, "no suppression %d", id)
	}
	// Checked again in the delete itself: they may have unsubscribed since it was read.
	gone := s.InOrg(org).Where("id = ? and reason in ?", id, []string{SuppressAdded, SuppressBounced}).Delete(&Suppression{})
	if gone.Error != nil {
		return gone.Error
	}
	if gone.RowsAffected == 0 {
		return errors.New("they asked themselves not to be mailed, by unsubscribing or marking a mail as spam, so only they can be mailed again by signing up again")
	}
	return nil
}

// Suppressions are the list, newest first, a page at a time.
func (s *suppressionStore) Suppressions(org int64, page paging.Page) ([]Suppression, string, error) {
	at, err := page.At(1)
	if err != nil {
		return nil, "", err
	}
	if at[0] == paging.NoneBefore {
		return []Suppression{}, "", nil
	}
	q := s.InOrg(org)
	if at[0] > 0 {
		q = q.Where("id < ?", at[0])
	}
	rows := []Suppression{}
	if err := q.Order("id desc").Limit(page.Size() + 1).Find(&rows).Error; err != nil {
		return nil, "", err
	}
	if len(rows) <= page.Size() {
		return rows, "", nil
	}
	rows = rows[:page.Size()]
	return rows, paging.Cursor(rows[len(rows)-1].ID), nil
}

// marketable says why marketing mail can't go to an address, or nil when it can: it
// is one address, not on the suppression list, not on the never-contact list (by the
// address or the person's LinkedIn profile), and not someone erased on request. An
// error that leftOutBecause doesn't know is a failure to check, not a reason.
func (s *suppressionStore) marketable(tx *gorm.DB, o *Org, address, link string) error {
	g, err := s.gate(tx, o)
	if err != nil {
		return err
	}
	return g.why(address, link)
}

func (s *suppressionStore) gate(tx *gorm.DB, o *Org) (*mailGate, error) {
	g := &mailGate{o: o, suppressed: map[string]Suppression{}, erased: map[string]bool{}}
	var listed []Suppression
	var hashes []string
	if err := tx.Model(&Suppression{}).Select("address", "reason", "bounced_at").Where("org_id = ?", o.ID).Find(&listed).Error; err != nil {
		return nil, err
	}
	if err := tx.Model(&Erasure{}).Where("org_id = ?", o.ID).Pluck("hash", &hashes).Error; err != nil {
		return nil, err
	}
	for _, l := range listed {
		g.suppressed[l.Address] = l
	}
	for _, h := range hashes {
		g.erased[h] = true
	}
	return g, nil
}

// unsubscribeLink makes the secret for one mail's unsubscribe link to an address.
func (s *suppressionStore) unsubscribeLink(tx *gorm.DB, org int64, address string) (string, error) {
	addr, err := bareAddress(address)
	if err != nil {
		return "", err
	}
	token := randomToken(32)
	return token, tx.Create(&UnsubscribeLink{OrgID: org, Address: addr, TokenHash: hashToken(token), CreatedAt: time.Now()}).Error
}

// UnsubscribeByToken is the one look-up that starts from a secret and not an
// organisation. It reports nothing back: a link that was never made and one that was
// are answered alike.
func (s *suppressionStore) UnsubscribeByToken(token string) error {
	if token == "" {
		return nil
	}
	return s.DB.Transaction(func(tx *gorm.DB) error {
		var link UnsubscribeLink
		found := tx.Where("token_hash = ?", hashToken(token)).Limit(1).Find(&link)
		if found.Error != nil || found.RowsAffected == 0 {
			return found.Error
		}
		_, err := s.suppress(tx, link.OrgID, link.Address, SuppressUnsubscribed, "")
		return err
	})
}
