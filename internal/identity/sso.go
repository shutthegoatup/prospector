package identity

import (
	"errors"

	"gorm.io/gorm"

	"github.com/shutthegoatup/prospector/internal/say"
	"github.com/shutthegoatup/prospector/internal/store"
)

const (
	maxSSODomains = 20
)

var ErrNoAccount = say.New("there's no account here for that address: ask whoever runs your organisation to add you")

var ErrUnvouched = say.New("the provider didn't confirm that address is yours, so it can't be matched to an account here")

var ErrElsewhere = say.New("that sign-in is linked to an account whose address isn't at this organisation's domains")

var ErrNotMember = say.New("you aren't a member of that organisation: ask one of its owners to add you")

// resolve finds whose sign-in a provider's answer is. A link made before wins; an
// address makes a new one only when byEmail says the provider may speak for it.
func resolve(tx *gorm.DB, who *Outsider, email string, byEmail bool, create func() (*store.User, error)) (*store.User, error) {
	var u store.User
	var id store.Identity
	err := tx.Where("provider = ? and subject = ?", who.Issuer, who.Subject).First(&id).Error
	if err == nil {
		return &u, tx.First(&u, id.UserID).Error
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if !byEmail || email == "" {
		return nil, ErrUnvouched
	}
	found := &u
	if err := tx.Where("email = ?", email).First(&u).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		if create == nil {
			return nil, ErrNoAccount
		}
		if found, err = create(); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	return found, tx.Create(&store.Identity{UserID: found.ID, Provider: who.Issuer, Subject: who.Subject}).Error
}

// Claim ends a first password its inviter saw, once the account's owner has arrived
// another way: they can't be asked to change a password they were never told.
func Claim(tx *gorm.DB, u *store.User) error {
	if !u.MustChangePassword {
		return nil
	}
	u.PasswordHash, u.MustChangePassword = store.UnusableHash(), false
	if err := tx.Model(u).Select("password_hash", "must_change_password").Updates(u).Error; err != nil {
		return err
	}
	// An emailed invitation could still set a password; it ends with the first password.
	if err := tx.Where("user_id = ?", u.ID).Delete(&store.PasswordToken{}).Error; err != nil {
		return err
	}
	return tx.Where("user_id = ?", u.ID).Delete(&store.Session{}).Error
}
