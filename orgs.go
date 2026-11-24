package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const sessionLife = 30 * 24 * time.Hour

var (
	slugRe    = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)
	notSlug   = regexp.MustCompile(`[^a-z0-9]+`)
	colorRe   = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	orgColors = []string{"#56c5f4", "#f5c66b", "#7fe0a8", "#ff9a9a", "#c4a7ff"}
)

func randomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func hashToken(t string) string {
	h := sha256.Sum256([]byte(t))
	return hex.EncodeToString(h[:])
}

func slugify(name string) string {
	s := strings.Trim(notSlug.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(s) > 40 {
		s = strings.Trim(s[:40], "-")
	}
	return s
}

func (s *Store) CreateUser(email, name, password string) (*User, error) {
	return s.newUser(email, name, password, false)
}

// InviteUser creates an account whose first password the inviter has seen, so its
// owner is made to choose their own before doing anything else.
func (s *Store) InviteUser(email, name, password string) (*User, error) {
	return s.newUser(email, name, password, true)
}

func (s *Store) newUser(email, name, password string, mustChange bool) (*User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if !strings.Contains(email, "@") {
		return nil, fmt.Errorf("%q isn't an email address", email)
	}
	if len(password) < 10 {
		return nil, errors.New("passwords need at least 10 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	u := &User{Email: email, Name: strings.TrimSpace(name), PasswordHash: string(hash), MustChangePassword: mustChange}
	return u, s.db.Create(u).Error
}

func (s *Store) SetPassword(email, password string) error {
	if len(password) < 10 {
		return errors.New("passwords need at least 10 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	u, err := s.UserByEmail(email)
	if err != nil {
		return fmt.Errorf("no user %s", email)
	}
	return s.savePassword(u.ID, string(hash), "")
}

var errWrongCurrent = errors.New("the current password is wrong")

// ChangePassword is a signed-in user changing their own; keepToken is the session doing it.
func (s *Store) ChangePassword(u *User, current, password, keepToken string) error {
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(current)) != nil {
		return errWrongCurrent
	}
	if len(password) < 10 {
		return errors.New("passwords need at least 10 characters")
	}
	if password == current {
		return errors.New("choose a password different from the current one")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return s.savePassword(u.ID, string(hash), keepToken)
}

// savePassword also signs the user out everywhere except the session in keepToken.
func (s *Store) savePassword(user int64, hash, keepToken string) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		set := map[string]any{"password_hash": hash, "must_change_password": false}
		if err := tx.Model(&User{}).Where("id = ?", user).Updates(set).Error; err != nil {
			return err
		}
		return tx.Where("user_id = ? and token_hash <> ?", user, hashToken(keepToken)).Delete(&Session{}).Error
	})
}

func (s *Store) UserByEmail(email string) (*User, error) {
	var u User
	return &u, s.db.Where("email = ?", strings.ToLower(strings.TrimSpace(email))).First(&u).Error
}

// CreateOrg makes an organisation nobody belongs to yet, for the CLI to add members to.
func (s *Store) CreateOrg(slug, name string) (*Org, error) {
	return createOrg(s.db, slug, name)
}

// CreateOrgWithOwner makes the organisation and its first owner together, so a failure
// can't leave one nobody can get into.
func (s *Store) CreateOrgWithOwner(slug, name string, owner *User) (*Org, error) {
	var o *Org
	err := s.db.Transaction(func(tx *gorm.DB) error {
		var err error
		if o, err = createOrg(tx, slug, name); err != nil {
			return err
		}
		return tx.Create(&Membership{OrgID: o.ID, UserID: owner.ID, Role: "owner", CallerName: owner.Name}).Error
	})
	if err != nil {
		return nil, err
	}
	return o, nil
}

func createOrg(tx *gorm.DB, slug, name string) (*Org, error) {
	if slug == "" {
		slug = slugify(name)
	}
	if !slugRe.MatchString(slug) {
		return nil, fmt.Errorf("%q can't be used in a URL: use a-z, 0-9 and -", slug)
	}
	var n int64
	tx.Model(&Org{}).Count(&n)
	o := &Org{Slug: slug, Name: strings.TrimSpace(name), Color: orgColors[int(n)%len(orgColors)]}
	return o, tx.Create(o).Error
}

func (s *Store) OrgByID(id int64) (*Org, error) {
	var o Org
	return &o, s.db.First(&o, id).Error
}

func (s *Store) OrgBySlug(slug string) (*Org, error) {
	var o Org
	return &o, s.db.Where("slug = ?", slug).First(&o).Error
}

var errLastOwner = errors.New("an organisation needs an owner: make someone else an owner first")

// AddMember adds a user to an organisation, or changes the role of one already in it.
func (s *Store) AddMember(org, user int64, role, callerName string) error {
	if role != "owner" && role != "member" {
		return fmt.Errorf("a role is owner or member, not %q", role)
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := lockOrg(tx, org); err != nil {
			return err
		}
		var m Membership
		err := tx.Where("org_id = ? and user_id = ?", org, user).First(&m).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return tx.Create(&Membership{OrgID: org, UserID: user, Role: role, CallerName: callerName}).Error
		}
		if err != nil {
			return err
		}
		if m.Role == "owner" && role != "owner" && lastOwner(tx, org) {
			return errLastOwner
		}
		return tx.Model(&m).Update("role", role).Error
	})
}

// lockOrg makes changes to one organisation's members take turns, so two owners
// removing each other at the same moment can't both count the other as remaining.
func lockOrg(tx *gorm.DB, org int64) error {
	return tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&Org{}, org).Error
}

func lastOwner(tx *gorm.DB, org int64) bool {
	var owners int64
	tx.Model(&Membership{}).Where("org_id = ? and role = ?", org, "owner").Count(&owners)
	return owners <= 1
}

// RemoveMember takes a user out of an organisation and returns who it was. Their
// account, and anything they added, stays.
func (s *Store) RemoveMember(org, user int64, actor string) (*User, error) {
	var u User
	return &u, s.db.Transaction(func(tx *gorm.DB) error {
		if err := lockOrg(tx, org); err != nil {
			return err
		}
		var m Membership
		if err := tx.Where("org_id = ? and user_id = ?", org, user).First(&m).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("that person isn't in this organisation")
		} else if err != nil {
			return err
		}
		if m.Role == "owner" && lastOwner(tx, org) {
			return errLastOwner
		}
		if err := tx.First(&u, user).Error; err != nil {
			return err
		}
		if err := tx.Delete(&m).Error; err != nil {
			return err
		}
		return s.log(tx, org, 0, nil, actor, "member:remove", &u.Email)
	})
}

type OrgMembership struct {
	Org
	Role string
}

func (s *Store) OrgsFor(user int64) ([]OrgMembership, error) {
	var out []OrgMembership
	err := s.db.Table("orgs").Select("orgs.*, memberships.role").
		Joins("join memberships on memberships.org_id = orgs.id").
		Where("memberships.user_id = ?", user).Order("orgs.name").Scan(&out).Error
	return out, err
}

func (s *Store) Membership(org, user int64) (*Membership, error) {
	var m Membership
	return &m, s.db.Where("org_id = ? and user_id = ?", org, user).First(&m).Error
}

type Member struct {
	Membership
	Email, Name string
}

func (s *Store) Members(org int64) ([]Member, error) {
	var out []Member
	err := s.db.Table("memberships").Select("memberships.*, users.email, users.name").
		Joins("join users on users.id = memberships.user_id").
		Where("memberships.org_id = ?", org).Order("users.name").Scan(&out).Error
	return out, err
}

var errWrongLogin = errors.New("wrong email or password")

func (s *Store) Login(email, password string) (string, *User, error) {
	u, err := s.UserByEmail(email)
	if err != nil {
		bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return "", nil, errWrongLogin
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		return "", nil, errWrongLogin
	}
	token, err := s.NewSession(u.ID, 0)
	return token, u, err
}

// NewSession signs a user in. One that began at an organisation's own sign-in is
// limited to that organisation: its provider vouches for nothing else.
func (s *Store) NewSession(user, ssoOrg int64) (string, error) {
	token := randomToken(32)
	sess := &Session{TokenHash: hashToken(token), UserID: user, ExpiresAt: time.Now().Add(sessionLife)}
	if ssoOrg != 0 {
		sess.Limited, sess.SSOOrgs = true, fmt.Sprintf(",%d,", ssoOrg)
	}
	return token, s.db.Create(sess).Error
}

func (sess *Session) Proves(org int64) bool {
	return strings.Contains(sess.SSOOrgs, fmt.Sprintf(",%d,", org))
}

func (sess *Session) Opens(org int64) bool { return !sess.Limited || sess.Proves(org) }

// AddProof records that a session already open has now been through org's own sign-in too.
func (s *Store) AddProof(sess *Session, org int64) error {
	if sess.Proves(org) {
		return nil
	}
	orgs := sess.SSOOrgs
	if orgs == "" {
		orgs = ","
	}
	sess.SSOOrgs = orgs + fmt.Sprintf("%d,", org)
	return s.db.Model(sess).Update("sso_orgs", sess.SSOOrgs).Error
}

func (s *Store) SessionFor(token string) (*Session, *User, error) {
	var sess Session
	if err := s.db.Where("token_hash = ? and expires_at > ?", hashToken(token), time.Now()).First(&sess).Error; err != nil {
		return nil, nil, err
	}
	var u User
	return &sess, &u, s.db.First(&u, sess.UserID).Error
}

func (s *Store) SessionUser(token string) (*User, error) {
	_, u, err := s.SessionFor(token)
	return u, err
}

func (s *Store) Logout(token string) error {
	return s.db.Where("token_hash = ?", hashToken(token)).Delete(&Session{}).Error
}

// Scopes an API key can carry. None of them can send, change settings or reach another org; only import skips review.
var Scopes = []struct{ Name, What string }{
	{"read", "search companies and people, read what's due"},
	{"research:write", "add companies, facts and suggested contacts (they wait in Research)"},
	{"drafts:write", "write draft notes and emails, marked as written by the agent"},
	{"outcomes:write", "record what happened (added, accepted, sent, called, replied, declined, hold), shown as the agent's"},
	{"import", "push the repo's markdown and pull the snapshot"},
}

func validScope(s string) bool {
	for _, sc := range Scopes {
		if sc.Name == s {
			return true
		}
	}
	return false
}

func (k *APIKey) Has(scope string) bool {
	for _, s := range strings.Split(k.Scopes, " ") {
		if s == scope {
			return true
		}
	}
	return false
}

// CreateKey returns the secret once; only its hash is stored.
func (s *Store) CreateKey(org int64, name string, scopes []string, by string) (string, *APIKey, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", nil, errors.New("give the key a name")
	}
	for _, sc := range scopes {
		if !validScope(sc) {
			return "", nil, fmt.Errorf("unknown scope %q", sc)
		}
	}
	if len(scopes) == 0 {
		return "", nil, errors.New("pick at least one scope")
	}
	secret := "pk_" + randomToken(24)
	k := &APIKey{OrgID: org, Name: name, Prefix: secret[:7], Hash: hashToken(secret), Scopes: strings.Join(scopes, " "), CreatedBy: by}
	return secret, k, s.db.Create(k).Error
}

func (s *Store) KeyFor(secret string) (*APIKey, error) {
	var k APIKey
	if err := s.db.Where("hash = ? and revoked_at is null", hashToken(secret)).First(&k).Error; err != nil {
		return nil, err
	}
	now := time.Now()
	s.db.Model(&k).Update("last_used_at", now)
	return &k, nil
}

func (s *Store) Keys(org int64) ([]APIKey, error) {
	var keys []APIKey
	return keys, s.in(org).Where("revoked_at is null").Order("created_at desc").Find(&keys).Error
}

func (s *Store) RevokeKey(org, id int64) error {
	res := s.in(org).Model(&APIKey{}).Where("id = ?", id).Update("revoked_at", time.Now())
	if res.Error == nil && res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return res.Error
}

func (s *Store) SaveOrg(o *Org) error {
	if !colorRe.MatchString(o.Color) {
		return fmt.Errorf("colour must look like #56c5f4")
	}
	return s.db.Model(o).Select("name", "color", "settings").Updates(o).Error
}

func (s *Store) SaveVoice(org, user int64, caller, signature string) error {
	return s.db.Model(&Membership{}).Where("org_id = ? and user_id = ?", org, user).
		Updates(map[string]any{"caller_name": strings.TrimSpace(caller), "signature": strings.TrimSpace(signature)}).Error
}
