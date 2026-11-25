package main

import (
	"errors"
	"log"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Suppression is an address the organisation's marketing mail never goes to again. One
// list serves every mailing: someone who unsubscribed from one has unsubscribed.
type Suppression struct {
	ID      int64  `gorm:"primaryKey" json:"id"`
	OrgID   int64  `gorm:"uniqueIndex:idx_suppression_address,priority:1;not null" json:"-"`
	Address string `gorm:"uniqueIndex:idx_suppression_address,priority:2;not null" json:"address"`
	// Reason is "unsubscribed" when they asked, by a link in a mail, and "added" when a
	// member or key put them here. Only an added one can be taken off.
	Reason    string    `gorm:"not null;default:'added'" json:"reason"`
	By        string    `gorm:"not null;default:''" json:"by"`
	CreatedAt time.Time `json:"created_at"`
}

const (
	SuppressUnsubscribed = "unsubscribed"
	SuppressAdded        = "added"
)

// UnsubscribeLink is the secret in one mail's unsubscribe link, kept as its hash. It
// never runs out, because the mail it is in doesn't.
type UnsubscribeLink struct {
	ID        int64     `gorm:"primaryKey"`
	OrgID     int64     `gorm:"index;not null"`
	Address   string    `gorm:"index;not null"`
	TokenHash string    `gorm:"uniqueIndex;not null"`
	CreatedAt time.Time `gorm:"index"`
}

var errNotAnAddress = errors.New("that isn't one email address")

func bareAddress(address string) (string, error) {
	at, err := mail.ParseAddress(strings.TrimSpace(address))
	if err != nil {
		return "", errNotAnAddress
	}
	return strings.ToLower(at.Address), nil
}

// Suppress puts an address on the list. Asking twice changes nothing, and an address
// someone unsubscribed stays theirs even if a member adds it too.
func (s *Store) Suppress(org int64, address, actor string) (*Suppression, error) {
	addr, err := bareAddress(address)
	if err != nil {
		return nil, err
	}
	return s.suppress(s.db, org, addr, SuppressAdded, actor)
}

func (s *Store) suppress(tx *gorm.DB, org int64, addr, reason, actor string) (*Suppression, error) {
	// Their own word outranks a member's: once they unsubscribe, it is theirs. A row a
	// member is taking off at the same moment is put back, not lost: each round either
	// finds the row and makes it theirs, or creates it.
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
		if reason != SuppressUnsubscribed || kept.Reason == SuppressUnsubscribed {
			return &kept, nil
		}
		made := tx.Model(&Suppression{}).Where("id = ?", kept.ID).Updates(map[string]any{"reason": reason, "by": actor})
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

// Unsuppress takes off an address a member or key added. One that was unsubscribed by
// the person can't be taken off from here: only they can ask again.
func (s *Store) Unsuppress(org, id int64, actor string) error {
	var row Suppression
	if err := s.in(org).Where("id = ?", id).First(&row).Error; err != nil {
		return missing(err, "no suppression %d", id)
	}
	// Checked again in the delete itself: they may have unsubscribed since it was read.
	gone := s.in(org).Where("id = ? and reason = ?", id, SuppressAdded).Delete(&Suppression{})
	if gone.Error != nil {
		return gone.Error
	}
	if row.Reason == SuppressUnsubscribed || gone.RowsAffected == 0 {
		return errors.New("they unsubscribed themselves, so only they can be mailed again by signing up again")
	}
	return nil
}

// Suppressions are the list, newest first, a page at a time.
func (s *Store) Suppressions(org int64, page PageIn) ([]Suppression, string, error) {
	at, err := page.at(1)
	if err != nil {
		return nil, "", err
	}
	if at[0] == noneBefore {
		return []Suppression{}, "", nil
	}
	q := s.in(org)
	if at[0] > 0 {
		q = q.Where("id < ?", at[0])
	}
	rows := []Suppression{}
	if err := q.Order("id desc").Limit(page.size() + 1).Find(&rows).Error; err != nil {
		return nil, "", err
	}
	if len(rows) <= page.size() {
		return rows, "", nil
	}
	rows = rows[:page.size()]
	return rows, cursorOf(rows[len(rows)-1].ID), nil
}

// marketable says why marketing mail can't go to an address, or nil when it can: it
// is one address, not on the suppression list, not on the never-contact list, and not
// someone erased on request.
func (s *Store) marketable(tx *gorm.DB, o *Org, address string) error {
	addr, err := bareAddress(address)
	if err != nil {
		return err
	}
	var n int64
	if err := tx.Model(&Suppression{}).Where("org_id = ? and address = ?", o.ID, addr).Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return errSuppressed
	}
	if err := o.Settings.listed(addr, ""); err != nil {
		return err
	}
	return erased(tx, o.ID, "", "", addr, "")
}

var errSuppressed = errors.New("that address is on the list not to be mailed")

// unsubscribeLink makes the secret for one mail's unsubscribe link to an address.
func (s *Store) unsubscribeLink(tx *gorm.DB, org int64, address string) (string, error) {
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
func (s *Store) UnsubscribeByToken(token string) error {
	if token == "" {
		return nil
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		var link UnsubscribeLink
		found := tx.Where("token_hash = ?", hashToken(token)).Limit(1).Find(&link)
		if found.Error != nil || found.RowsAffected == 0 {
			return found.Error
		}
		_, err := s.suppress(tx, link.OrgID, link.Address, SuppressUnsubscribed, "")
		return err
	})
}

// forgetAddresses drops what is held against addresses being erased. The erasure's own
// hash of each keeps them from being mailed again.
func forgetAddresses(tx *gorm.DB, org int64, addresses []string) error {
	if len(addresses) == 0 {
		return nil
	}
	// As the erasure's own hash does: a dot at either end is the sentence's, not the address's.
	for _, a := range addresses {
		addresses = append(addresses, strings.Trim(a, "."))
	}
	if err := tx.Where("org_id = ? and address in ?", org, addresses).Delete(&Suppression{}).Error; err != nil {
		return err
	}
	return tx.Where("org_id = ? and address in ?", org, addresses).Delete(&UnsubscribeLink{}).Error
}

// UnsubscribeView is everything the page at an unsubscribe link shows: nothing about
// whose link it is, since mail gets forwarded.
type UnsubscribeView struct {
	Token string
	Done  bool
}

func (a *App) unsubscribePage(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	// Only something shaped like a link we make goes back into the page's form: the path
	// arrives decoded, so "..%2Flogout" would otherwise aim the button at another route.
	if !linkToken(token) {
		token = ""
	}
	a.unsubscribeRender(w, r, UnsubscribeView{Token: token})
}

// linkToken is whether a path segment has the shape randomToken(32) gives.
func linkToken(t string) bool {
	if len(t) != 43 {
		return false
	}
	for _, c := range t {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// unsubscribe takes the button on that page, and the one-click request a mail provider
// sends for the List-Unsubscribe-Post header. RFC 8058 has it answered without a
// redirect, and it comes with no cookie and no origin.
func (a *App) unsubscribe(w http.ResponseWriter, r *http.Request) {
	if err := a.s.UnsubscribeByToken(r.PathValue("token")); err != nil {
		log.Printf("an unsubscribe wasn't recorded: %v", err)
		http.Error(w, "that couldn't be saved; try the link again", http.StatusInternalServerError)
		return
	}
	a.unsubscribeRender(w, r, UnsubscribeView{Done: true})
}

func (a *App) unsubscribeRender(w http.ResponseWriter, r *http.Request, v UnsubscribeView) {
	customerHeaders(w)
	w.WriteHeader(http.StatusOK)
	if err := UnsubscribePage(v).Render(r.Context(), w); err != nil {
		log.Print(err)
	}
}
