package main

import (
	"errors"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func (s *identityStore) newPasswordToken(user int64, invite bool) (string, error) {
	life := resetLife
	if invite {
		life = inviteLife
	}
	token := randomToken(32)
	return token, s.DB.Create(&PasswordToken{TokenHash: hashToken(token), UserID: user, Invite: invite, ExpiresAt: time.Now().Add(life)}).Error
}

// ResetToken refuses a second one inside resetGap. It counts from the table, so a
// restart doesn't reset the wait.
func (s *identityStore) ResetToken(user int64) (string, error) {
	var recent int64
	if err := s.DB.Model(&PasswordToken{}).Where("user_id = ? and invite = ? and created_at > ?", user, false, time.Now().Add(-resetGap)).Count(&recent).Error; err != nil {
		return "", err
	}
	if recent > 0 {
		return "", errTooSoon
	}
	_ = logged(s.DB.Where("expires_at < ?", time.Now()).Delete(&PasswordToken{}))
	return s.newPasswordToken(user, false)
}

// InviteToken replaces any earlier invitation, and refuses a second inside resetGap, so
// adding someone over and over can't fill their inbox.
func (s *identityStore) InviteToken(user int64) (string, error) {
	var recent int64
	if err := s.DB.Model(&PasswordToken{}).Where("user_id = ? and invite = ? and created_at > ?", user, true, time.Now().Add(-resetGap)).Count(&recent).Error; err != nil {
		return "", err
	}
	if recent > 0 {
		return "", errTooSoon
	}
	if err := s.DropInvites(user); err != nil {
		return "", err
	}
	return s.newPasswordToken(user, true)
}

func (s *identityStore) DropInvites(user int64) error {
	return s.DB.Where("user_id = ? and invite = ?", user, true).Delete(&PasswordToken{}).Error
}

func (s *identityStore) PasswordTokenUser(token string) (*User, error) {
	var t PasswordToken
	if err := s.DB.Where("token_hash = ? and expires_at > ?", hashToken(token), time.Now()).First(&t).Error; err != nil {
		return nil, errBadLink
	}
	var u User
	return &u, s.DB.First(&u, t.UserID).Error
}

// RedeemPasswordToken sets the password and signs the account out everywhere. The
// token is spent first, inside the transaction, so two requests can't both use it.
func (s *identityStore) RedeemPasswordToken(token, password string) error {
	u, err := s.PasswordTokenUser(token)
	if err != nil {
		return err
	}
	if len(password) < 10 {
		return errors.New("passwords need at least 10 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), passwordCost)
	if err != nil {
		return err
	}
	return s.DB.Transaction(func(tx *gorm.DB) error {
		spent := tx.Where("token_hash = ? and expires_at > ?", hashToken(token), time.Now()).Delete(&PasswordToken{})
		if spent.Error != nil {
			return spent.Error
		}
		if spent.RowsAffected != 1 {
			return errBadLink
		}
		return s.savePasswordIn(tx, u.ID, string(hash), "")
	})
}

func (s *identityStore) OrgMail(org int64) (*OrgMail, error) {
	var m OrgMail
	return &m, s.DB.First(&m, "org_id = ?", org).Error
}

func (s *identityStore) SaveOrgMail(m *OrgMail) error { return s.DB.Save(m).Error }

func (s *identityStore) DeleteOrgMail(org int64) error {
	return s.DB.Delete(&OrgMail{}, "org_id = ?", org).Error
}
