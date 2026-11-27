package identity

import (
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/shutthegoatup/prospector/internal/say"
	"github.com/shutthegoatup/prospector/internal/store"
)

func (s *IdentityStore) newPasswordToken(user int64, invite bool) (string, error) {
	life := resetLife
	if invite {
		life = inviteLife
	}
	token := store.RandomToken(32)
	return token, s.DB.Create(&store.PasswordToken{TokenHash: store.HashToken(token), UserID: user, Invite: invite, ExpiresAt: time.Now().Add(life)}).Error
}

// ResetToken refuses a second one inside resetGap. It counts from the table, so a
// restart doesn't reset the wait.
func (s *IdentityStore) ResetToken(user int64) (string, error) {
	var recent int64
	if err := s.DB.Model(&store.PasswordToken{}).Where("user_id = ? and invite = ? and created_at > ?", user, false, time.Now().Add(-ResetGap)).Count(&recent).Error; err != nil {
		return "", err
	}
	if recent > 0 {
		return "", ErrTooSoon
	}
	_ = store.Logged(s.DB.Where("expires_at < ?", time.Now()).Delete(&store.PasswordToken{}))
	return s.newPasswordToken(user, false)
}

// InviteToken replaces any earlier invitation, and refuses a second inside resetGap, so
// adding someone over and over can't fill their inbox.
func (s *IdentityStore) InviteToken(user int64) (string, error) {
	var recent int64
	if err := s.DB.Model(&store.PasswordToken{}).Where("user_id = ? and invite = ? and created_at > ?", user, true, time.Now().Add(-ResetGap)).Count(&recent).Error; err != nil {
		return "", err
	}
	if recent > 0 {
		return "", ErrTooSoon
	}
	if err := s.DropInvites(user); err != nil {
		return "", err
	}
	return s.newPasswordToken(user, true)
}

func (s *IdentityStore) DropInvites(user int64) error {
	return s.DB.Where("user_id = ? and invite = ?", user, true).Delete(&store.PasswordToken{}).Error
}

func (s *IdentityStore) PasswordTokenUser(token string) (*store.User, error) {
	var t store.PasswordToken
	if err := s.DB.Where("token_hash = ? and expires_at > ?", store.HashToken(token), time.Now()).First(&t).Error; err != nil {
		return nil, ErrBadLink
	}
	var u store.User
	return &u, s.DB.First(&u, t.UserID).Error
}

// RedeemPasswordToken sets the password and signs the account out everywhere. The
// token is spent first, inside the transaction, so two requests can't both use it.
func (s *IdentityStore) RedeemPasswordToken(token, password string) error {
	u, err := s.PasswordTokenUser(token)
	if err != nil {
		return err
	}
	if len(password) < 10 {
		return say.New("passwords need at least 10 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), PasswordCost)
	if err != nil {
		return err
	}
	return s.DB.Transaction(func(tx *gorm.DB) error {
		spent := tx.Where("token_hash = ? and expires_at > ?", store.HashToken(token), time.Now()).Delete(&store.PasswordToken{})
		if spent.Error != nil {
			return spent.Error
		}
		if spent.RowsAffected != 1 {
			return ErrBadLink
		}
		return s.savePasswordIn(tx, u.ID, string(hash), "")
	})
}

func (s *IdentityStore) OrgMail(org int64) (*store.OrgMail, error) {
	var m store.OrgMail
	return &m, s.DB.First(&m, "org_id = ?", org).Error
}

func (s *IdentityStore) SaveOrgMail(m *store.OrgMail) error { return s.DB.Save(m).Error }

func (s *IdentityStore) DeleteOrgMail(org int64) error {
	return s.DB.Delete(&store.OrgMail{}, "org_id = ?", org).Error
}
