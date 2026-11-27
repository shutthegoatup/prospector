package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/shutthegoatup/prospector/internal/say"
)

// ssoStore is the single sign-on area's queries: an organisation's own identity provider and its domains.
type ssoStore struct{ *core }

func (s *ssoStore) SSO(org int64) (*SSO, error) {
	c := &SSO{}
	if err := s.DB.Where("org_id = ?", org).Limit(1).Find(&c.SSOConnection).Error; err != nil {
		return nil, err
	}
	c.OrgID = org
	return c, s.InOrg(org).Order("domain").Find(&c.Domains).Error
}

// SaveSSO keeps the connection; an empty issuer turns the organisation's sign-in off.
func (s *ssoStore) SaveSSO(c *SSOConnection, actor string) error {
	// Sealed here, whoever is saving, so nothing a key could seal is written in clear.
	if c.ClientSecret != "" && !sealed(c.ClientSecret) && s.Keyring != nil {
		stored, err := s.Keyring.Seal(c.ClientSecret)
		if err != nil {
			return err
		}
		c.ClientSecret = stored
	}
	return s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("org_id = ?", c.OrgID).Delete(&SSOConnection{}).Error; err != nil {
			return err
		}
		what := "off"
		if c.Issuer != "" {
			// Select, so that a false isn't left to the column's default.
			if err := tx.Select("*").Create(c).Error; err != nil {
				return err
			}
			what = fmt.Sprintf("%s, required: %t, joining: %t", c.Issuer, c.Required, c.AutoJoin)
		}
		return s.Log(tx, c.OrgID, 0, nil, actor, "sso:change", &what)
	})
}

// RequiresSSO is one read, as most organisations require nothing.
func (s *ssoStore) RequiresSSO(org int64) (bool, error) {
	var c SSOConnection
	err := s.InOrg(org).Limit(1).Find(&c).Error
	return c.Required, err
}

func (s *ssoStore) AddSSODomain(org int64, domain string) (*SSODomain, error) {
	var n int64
	if err := s.InOrg(org).Model(&SSODomain{}).Count(&n).Error; err != nil {
		return nil, err
	}
	if n >= maxSSODomains {
		return nil, say.Errorf("an organisation can have %d domains", maxSSODomains)
	}
	d := &SSODomain{OrgID: org, Domain: domain, Token: randomToken(20)}
	if err := s.DB.Create(d).Error; err != nil {
		if breaksRule(err) {
			err = conflict{say.Errorf("%s is already listed", domain)}
		}
		return nil, err
	}
	return d, nil
}

func (s *ssoStore) SSODomain(org int64, domain string) (*SSODomain, error) {
	var d SSODomain
	if err := s.InOrg(org).Where("domain = ?", domain).First(&d).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		return nil, say.New("that domain isn't listed")
	}
	return &d, nil
}

func (s *ssoStore) VerifySSODomain(org, id int64, at time.Time) error {
	return s.InOrg(org).Model(&SSODomain{}).Where("id = ?", id).Update("verified_at", &at).Error
}

func (s *ssoStore) RemoveSSODomain(org int64, domain string) error {
	return s.InOrg(org).Where("domain = ?", domain).Delete(&SSODomain{}).Error
}

// SSOOrgsFor is the organisations whose own sign-in speaks for an email domain.
func (s *ssoStore) SSOOrgsFor(domain string) ([]Org, error) {
	var out []Org
	if domain == "" {
		return nil, nil
	}
	err := s.DB.Table("orgs").Select("orgs.*").
		Joins("join sso_domains on sso_domains.org_id = orgs.id and sso_domains.verified_at is not null").
		Joins("join sso_connections on sso_connections.org_id = orgs.id and sso_connections.issuer <> ''").
		Where("sso_domains.domain = ?", domain).Order("orgs.name").Scan(&out).Error
	return out, err
}

// SocialUser is the account a social sign-in belongs to. It never makes one.
func (s *ssoStore) SocialUser(who *outsider) (*User, error) {
	var u *User
	err := s.DB.Transaction(func(tx *gorm.DB) (err error) {
		if u, err = resolve(tx, who, who.Email, who.Vouched, nil); err != nil {
			return err
		}
		return claim(tx, u)
	})
	return u, err
}

// SSOUser is the member an organisation's own sign-in has let in, made a member on
// the way if the organisation lets people join. Nobody becomes an owner like this.
func (s *ssoStore) SSOUser(org *Org, c *SSO, who *outsider, email string) (*User, error) {
	var u *User
	err := s.DB.Transaction(func(tx *gorm.DB) (err error) {
		var create func() (*User, error)
		if c.AutoJoin {
			create = func() (*User, error) {
				name := who.Name
				if name == "" {
					name, _, _ = strings.Cut(email, "@")
				}
				made := &User{Email: email, Name: name, PasswordHash: unusableHash()}
				return made, tx.Create(made).Error
			}
		}
		if u, err = resolve(tx, who, email, c.Proves(email), create); err != nil {
			return err
		}
		if !c.Proves(u.Email) {
			return errElsewhere
		}
		if err := claim(tx, u); err != nil {
			return err
		}
		var n int64
		if err := tx.Model(&Membership{}).Where("org_id = ? and user_id = ?", org.ID, u.ID).Count(&n).Error; err != nil || n > 0 {
			return err
		}
		if !c.AutoJoin {
			return errNotMember
		}
		if err := tx.Create(&Membership{OrgID: org.ID, UserID: u.ID, Role: "member", CallerName: u.Name}).Error; err != nil {
			return err
		}
		return s.Log(tx, org.ID, 0, nil, "sso", "member:join", &u.Email)
	})
	return u, err
}
