package store

import (
	"strings"
	"time"
)

// Identity links an account to who a provider says somebody is. The issuer and its
// subject are the link; an email address only ever makes the first one.
type Identity struct {
	ID        int64  `gorm:"primaryKey"`
	UserID    int64  `gorm:"index;not null"`
	Provider  string `gorm:"uniqueIndex:idx_identity;not null"`
	Subject   string `gorm:"uniqueIndex:idx_identity;not null"`
	CreatedAt time.Time
}

// SSOConnection is an organisation's own sign-in: its OpenID Connect provider.
type SSOConnection struct {
	OrgID    int64  `gorm:"primaryKey;autoIncrement:false"`
	Issuer   string `gorm:"not null;default:''"`
	ClientID string
	// ClientSecret is sealed (see keyring.seal); Store.spec opens it.
	ClientSecret string
	// Required turns a password away from members at the organisation's domains.
	Required bool `gorm:"not null;default:false"`
	// AutoJoin makes a member of anyone at those domains the first time they sign in.
	AutoJoin  bool `gorm:"not null;default:false"`
	UpdatedAt time.Time
}

// SSODomain is an email domain an organisation says is its own, believed once the
// token is found in the domain's DNS.
type SSODomain struct {
	ID         int64  `gorm:"primaryKey"`
	OrgID      int64  `gorm:"uniqueIndex:idx_sso_domain;not null"`
	Domain     string `gorm:"uniqueIndex:idx_sso_domain;not null"`
	Token      string
	VerifiedAt *time.Time
}

func (d SSODomain) Record() string { return ChallengeLabel + "." + d.Domain }

const ChallengeLabel = "_prospector-challenge"

// HasPassword is false for an account that only ever signs in through a provider.
func (u *User) HasPassword() bool {
	return u.PasswordHash != "" && !strings.HasPrefix(u.PasswordHash, "!")
}
