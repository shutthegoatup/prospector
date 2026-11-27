package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
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
	hash, err := bcrypt.GenerateFromPassword([]byte(password), passwordCost)
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
	hash, err := bcrypt.GenerateFromPassword([]byte(password), passwordCost)
	if err != nil {
		return err
	}
	return s.savePassword(u.ID, string(hash), keepToken)
}

// savePassword also signs the user out everywhere except the session in keepToken.
func (s *Store) savePassword(user int64, hash, keepToken string) error {
	return s.db.Transaction(func(tx *gorm.DB) error { return s.savePasswordIn(tx, user, hash, keepToken) })
}

// A new password also ends any emailed link that could still have set one.
func (s *Store) savePasswordIn(tx *gorm.DB, user int64, hash, keepToken string) error {
	set := map[string]any{"password_hash": hash, "must_change_password": false}
	if err := tx.Model(&User{}).Where("id = ?", user).Updates(set).Error; err != nil {
		return err
	}
	if err := tx.Where("user_id = ?", user).Delete(&PasswordToken{}).Error; err != nil {
		return err
	}
	return tx.Where("user_id = ? and token_hash not in ?", user, s.keys.lookups(sessions, keepToken)).Delete(&Session{}).Error
}

func (s *Store) UserByEmail(email string) (*User, error) {
	var u User
	return &u, s.db.Where("email = ?", strings.ToLower(strings.TrimSpace(email))).First(&u).Error
}

// CreateOrg makes an organisation nobody belongs to yet, for the CLI to add members to.
func (s *Store) CreateOrg(slug, name string) (*Org, error) {
	return createOrg(s.db, slug, name)
}

// frontOrgSlug is the organisation the front page's contact form sends enquiries to.
const frontOrgSlug = "prospector"

// EnsureFrontOrg makes that organisation on a server that hasn't one. A new one takes
// enquiries from the server's own address when that is known; from then on its sites
// are its owners' to change, so an existing one is left as it is.
func (s *Store) EnsureFrontOrg(base string) error {
	if _, err := s.OrgBySlug(frontOrgSlug); !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	// Made and given its site together: one left without would never be given it.
	err := s.db.Transaction(func(tx *gorm.DB) error {
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
func (s *Store) FrontFormOff(base string) string {
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
	if !o.Settings.takesEnquiriesFrom(browserOrigin(base)) {
		return fmt.Sprintf("the %q organisation %s, not %s; %s", frontOrgSlug, listed, browserOrigin(base), fix)
	}
	return ""
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
	inUse := conflict(fmt.Sprintf("another organisation already has the address /o/%s: choose a different name", slug))
	var same, n int64
	if err := tx.Model(&Org{}).Where("slug = ?", slug).Count(&same).Error; err != nil {
		return nil, err
	}
	if same > 0 {
		return nil, inUse
	}
	if err := tx.Model(&Org{}).Count(&n).Error; err != nil {
		return nil, err
	}
	o := &Org{Slug: slug, Name: strings.TrimSpace(name), Color: orgColors[int(n)%len(orgColors)]}
	// Two asking for the same address at once both find it free; the index stops the
	// second, and the slug is the only thing about a new organisation it guards.
	if err := tx.Create(o).Error; breaksRule(err) {
		return nil, inUse
	} else if err != nil {
		return nil, err
	}
	return o, nil
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
		return setRole(tx, org, &m, role)
	})
}

// SetRole changes the role of someone already in the organisation.
func (s *Store) SetRole(org, user int64, role string) error {
	if role != "owner" && role != "member" {
		return fmt.Errorf("a role is owner or member, not %q", role)
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
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

func setRole(tx *gorm.DB, org int64, m *Membership, role string) error {
	if m.Role == "owner" && role != "owner" && lastOwner(tx, org) {
		return errLastOwner
	}
	return tx.Model(m).Update("role", role).Error
}

// lockOrg makes changes to one organisation's members take turns, so two owners
// removing each other at the same moment can't both count the other as remaining.
func lockOrg(tx *gorm.DB, org int64) error {
	return tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&Org{}, org).Error
}

func lastOwner(tx *gorm.DB, org int64) bool {
	var owners int64
	// A count that failed is taken as one owner, so nobody is removed on the strength of it.
	if logged(tx.Model(&Membership{}).Where("org_id = ? and role = ?", org, "owner").Count(&owners)) != nil {
		return true
	}
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
		if err := dropRecipients(tx, tx.Where("org_id = ? and user_id = ?", org, user)); err != nil {
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
func (s *Store) NewSession(user, ssoOrg int64) (string, error) {
	token := randomToken(32)
	sess := &Session{TokenHash: s.keys.lookup(sessions, token), UserID: user, ExpiresAt: time.Now().Add(sessionLife)}
	if ssoOrg != 0 {
		sess.Limited = true
		sess.prove(ssoOrg, time.Now())
	}
	return token, s.db.Create(sess).Error
}

// proofLife is how long going through an organisation's provider counts for. After it
// the provider is asked again, so somebody it has since turned away is out within a day.
const proofLife = 24 * time.Hour

// proofs reads SSOOrgs: each organisation with when its proof runs out, as ",3:1795600000,".
func (sess *Session) proofs() map[int64]int64 {
	out := map[int64]int64{}
	for _, part := range strings.Split(sess.SSOOrgs, ",") {
		org, until, ok := strings.Cut(part, ":")
		o, err1 := strconv.ParseInt(org, 10, 64)
		u, err2 := strconv.ParseInt(until, 10, 64)
		if ok && err1 == nil && err2 == nil {
			out[o] = u
		}
	}
	return out
}

func (sess *Session) prove(org int64, now time.Time) {
	proofs := sess.proofs()
	proofs[org] = now.Add(proofLife).Unix()
	sess.SSOOrgs = ","
	for o, until := range proofs {
		if until > now.Unix() {
			sess.SSOOrgs += fmt.Sprintf("%d:%d,", o, until)
		}
	}
}

func (sess *Session) Proves(org int64) bool { return sess.proofs()[org] > time.Now().Unix() }

func (sess *Session) Opens(org int64) bool { return !sess.Limited || sess.Proves(org) }

// AddProof records that a session already open has now been through org's own sign-in too.
func (s *Store) AddProof(sess *Session, org int64) error {
	sess.prove(org, time.Now())
	return s.db.Model(sess).Update("sso_orgs", sess.SSOOrgs).Error
}

func (s *Store) SessionFor(token string) (*Session, *User, error) {
	var sess Session
	if err := s.db.Where("token_hash in ? and expires_at > ?", s.keys.lookups(sessions, token), time.Now()).First(&sess).Error; err != nil {
		return nil, nil, err
	}
	// Found by an older key: moved to the current one, so that key can be removed. One
	// under a newer key is another server's doing and is left alone.
	if s.keys.lookupBehind(sess.TokenHash) {
		current := s.keys.lookup(sessions, token)
		if s.db.Model(&sess).Update("token_hash", current).Error == nil {
			sess.TokenHash = current
		}
	}
	var u User
	return &sess, &u, s.db.First(&u, sess.UserID).Error
}

func (s *Store) SessionUser(token string) (*User, error) {
	_, u, err := s.SessionFor(token)
	return u, err
}

func (s *Store) Logout(token string) error {
	return s.db.Where("token_hash in ?", s.keys.lookups(sessions, token)).Delete(&Session{}).Error
}

// Scopes an API key can carry. None can change members or keys or reach another org; only enquiries:reply, customers:invite and campaigns:send send mail, and only import skips review.
var Scopes = []struct{ Name, What string }{
	{"read", "search companies and people, read what's due and the tasks people wrote, read deals, subscriptions, onboardings and enquiries with what was said in answering them, and read the organisation's settings, its never-contact list and agent guidance included, and what it sells with where each company stands for each"},
	{"research:write", "add companies, facts and suggested contacts (they wait in Research), and leave notes on tasks"},
	{"drafts:write", "write draft notes and emails, marked as written by the agent"},
	{"outcomes:write", "record what happened (added, accepted, sent, called, replied, declined, hold), shown as the agent's"},
	{"import", "push the repo's markdown and pull the snapshot"},
	{"enquiries:write", "record an enquiry or sign-up from your own product or site backend"},
	{"enquiries:reply", "email a reply to whoever sent an enquiry, through your mail server, and record what they said back"},
	{"customers:write", "record and change subscriptions, start their renewals, and start, change and remove onboardings"},
	{"pipeline:write", "work deals (start, value, stage, owner, remove) and say where answering an enquiry stands, who has it, and put its sender on the board"},
	{"enquiries:triage", "say where answering an enquiry stands, who has it, and put its sender on the board, without touching deals"},
	{"enquiries:delete", "delete an enquiry for good, with what was said in answering it"},
	{"campaigns:write", "write campaigns, see who they would go to, and keep the list of addresses marketing mail never goes to"},
	{"campaigns:send", "send a campaign to everyone in conversation or at a customer, through your own mail server, and cancel or resume one"},
	{"agreements:write", "start agreements for companies, upload their documents, and set out who signs them and where; nothing is sent"},
	{"customers:invite", "email a customer a link to their onboarding steps, through your mail server, and withdraw it"},
	{"contacts:write", "add people straight to outreach and change their details and where their conversation stands"},
	{"research:review", "qualify and reject companies, for the organisation or for one thing it sells, pick suggested people for outreach and accept or dismiss details agents found: the review a person otherwise does"},
	{"people:privacy", "export everything held on a person, and erase them for good on their request"},
	{"enrich:spend", "ask the organisation's enrichment vendors, at its cost and within each one's monthly limit, for an email, phone or profile someone lacks; what is found waits to be accepted"},
	{"tasks:write", "start tasks, ask them for more, mark their requests done or not, close and reopen them, and delete the notes agents left on them"},
	{"agents:run", "start and stop Prospector's own agents on tasks"},
	{"agents:configure", "add, change and delete Prospector's own agents, their endpoints, keys and tools"},
	{"settings:write", "change the organisation's settings: its name and colour, waits and wording, the guidance sent to every agent, never-contact list, what it looks for, enquiry sites, onboarding steps, currency, postal address and whether to email only incorporated companies"},
	{"team:write", "add people to the team, change their roles and areas, and take them out: as strong as an owner, since it can make one"},
	{"mail:configure", "set the organisation's outgoing mail server, its username and password"},
	{"sso:configure", "set up single sign-on: the provider, its client secret, the email domains it speaks for and whether it is required"},
	{"org:export", "download everything the organisation holds in one file"},
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
	if err := checkScopes(scopes); err != nil {
		return "", nil, err
	}
	secret := "pk_" + randomToken(24)
	k := &APIKey{OrgID: org, Name: name, Prefix: secret[:7], Hash: s.keys.lookup(apiKeys, secret), Scopes: strings.Join(scopes, " "), CreatedBy: by}
	return secret, k, s.db.Create(k).Error
}

func (s *Store) KeyFor(secret string) (*APIKey, error) {
	var k APIKey
	// A key made before there was a secret key is still found by its plain hash: an
	// agent can't be asked to sign in again the way a person can. It moves on first
	// use. The cost is that a row written straight into the database with a plain hash
	// is taken for one of those, so the keyed lookup guards sessions and not API keys.
	found := append(s.keys.lookups(apiKeys, secret), hashToken(secret))
	if err := s.db.Where("hash in ? and revoked_at is null", found).First(&k).Error; err != nil {
		return nil, err
	}
	now := time.Now()
	set := map[string]any{"last_used_at": now}
	if s.keys.lookupBehind(k.Hash) {
		set["hash"] = s.keys.lookup(apiKeys, secret)
	}
	_ = logged(s.db.Model(&k).Updates(set))
	return &k, nil
}

func (s *Store) Keys(org int64) ([]APIKey, error) {
	var keys []APIKey
	return keys, s.in(org).Where("revoked_at is null").Order("created_at desc").Find(&keys).Error
}

func checkScopes(scopes []string) error {
	for _, sc := range scopes {
		if !validScope(sc) {
			return fmt.Errorf("unknown scope %q", sc)
		}
	}
	if len(scopes) == 0 {
		return errors.New("pick at least one scope")
	}
	return nil
}

var errBuiltinKey = errors.New("this key belongs to one of Prospector's own agents, which change on the agent itself")

// SetKeyScopes changes what a key may do without changing its secret.
func (s *Store) SetKeyScopes(org, id int64, scopes []string) error {
	if err := checkScopes(scopes); err != nil {
		return err
	}
	var k APIKey
	if err := s.in(org).Where("id = ? and revoked_at is null", id).First(&k).Error; errors.Is(err, gorm.ErrRecordNotFound) {
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
	return s.in(org).Model(&APIKey{}).Where("id = ? and revoked_at is null", id).Update("scopes", strings.Join(ordered, " ")).Error
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

// ChangeSettings saves o and notes in its history who changed which settings, since a
// key can now empty the never-contact list or open the enquiry route to a site.
func (s *Store) ChangeSettings(o *Org, who Actor, changed []string) error {
	if len(changed) == 0 {
		return nil
	}
	if !colorRe.MatchString(o.Color) {
		return fmt.Errorf("colour must look like #56c5f4")
	}
	what := strings.Join(changed, ", ")
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(o).Select("name", "color", "settings").Updates(o).Error; err != nil {
			return err
		}
		return s.logAs(tx, o.ID, 0, nil, who, "settings:change", &what)
	})
}

func (s *Store) SaveVoice(org, user int64, caller, signature string) error {
	return s.db.Model(&Membership{}).Where("org_id = ? and user_id = ?", org, user).
		Updates(map[string]any{"caller_name": strings.TrimSpace(caller), "signature": strings.TrimSpace(signature)}).Error
}
