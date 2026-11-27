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

type SSO struct {
	SSOConnection
	Domains []SSODomain
}

func (c *SSO) On() bool { return c.Issuer != "" }

func EmailDomain(email string) string {
	_, domain, _ := strings.Cut(strings.ToLower(strings.TrimSpace(email)), "@")
	return domain
}

// Proves is whether the organisation's sign-in may speak for an address: only one at
// a domain it has shown is its own, matched whole.
func (c *SSO) Proves(email string) bool {
	domain := EmailDomain(email)
	for _, d := range c.Domains {
		if d.VerifiedAt != nil && d.Domain == domain && domain != "" {
			return true
		}
	}
	return false
}

// IdpSpec opens the connection's client secret, which the database holds sealed.
func (k *Keyring) IdpSpec(c *SSO) (IdpSpec, error) {
	secret, err := k.Open(c.ClientSecret)
	return IdpSpec{Issuer: c.Issuer, ClientID: c.ClientID, Secret: secret}, err
}

func UnusableHash() string { return "!" + RandomToken(32) }
