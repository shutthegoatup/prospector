package main

import (
	"crypto/hmac"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// identityStore is the identity area's queries: people, organisations and who belongs to which, sessions, API keys and password resets.
type identityStore struct{ *core }

func (s *identityStore) CreateUser(email, name, password string) (*User, error) {
	return s.newUser(email, name, password, false)
}

// InviteUser creates an account whose first password the inviter has seen, so its
// owner is made to choose their own before doing anything else.
func (s *identityStore) InviteUser(email, name, password string) (*User, error) {
	return s.newUser(email, name, password, true)
}

func (s *identityStore) newUser(email, name, password string, mustChange bool) (*User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if !strings.Contains(email, "@") {
		return nil, fmt.Errorf("%q isn't an email address", email)
	}
	if len(password) < 10 {
		return nil, errors.New("passwords need at least 10 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), passwordCost)
	if err != nil {
		return nil, err
	}
	u := &User{Email: email, Name: strings.TrimSpace(name), PasswordHash: string(hash), MustChangePassword: mustChange}
	return u, s.DB.Create(u).Error
}

func (s *identityStore) SetPassword(email, password string) error {
	if len(password) < 10 {
		return errors.New("passwords need at least 10 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), passwordCost)
	if err != nil {
		return err
	}
	u, err := s.UserByEmail(email)
	if err != nil {
		return fmt.Errorf("no user %s", email)
	}
	return s.savePassword(u.ID, string(hash), "")
}

// ChangePassword is a signed-in user changing their own; keepToken is the session doing it.
func (s *identityStore) ChangePassword(u *User, current, password, keepToken string) error {
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(current)) != nil {
		return errWrongCurrent
	}
	if len(password) < 10 {
		return errors.New("passwords need at least 10 characters")
	}
	if password == current {
		return errors.New("choose a password different from the current one")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), passwordCost)
	if err != nil {
		return err
	}
	return s.savePassword(u.ID, string(hash), keepToken)
}

// savePassword also signs the user out everywhere except the session in keepToken.
func (s *identityStore) savePassword(user int64, hash, keepToken string) error {
	return s.DB.Transaction(func(tx *gorm.DB) error { return s.savePasswordIn(tx, user, hash, keepToken) })
}

// A new password also ends any emailed link that could still have set one.
func (s *identityStore) savePasswordIn(tx *gorm.DB, user int64, hash, keepToken string) error {
	set := map[string]any{"password_hash": hash, "must_change_password": false}
	if err := tx.Model(&User{}).Where("id = ?", user).Updates(set).Error; err != nil {
		return err
	}
	if err := tx.Where("user_id = ?", user).Delete(&PasswordToken{}).Error; err != nil {
		return err
	}
	return tx.Where("user_id = ? and token_hash not in ?", user, s.Keyring.Lookups(sessions, keepToken)).Delete(&Session{}).Error
}

func (s *identityStore) UserByEmail(email string) (*User, error) {
	var u User
	return &u, s.DB.Where("email = ?", strings.ToLower(strings.TrimSpace(email))).First(&u).Error
}

// CreateOrg makes an organisation nobody belongs to yet, for the CLI to add members to.
func (s *identityStore) CreateOrg(slug, name string) (*Org, error) {
	return createOrg(s.DB, slug, name)
}

// EnsureFrontOrg makes that organisation on a server that hasn't one. A new one takes
// enquiries from the server's own address when that is known; from then on its sites
// are its owners' to change, so an existing one is left as it is.
func (s *identityStore) EnsureFrontOrg(base string) error {
	if _, err := s.OrgBySlug(frontOrgSlug); !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	// Made and given its site together: one left without would never be given it.
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		o, err := createOrg(tx, frontOrgSlug, "Prospector")
		if err != nil || base == "" {
			return err
		}
		o.Settings.EnquirySites = browserOrigin(base)
		return tx.Model(o).Select("settings").Updates(o).Error
	})
	if err != nil {
		// Another server on the same database may have just made it.
		if _, found := s.OrgBySlug(frontOrgSlug); found == nil {
			return nil
		}
	}
	return err
}

// FrontFormOff says why the front page will have no contact form at base, the address
// people reach the server at, or nothing when it will have one.
func (s *identityStore) FrontFormOff(base string) string {
	o, err := s.OrgBySlug(frontOrgSlug)
	if err != nil {
		return err.Error()
	}
	sites, _ := enquiryOrigins(o.Settings.EnquirySites)
	listed := "lists no enquiry sites"
	if len(sites) > 0 {
		listed = "lists only " + strings.Join(sites, ", ")
	}
	fix := fmt.Sprintf("add yourself with `prospector member add %s <email> owner` and list the address in its Settings", frontOrgSlug)
	if base == "" && len(sites) > 0 {
		return "" // Whether a request's own address is among them is only known as it arrives.
	}
	if base == "" {
		// Setting PROSPECTOR_URL now wouldn't list it: the organisation is already made.
		return fmt.Sprintf("PROSPECTOR_URL isn't set, so the form shows only at an address the %q organisation lists, and it %s; %s",
			frontOrgSlug, listed, fix)
	}
	if !o.Settings.TakesEnquiriesFrom(browserOrigin(base)) {
		return fmt.Sprintf("the %q organisation %s, not %s; %s", frontOrgSlug, listed, browserOrigin(base), fix)
	}
	return ""
}

// CreateOrgWithOwner makes the organisation and its first owner together, so a failure
// can't leave one nobody can get into.
func (s *identityStore) CreateOrgWithOwner(slug, name string, owner *User) (*Org, error) {
	var o *Org
	err := s.DB.Transaction(func(tx *gorm.DB) error {
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

func (s *identityStore) OrgByID(id int64) (*Org, error) {
	var o Org
	return &o, s.DB.First(&o, id).Error
}

func (s *identityStore) OrgBySlug(slug string) (*Org, error) {
	var o Org
	return &o, s.DB.Where("slug = ?", slug).First(&o).Error
}

// AddMember adds a user to an organisation, or changes the role of one already in it.
func (s *identityStore) AddMember(org, user int64, role, callerName string) error {
	if role != "owner" && role != "member" {
		return fmt.Errorf("a role is owner or member, not %q", role)
	}
	return s.DB.Transaction(func(tx *gorm.DB) error {
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
		return setRole(tx, org, &m, role)
	})
}

// SetRole changes the role of someone already in the organisation.
func (s *identityStore) SetRole(org, user int64, role string) error {
	if role != "owner" && role != "member" {
		return fmt.Errorf("a role is owner or member, not %q", role)
	}
	return s.DB.Transaction(func(tx *gorm.DB) error {
		if err := lockOrg(tx, org); err != nil {
			return err
		}
		var m Membership
		if err := tx.Where("org_id = ? and user_id = ?", org, user).First(&m).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("that person isn't in this organisation")
		} else if err != nil {
			return err
		}
		return setRole(tx, org, &m, role)
	})
}

// RemoveMember takes a user out of an organisation and returns who it was. Their
// account, and anything they added, stays.
func (s *identityStore) RemoveMember(org, user int64, actor string) (*User, error) {
	var u User
	return &u, s.DB.Transaction(func(tx *gorm.DB) error {
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
		if err := dropRecipients(tx, tx.Where("org_id = ? and user_id = ?", org, user)); err != nil {
			return err
		}
		return s.Log(tx, org, 0, nil, actor, "member:remove", &u.Email)
	})
}

func (s *identityStore) OrgsFor(user int64) ([]OrgMembership, error) {
	var out []OrgMembership
	err := s.DB.Table("orgs").Select("orgs.*, memberships.role").
		Joins("join memberships on memberships.org_id = orgs.id").
		Where("memberships.user_id = ?", user).Order("orgs.name").Scan(&out).Error
	return out, err
}

func (s *identityStore) Membership(org, user int64) (*Membership, error) {
	var m Membership
	return &m, s.DB.Where("org_id = ? and user_id = ?", org, user).First(&m).Error
}

func (s *identityStore) Members(org int64) ([]Member, error) {
	var out []Member
	err := s.DB.Table("memberships").Select("memberships.*, users.email, users.name").
		Joins("join users on users.id = memberships.user_id").
		Where("memberships.org_id = ?", org).Order("users.name").Scan(&out).Error
	return out, err
}

func (s *identityStore) Login(email, password string) (string, *User, error) {
	u, err := s.UserByEmail(email)
	if err != nil {
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		// An address the database won't even compare, such as one holding a NUL, is no
		// account's. Any other failed look-up says nothing about the password.
		if !errors.Is(err, gorm.ErrRecordNotFound) && !unstorable(err) {
			return "", nil, err
		}
		return "", nil, errWrongLogin
	}
	// An account with no password takes as long to refuse as any other.
	if !u.HasPassword() {
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
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
func (s *identityStore) NewSession(user, ssoOrg int64) (string, error) {
	token := randomToken(32)
	sess := &Session{TokenHash: s.Keyring.Lookup(sessions, token), UserID: user, ExpiresAt: time.Now().Add(sessionLife)}
	if ssoOrg != 0 {
		sess.Limited = true
		sess.Prove(ssoOrg, time.Now())
	}
	return token, s.DB.Create(sess).Error
}

// AddProof records that a session already open has now been through org's own sign-in too.
func (s *identityStore) AddProof(sess *Session, org int64) error {
	sess.Prove(org, time.Now())
	return s.DB.Model(sess).Update("sso_orgs", sess.SSOOrgs).Error
}

func (s *identityStore) SessionFor(token string) (*Session, *User, error) {
	var sess Session
	if err := s.DB.Where("token_hash in ? and expires_at > ?", s.Keyring.Lookups(sessions, token), time.Now()).First(&sess).Error; err != nil {
		return nil, nil, err
	}
	// Found by an older key: moved to the current one, so that key can be removed. One
	// under a newer key is another server's doing and is left alone.
	if s.Keyring.LookupBehind(sess.TokenHash) {
		current := s.Keyring.Lookup(sessions, token)
		if s.DB.Model(&sess).Update("token_hash", current).Error == nil {
			sess.TokenHash = current
		}
	}
	var u User
	return &sess, &u, s.DB.First(&u, sess.UserID).Error
}

func (s *identityStore) SessionUser(token string) (*User, error) {
	_, u, err := s.SessionFor(token)
	return u, err
}

func (s *identityStore) Logout(token string) error {
	return s.DB.Where("token_hash in ?", s.Keyring.Lookups(sessions, token)).Delete(&Session{}).Error
}

// CreateKey returns the secret once; only its hash is stored.
func (s *identityStore) CreateKey(org int64, name string, scopes []string, by string) (string, *APIKey, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", nil, errors.New("give the key a name")
	}
	if err := checkScopes(scopes); err != nil {
		return "", nil, err
	}
	secret := "pk_" + randomToken(24)
	k := &APIKey{OrgID: org, Name: name, Prefix: secret[:7], Hash: s.Keyring.Lookup(apiKeys, secret), Scopes: strings.Join(scopes, " "), CreatedBy: by}
	return secret, k, s.DB.Create(k).Error
}

func (s *identityStore) KeyFor(secret string) (*APIKey, error) {
	var k APIKey
	// A key made before there was a secret key is still found by its plain hash: an
	// agent can't be asked to sign in again the way a person can. It moves on first
	// use. The cost is that a row written straight into the database with a plain hash
	// is taken for one of those, so the keyed lookup guards sessions and not API keys.
	found := append(s.Keyring.Lookups(apiKeys, secret), hashToken(secret))
	if err := s.DB.Where("hash in ? and revoked_at is null", found).First(&k).Error; err != nil {
		return nil, err
	}
	now := time.Now()
	set := map[string]any{}
	// Not every call: a key busy all day would make every read a write, and the minute
	// is as near as its last use is ever shown.
	if k.LastUsedAt == nil || now.Sub(*k.LastUsedAt) >= time.Minute {
		set["last_used_at"] = now
	}
	if s.Keyring.LookupBehind(k.Hash) {
		set["hash"] = s.Keyring.Lookup(apiKeys, secret)
	}
	if len(set) > 0 {
		_ = logged(s.DB.Model(&k).Updates(set))
	}
	return &k, nil
}

func (s *identityStore) Keys(org int64) ([]APIKey, error) {
	var keys []APIKey
	return keys, s.InOrg(org).Where("revoked_at is null").Order("created_at desc").Find(&keys).Error
}

// SetKeyScopes changes what a key may do without changing its secret.
func (s *identityStore) SetKeyScopes(org, id int64, scopes []string) error {
	if err := checkScopes(scopes); err != nil {
		return err
	}
	var k APIKey
	if err := s.InOrg(org).Where("id = ? and revoked_at is null", id).First(&k).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return errors.New("that key isn't in this organisation, or has been revoked")
	} else if err != nil {
		return err
	}
	if k.Prefix == builtinPrefix {
		return errBuiltinKey
	}
	// In a fixed order, so the same scopes resent don't end the key's open sessions.
	var ordered []string
	for _, sc := range Scopes {
		if slices.Contains(scopes, sc.Name) {
			ordered = append(ordered, sc.Name)
		}
	}
	return s.InOrg(org).Model(&APIKey{}).Where("id = ? and revoked_at is null", id).Update("scopes", strings.Join(ordered, " ")).Error
}

func (s *identityStore) RevokeKey(org, id int64) error {
	res := s.InOrg(org).Model(&APIKey{}).Where("id = ?", id).Update("revoked_at", time.Now())
	if res.Error == nil && res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return res.Error
}

func (s *identityStore) SaveOrg(o *Org) error {
	if !colorRe.MatchString(o.Color) {
		return fmt.Errorf("colour must look like #56c5f4")
	}
	return s.DB.Model(o).Select("name", "color", "settings").Updates(o).Error
}

// ChangeSettings saves o and notes in its history who changed which settings, since a
// key can now empty the never-contact list or open the enquiry route to a site.
func (s *identityStore) ChangeSettings(o *Org, who Actor, changed []string) error {
	if len(changed) == 0 {
		return nil
	}
	if !colorRe.MatchString(o.Color) {
		return fmt.Errorf("colour must look like #56c5f4")
	}
	what := strings.Join(changed, ", ")
	return s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(o).Select("name", "color", "settings").Updates(o).Error; err != nil {
			return err
		}
		return s.LogAs(tx, o.ID, 0, nil, who, "settings:change", &what)
	})
}

func (s *identityStore) SaveVoice(org, user int64, caller, signature string) error {
	return s.DB.Model(&Membership{}).Where("org_id = ? and user_id = ?", org, user).
		Updates(map[string]any{"caller_name": strings.TrimSpace(caller), "signature": strings.TrimSpace(signature)}).Error
}

// KnownDevice reports whether token was issued to this email's account, and if so
// the name its attempts are counted under: the email and the device together, so
// guesses made elsewhere don't hold this browser up, and its own are still slowed.
func (s *identityStore) KnownDevice(email, token string) (who string, ok bool) {
	nonce, signature, found := strings.Cut(token, ".")
	if !found || len(token) > 200 {
		return "", false
	}
	u, err := s.UserByEmail(email)
	if err != nil || !hmac.Equal([]byte(signature), []byte(deviceMAC(u, nonce))) {
		return "", false
	}
	return signInBucket(u.Email) + "\x00" + nonce, true
}

// Users is how many people can sign in, in any organisation.
func (s *identityStore) Users() (int64, error) {
	var n int64
	return n, s.DB.Model(&User{}).Count(&n).Error
}

// SetAreas says what someone works on. None leaves them reading only.
func (s *identityStore) SetAreas(org, user int64, areas []string) error {
	for _, ar := range areas {
		if ar != areaSales && ar != areaSupport {
			return fmt.Errorf("unknown area %q", ar)
		}
	}
	var keep []string
	for _, ar := range Areas {
		if slices.Contains(areas, ar.Name) {
			keep = append(keep, ar.Name)
		}
	}
	res := s.DB.Model(&Membership{}).Where("org_id = ? and user_id = ?", org, user).Update("areas", strings.Join(keep, " "))
	if res.Error == nil && res.RowsAffected == 0 {
		return errors.New("that person isn't in this organisation")
	}
	return res.Error
}
