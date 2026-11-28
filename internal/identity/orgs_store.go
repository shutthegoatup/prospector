package identity

import (
	"crypto/hmac"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/shutthegoatup/prospector/internal/say"
	"github.com/shutthegoatup/prospector/internal/store"
)

// IdentityStore is the identity area's queries: people, organisations and who belongs to which, sessions, API keys and password resets.
type IdentityStore struct{ *store.Core }

func (s *IdentityStore) CreateUser(email, name, password string) (*store.User, error) {
	return s.newUser(email, name, password, false)
}

// InviteUser creates an account whose first password the inviter has seen, so its
// owner is made to choose their own before doing anything else.
func (s *IdentityStore) InviteUser(email, name, password string) (*store.User, error) {
	return s.newUser(email, name, password, true)
}

func (s *IdentityStore) newUser(email, name, password string, mustChange bool) (*store.User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if !strings.Contains(email, "@") {
		return nil, say.Errorf("%q isn't an email address", email)
	}
	if len(password) < 10 {
		return nil, say.New("passwords need at least 10 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), PasswordCost)
	if err != nil {
		return nil, err
	}
	u := &store.User{Email: email, Name: strings.TrimSpace(name), PasswordHash: string(hash), MustChangePassword: mustChange}
	return u, s.DB.Create(u).Error
}

func (s *IdentityStore) SetPassword(email, password string) error {
	if len(password) < 10 {
		return say.New("passwords need at least 10 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), PasswordCost)
	if err != nil {
		return err
	}
	u, err := s.UserByEmail(email)
	if err != nil {
		return say.Errorf("no user %s", email)
	}
	return s.savePassword(u.ID, string(hash), "")
}

// ChangePassword is a signed-in user changing their own; keepToken is the session doing it.
func (s *IdentityStore) ChangePassword(u *store.User, current, password, keepToken string) error {
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(current)) != nil {
		return ErrWrongCurrent
	}
	if len(password) < 10 {
		return say.New("passwords need at least 10 characters")
	}
	if password == current {
		return say.New("choose a password different from the current one")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), PasswordCost)
	if err != nil {
		return err
	}
	return s.savePassword(u.ID, string(hash), keepToken)
}

// savePassword also signs the user out everywhere except the session in keepToken.
func (s *IdentityStore) savePassword(user int64, hash, keepToken string) error {
	return s.DB.Transaction(func(tx *gorm.DB) error { return s.savePasswordIn(tx, user, hash, keepToken) })
}

// A new password also ends any emailed link that could still have set one.
func (s *IdentityStore) savePasswordIn(tx *gorm.DB, user int64, hash, keepToken string) error {
	set := map[string]any{"password_hash": hash, "must_change_password": false}
	if err := tx.Model(&store.User{}).Where("id = ?", user).Updates(set).Error; err != nil {
		return err
	}
	if err := tx.Where("user_id = ?", user).Delete(&store.PasswordToken{}).Error; err != nil {
		return err
	}
	return tx.Where("user_id = ? and token_hash not in ?", user, s.Keyring.Lookups(Sessions, keepToken)).Delete(&store.Session{}).Error
}

func (s *IdentityStore) UserByEmail(email string) (*store.User, error) {
	var u store.User
	return &u, s.DB.Where("email = ?", strings.ToLower(strings.TrimSpace(email))).First(&u).Error
}

// CreateOrg makes an organisation nobody belongs to yet, for the CLI to add members to.
func (s *IdentityStore) CreateOrg(slug, name string) (*store.Org, error) {
	return createOrg(s.DB, slug, name)
}

// EnsureFrontOrg makes that organisation on a server that hasn't one. A new one takes
// enquiries from the server's own address when that is known; from then on its sites
// are its owners' to change, so an existing one is left as it is.
func (s *IdentityStore) EnsureFrontOrg(base string) error {
	if _, err := s.OrgBySlug(FrontOrgSlug); !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	// Made and given its site together: one left without would never be given it.
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		o, err := createOrg(tx, FrontOrgSlug, "Prospector")
		if err != nil || base == "" {
			return err
		}
		o.Settings.EnquirySites = BrowserOrigin(base)
		return tx.Model(o).Select("settings").Updates(o).Error
	})
	if err != nil {
		// Another server on the same database may have just made it.
		if _, found := s.OrgBySlug(FrontOrgSlug); found == nil {
			return nil
		}
	}
	return err
}

// FrontFormOff says why the front page will have no contact form at base, the address
// people reach the server at, or nothing when it will have one.
func (s *IdentityStore) FrontFormOff(base string) string {
	o, err := s.OrgBySlug(FrontOrgSlug)
	if err != nil {
		return err.Error()
	}
	sites, _ := store.EnquiryOrigins(o.Settings.EnquirySites)
	listed := "lists no enquiry sites"
	if len(sites) > 0 {
		listed = "lists only " + strings.Join(sites, ", ")
	}
	fix := fmt.Sprintf("add yourself with `prospector member add %s <email> owner` and list the address in its Settings", FrontOrgSlug)
	if base == "" && len(sites) > 0 {
		return "" // Whether a request's own address is among them is only known as it arrives.
	}
	if base == "" {
		// Setting PROSPECTOR_URL now wouldn't list it: the organisation is already made.
		return fmt.Sprintf("PROSPECTOR_URL isn't set, so the form shows only at an address the %q organisation lists, and it %s; %s",
			FrontOrgSlug, listed, fix)
	}
	if !o.Settings.TakesEnquiriesFrom(BrowserOrigin(base)) {
		return fmt.Sprintf("the %q organisation %s, not %s; %s", FrontOrgSlug, listed, BrowserOrigin(base), fix)
	}
	return ""
}

// CreateOrgWithOwner makes the organisation and its first owner together, so a failure
// can't leave one nobody can get into.
func (s *IdentityStore) CreateOrgWithOwner(slug, name string, owner *store.User) (*store.Org, error) {
	var o *store.Org
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		var err error
		if o, err = createOrg(tx, slug, name); err != nil {
			return err
		}
		return tx.Create(&store.Membership{OrgID: o.ID, UserID: owner.ID, Role: "owner", CallerName: owner.Name}).Error
	})
	if err != nil {
		return nil, err
	}
	return o, nil
}

func (s *IdentityStore) OrgByID(id int64) (*store.Org, error) {
	var o store.Org
	return &o, s.DB.First(&o, id).Error
}

func (s *IdentityStore) OrgBySlug(slug string) (*store.Org, error) {
	var o store.Org
	return &o, s.DB.Where("slug = ?", slug).First(&o).Error
}

// AddMember adds a user to an organisation, or changes the role of one already in it.
func (s *IdentityStore) AddMember(org, user int64, role, callerName, actor string) error {
	if role != "owner" && role != "member" {
		return say.Errorf("a role is owner or member, not %q", role)
	}
	return s.DB.Transaction(func(tx *gorm.DB) error {
		if err := lockOrg(tx, org); err != nil {
			return err
		}
		var m store.Membership
		err := tx.Where("org_id = ? and user_id = ?", org, user).First(&m).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			var u store.User
			if err := tx.First(&u, user).Error; err != nil {
				return err
			}
			if err := tx.Create(&store.Membership{OrgID: org, UserID: user, Role: role, CallerName: callerName}).Error; err != nil {
				return err
			}
			return s.Log(tx, org, 0, nil, actor, "member:add", new(u.Email+": "+role))
		}
		if err != nil {
			return err
		}
		return s.setRole(tx, org, &m, role, actor)
	})
}

// SetRole changes the role of someone already in the organisation.
func (s *IdentityStore) SetRole(org, user int64, role, actor string) error {
	if role != "owner" && role != "member" {
		return say.Errorf("a role is owner or member, not %q", role)
	}
	return s.DB.Transaction(func(tx *gorm.DB) error {
		if err := lockOrg(tx, org); err != nil {
			return err
		}
		var m store.Membership
		if err := tx.Where("org_id = ? and user_id = ?", org, user).First(&m).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return say.New("that person isn't in this organisation")
		} else if err != nil {
			return err
		}
		return s.setRole(tx, org, &m, role, actor)
	})
}

// RemoveMember takes a user out of an organisation and returns who it was. Their
// account, and anything they added, stays.
func (s *IdentityStore) RemoveMember(org, user int64, actor string) (*store.User, error) {
	var u store.User
	return &u, s.DB.Transaction(func(tx *gorm.DB) error {
		if err := lockOrg(tx, org); err != nil {
			return err
		}
		var m store.Membership
		if err := tx.Where("org_id = ? and user_id = ?", org, user).First(&m).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return say.New("that person isn't in this organisation")
		} else if err != nil {
			return err
		}
		if m.Role == "owner" && LastOwner(tx, org) {
			return errLastOwner
		}
		if err := tx.First(&u, user).Error; err != nil {
			return err
		}
		if err := tx.Delete(&m).Error; err != nil {
			return err
		}
		if err := DropRecipients(tx, tx.Where("org_id = ? and user_id = ?", org, user)); err != nil {
			return err
		}
		return s.Log(tx, org, 0, nil, actor, "member:remove", &u.Email)
	})
}

func (s *IdentityStore) OrgsFor(user int64) ([]OrgMembership, error) {
	var out []OrgMembership
	err := s.DB.Table("orgs").Select("orgs.*, memberships.role").
		Joins("join memberships on memberships.org_id = orgs.id").
		Where("memberships.user_id = ?", user).Order("orgs.name").Scan(&out).Error
	return out, err
}

func (s *IdentityStore) Membership(org, user int64) (*store.Membership, error) {
	var m store.Membership
	return &m, s.DB.Where("org_id = ? and user_id = ?", org, user).First(&m).Error
}

func (s *IdentityStore) Members(org int64) ([]Member, error) {
	var out []Member
	err := s.DB.Table("memberships").Select("memberships.*, users.email, users.name").
		Joins("join users on users.id = memberships.user_id").
		Where("memberships.org_id = ?", org).Order("users.name").Scan(&out).Error
	return out, err
}

func (s *IdentityStore) Login(email, password string) (string, *store.User, error) {
	u, err := s.UserByEmail(email)
	if err != nil {
		_ = bcrypt.CompareHashAndPassword(DummyHash, []byte(password))
		// An address the database won't even compare, such as one holding a NUL, is no
		// account's. Any other failed look-up says nothing about the password.
		if !errors.Is(err, gorm.ErrRecordNotFound) && !Unstorable(err) {
			return "", nil, err
		}
		return "", nil, ErrWrongLogin
	}
	// An account with no password takes as long to refuse as any other.
	if !u.HasPassword() {
		_ = bcrypt.CompareHashAndPassword(DummyHash, []byte(password))
		return "", nil, ErrWrongLogin
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		return "", nil, ErrWrongLogin
	}
	token, err := s.NewSession(u.ID, 0)
	return token, u, err
}

// NewSession signs a user in. One that began at an organisation's own sign-in is
// limited to that organisation: its provider vouches for nothing else.
func (s *IdentityStore) NewSession(user, ssoOrg int64) (string, error) {
	token := store.RandomToken(32)
	sess := &store.Session{TokenHash: s.Keyring.Lookup(Sessions, token), UserID: user, ExpiresAt: time.Now().Add(SessionLife)}
	if ssoOrg != 0 {
		sess.Limited = true
		sess.Prove(ssoOrg, time.Now())
	}
	_ = store.Logged(s.DB.Where("expires_at < ?", time.Now()).Delete(&store.Session{}))
	return token, s.DB.Create(sess).Error
}

// AddProof records that a session already open has now been through org's own sign-in too.
func (s *IdentityStore) AddProof(sess *store.Session, org int64) error {
	sess.Prove(org, time.Now())
	return s.DB.Model(sess).Update("sso_orgs", sess.SSOOrgs).Error
}

func (s *IdentityStore) SessionFor(token string) (*store.Session, *store.User, error) {
	var sess store.Session
	if err := s.DB.Where("token_hash in ? and expires_at > ?", s.Keyring.Lookups(Sessions, token), time.Now()).First(&sess).Error; err != nil {
		return nil, nil, err
	}
	// Found by an older key: moved to the current one, so that key can be removed. One
	// under a newer key is another server's doing and is left alone.
	if s.Keyring.LookupBehind(sess.TokenHash) {
		current := s.Keyring.Lookup(Sessions, token)
		if s.DB.Model(&sess).Update("token_hash", current).Error == nil {
			sess.TokenHash = current
		}
	}
	var u store.User
	return &sess, &u, s.DB.First(&u, sess.UserID).Error
}

func (s *IdentityStore) SessionUser(token string) (*store.User, error) {
	_, u, err := s.SessionFor(token)
	return u, err
}

func (s *IdentityStore) Logout(token string) error {
	return s.DB.Where("token_hash in ?", s.Keyring.Lookups(Sessions, token)).Delete(&store.Session{}).Error
}

// CreateKey returns the secret once; only its hash is stored.
func (s *IdentityStore) CreateKey(org int64, name string, scopes []string, by string) (string, *store.APIKey, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", nil, say.New("give the key a name")
	}
	if err := checkScopes(scopes); err != nil {
		return "", nil, err
	}
	secret := "pk_" + store.RandomToken(24)
	k := &store.APIKey{OrgID: org, Name: name, Prefix: secret[:7], Hash: s.Keyring.Lookup(ApiKeys, secret), Scopes: strings.Join(scopes, " "), CreatedBy: by}
	return secret, k, s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(k).Error; err != nil {
			return err
		}
		return s.Log(tx, org, 0, nil, by, "key:new", new(k.Name+": "+k.Scopes))
	})
}

func (s *IdentityStore) KeyFor(secret string) (*store.APIKey, error) {
	var k store.APIKey
	// A key made before there was a secret key is still found by its plain hash: an
	// agent can't be asked to sign in again the way a person can. It moves on first
	// use. The cost is that a row written straight into the database with a plain hash
	// is taken for one of those, so the keyed lookup guards sessions and not API keys.
	found := append(s.Keyring.Lookups(ApiKeys, secret), store.HashToken(secret))
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
		set["hash"] = s.Keyring.Lookup(ApiKeys, secret)
	}
	if len(set) > 0 {
		_ = store.Logged(s.DB.Model(&k).Updates(set))
	}
	return &k, nil
}

func (s *IdentityStore) Keys(org int64) ([]store.APIKey, error) {
	var keys []store.APIKey
	return keys, s.InOrg(org).Where("revoked_at is null").Order("created_at desc").Find(&keys).Error
}

// SetKeyScopes changes what a key may do without changing its secret.
func (s *IdentityStore) SetKeyScopes(org, id int64, scopes []string, actor string) error {
	if err := checkScopes(scopes); err != nil {
		return err
	}
	var k store.APIKey
	if err := s.InOrg(org).Where("id = ? and revoked_at is null", id).First(&k).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return say.New("that key isn't in this organisation, or has been revoked")
	} else if err != nil {
		return err
	}
	if k.Prefix == store.BuiltinPrefix {
		return errBuiltinKey
	}
	// In a fixed order, so the same scopes resent don't end the key's open sessions.
	var ordered []string
	for _, sc := range Scopes {
		if slices.Contains(scopes, sc.Name) {
			ordered = append(ordered, sc.Name)
		}
	}
	now := strings.Join(ordered, " ")
	if now == k.Scopes {
		return nil
	}
	return s.DB.Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&store.APIKey{}).Where("org_id = ? and id = ? and revoked_at is null", org, id).Update("scopes", now)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return say.New("that key isn't in this organisation, or has been revoked")
		}
		return s.Log(tx, org, 0, nil, actor, "key:scopes", new(k.Name+": "+now))
	})
}

func (s *IdentityStore) RevokeKey(org, id int64, actor string) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		var k store.APIKey
		res := tx.Model(&k).Clauses(clause.Returning{}).Where("org_id = ? and id = ? and revoked_at is null", org, id).Update("revoked_at", time.Now())
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			if err := tx.Where("org_id = ? and id = ?", org, id).First(&store.APIKey{}).Error; err != nil {
				return err
			}
			return nil
		}
		return s.Log(tx, org, 0, nil, actor, "key:revoke", &k.Name)
	})
}

func (s *IdentityStore) SaveOrg(o *store.Org) error {
	if !colorRe.MatchString(o.Color) {
		return say.Errorf("colour must look like #56c5f4")
	}
	return s.DB.Model(o).Select("name", "color", "settings").Updates(o).Error
}

// ChangeSettings saves o and notes in its history who changed which settings, since a
// key can now empty the never-contact list or open the enquiry route to a site.
func (s *IdentityStore) ChangeSettings(o *store.Org, who store.Actor, changed []string) error {
	if len(changed) == 0 {
		return nil
	}
	if !colorRe.MatchString(o.Color) {
		return say.Errorf("colour must look like #56c5f4")
	}
	what := strings.Join(changed, ", ")
	return s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(o).Select("name", "color", "settings").Updates(o).Error; err != nil {
			return err
		}
		return s.LogAs(tx, o.ID, 0, nil, who, "settings:change", &what)
	})
}

// SetLanguage keeps the language someone chose for themselves, in every organisation.
func (s *IdentityStore) SetLanguage(user int64, lang string) error {
	return s.DB.Model(&store.User{}).Where("id = ?", user).Update("language", lang).Error
}

func (s *IdentityStore) SaveVoice(org, user int64, caller, signature string) error {
	return s.DB.Model(&store.Membership{}).Where("org_id = ? and user_id = ?", org, user).
		Updates(map[string]any{"caller_name": strings.TrimSpace(caller), "signature": strings.TrimSpace(signature)}).Error
}

// KnownDevice reports whether token was issued to this email's account, and if so
// the name its attempts are counted under: the email and the device together, so
// guesses made elsewhere don't hold this browser up, and its own are still slowed.
func (s *IdentityStore) KnownDevice(email, token string) (who string, ok bool) {
	nonce, signature, found := strings.Cut(token, ".")
	if !found || len(token) > 200 {
		return "", false
	}
	u, err := s.UserByEmail(email)
	if err != nil || !hmac.Equal([]byte(signature), []byte(DeviceMAC(u, nonce))) {
		return "", false
	}
	return SignInBucket(u.Email) + "\x00" + nonce, true
}

// Users is how many people can sign in, in any organisation.
func (s *IdentityStore) Users() (int64, error) {
	var n int64
	return n, s.DB.Model(&store.User{}).Count(&n).Error
}

// SetAreas says what someone works on. None leaves them reading only.
func (s *IdentityStore) SetAreas(org, user int64, areas []string) error {
	for _, ar := range areas {
		if ar != AreaSales && ar != AreaSupport {
			return say.Errorf("unknown area %q", ar)
		}
	}
	var keep []string
	for _, ar := range Areas {
		if slices.Contains(areas, ar.Name) {
			keep = append(keep, ar.Name)
		}
	}
	res := s.DB.Model(&store.Membership{}).Where("org_id = ? and user_id = ?", org, user).Update("areas", strings.Join(keep, " "))
	if res.Error == nil && res.RowsAffected == 0 {
		return say.New("that person isn't in this organisation")
	}
	return res.Error
}
