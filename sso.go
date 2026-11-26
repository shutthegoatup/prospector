package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"gorm.io/gorm"
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

func (d SSODomain) Record() string { return challengeLabel + "." + d.Domain }

const (
	challengeLabel = "_prospector-challenge"
	maxSSODomains  = 20
)

var googleIssuer = "https://accounts.google.com"

type SSO struct {
	SSOConnection
	Domains []SSODomain
}

func (c *SSO) On() bool { return c.Issuer != "" }

func emailDomain(email string) string {
	_, domain, _ := strings.Cut(strings.ToLower(strings.TrimSpace(email)), "@")
	return domain
}

// Proves is whether the organisation's sign-in may speak for an address: only one at
// a domain it has shown is its own, matched whole.
func (c *SSO) Proves(email string) bool {
	domain := emailDomain(email)
	for _, d := range c.Domains {
		if d.VerifiedAt != nil && d.Domain == domain && domain != "" {
			return true
		}
	}
	return false
}

// spec opens the connection's client secret, which the database holds sealed.
func (s *Store) spec(c *SSO) (idpSpec, error) {
	secret, err := s.keys.open(c.ClientSecret)
	return idpSpec{Issuer: c.Issuer, ClientID: c.ClientID, Secret: secret}, err
}

func (s *Store) SSO(org int64) (*SSO, error) {
	c := &SSO{}
	if err := s.db.Where("org_id = ?", org).Limit(1).Find(&c.SSOConnection).Error; err != nil {
		return nil, err
	}
	c.OrgID = org
	return c, s.in(org).Order("domain").Find(&c.Domains).Error
}

// SaveSSO keeps the connection; an empty issuer turns the organisation's sign-in off.
func (s *Store) SaveSSO(c *SSOConnection, actor string) error {
	// Sealed here, whoever is saving, so nothing a key could seal is written in clear.
	if c.ClientSecret != "" && !sealed(c.ClientSecret) && s.keys != nil {
		stored, err := s.keys.seal(c.ClientSecret)
		if err != nil {
			return err
		}
		c.ClientSecret = stored
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
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
		return s.log(tx, c.OrgID, 0, nil, actor, "sso:change", &what)
	})
}

// SSOOrgsFor is the organisations whose own sign-in speaks for an email domain.
func (s *Store) SSOOrgsFor(domain string) ([]Org, error) {
	var out []Org
	if domain == "" {
		return nil, nil
	}
	err := s.db.Table("orgs").Select("orgs.*").
		Joins("join sso_domains on sso_domains.org_id = orgs.id and sso_domains.verified_at is not null").
		Joins("join sso_connections on sso_connections.org_id = orgs.id and sso_connections.issuer <> ''").
		Where("sso_domains.domain = ?", domain).Order("orgs.name").Scan(&out).Error
	return out, err
}

var (
	errNoAccount = errors.New("there's no account here for that address: ask whoever runs your organisation to add you")
	errUnvouched = errors.New("the provider didn't confirm that address is yours, so it can't be matched to an account here")
	errElsewhere = errors.New("that sign-in is linked to an account whose address isn't at this organisation's domains")
	errNotMember = errors.New("you aren't a member of that organisation: ask one of its owners to add you")
)

func unusableHash() string { return "!" + randomToken(32) }

// HasPassword is false for an account that only ever signs in through a provider.
func (u *User) HasPassword() bool {
	return u.PasswordHash != "" && !strings.HasPrefix(u.PasswordHash, "!")
}

// resolve finds whose sign-in a provider's answer is. A link made before wins; an
// address makes a new one only when byEmail says the provider may speak for it.
func resolve(tx *gorm.DB, who *outsider, email string, byEmail bool, create func() (*User, error)) (*User, error) {
	var u User
	var id Identity
	err := tx.Where("provider = ? and subject = ?", who.Issuer, who.Subject).First(&id).Error
	if err == nil {
		return &u, tx.First(&u, id.UserID).Error
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if !byEmail || email == "" {
		return nil, errUnvouched
	}
	found := &u
	if err := tx.Where("email = ?", email).First(&u).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		if create == nil {
			return nil, errNoAccount
		}
		if found, err = create(); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	return found, tx.Create(&Identity{UserID: found.ID, Provider: who.Issuer, Subject: who.Subject}).Error
}

// claim ends a first password its inviter saw, once the account's owner has arrived
// another way: they can't be asked to change a password they were never told.
func claim(tx *gorm.DB, u *User) error {
	if !u.MustChangePassword {
		return nil
	}
	u.PasswordHash, u.MustChangePassword = unusableHash(), false
	if err := tx.Model(u).Select("password_hash", "must_change_password").Updates(u).Error; err != nil {
		return err
	}
	// An emailed invitation could still set a password; it ends with the first password.
	if err := tx.Where("user_id = ?", u.ID).Delete(&PasswordToken{}).Error; err != nil {
		return err
	}
	return tx.Where("user_id = ?", u.ID).Delete(&Session{}).Error
}

// SocialUser is the account a social sign-in belongs to. It never makes one.
func (s *Store) SocialUser(who *outsider) (*User, error) {
	var u *User
	err := s.db.Transaction(func(tx *gorm.DB) (err error) {
		if u, err = resolve(tx, who, who.Email, who.Vouched, nil); err != nil {
			return err
		}
		return claim(tx, u)
	})
	return u, err
}

// SSOUser is the member an organisation's own sign-in has let in, made a member on
// the way if the organisation lets people join. Nobody becomes an owner like this.
func (s *Store) SSOUser(org *Org, c *SSO, who *outsider, email string) (*User, error) {
	var u *User
	err := s.db.Transaction(func(tx *gorm.DB) (err error) {
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
		return s.log(tx, org.ID, 0, nil, "sso", "member:join", &u.Email)
	})
	return u, err
}

// socialWay is a sign-in the whole deployment offers, registered once by whoever runs it.
type socialWay struct {
	Key, Label, Issuer string
	Plain, ManyTenants bool
	ClientID, Secret   string
}

func (way socialWay) spec() idpSpec {
	return idpSpec{Issuer: way.Issuer, ClientID: way.ClientID, Secret: way.Secret, Plain: way.Plain, ManyTenants: way.ManyTenants}
}

var socialWays = []socialWay{
	{Key: "google", Label: "Google", Issuer: googleIssuer},
	{Key: "microsoft", Label: "Microsoft", Issuer: "https://login.microsoftonline.com/common/v2.0", ManyTenants: true},
	{Key: "linkedin", Label: "LinkedIn", Issuer: "https://www.linkedin.com/oauth", Plain: true},
}

// socialFromEnv reads PROSPECTOR_<PROVIDER>_CLIENT_ID and _CLIENT_SECRET.
func socialFromEnv(getenv func(string) string) ([]socialWay, error) {
	var out []socialWay
	for _, way := range socialWays {
		name := "PROSPECTOR_" + strings.ToUpper(way.Key) + "_CLIENT_"
		way.ClientID, way.Secret = getenv(name+"ID"), getenv(name+"SECRET")
		if (way.ClientID == "") != (way.Secret == "") {
			return nil, fmt.Errorf("%sID and %sSECRET go together: one is set without the other", name, name)
		}
		if way.ClientID != "" {
			out = append(out, way)
		}
	}
	return out, nil
}

func (a *App) callbackURL() string { return a.base + "/login/callback" }

type LoginView struct {
	Next, Problem, Notice string
	Ways                  []socialWay
	SSO                   bool
	// Forgot offers the reset link, which needs mail.
	Forgot bool
}

func (a *App) loginView(next, problem string) LoginView {
	return LoginView{Next: next, Problem: problem, Ways: a.social, SSO: a.base != "", Forgot: a.mailOn()}
}

func (a *App) signInProblem(w http.ResponseWriter, r *http.Request, next string, code int, problem string) {
	w.Header().Set("content-type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	if err := LoginPage(a.loginView(next, problem)).Render(r.Context(), w); err != nil {
		log.Print(err)
	}
}

func (a *App) loginWith(w http.ResponseWriter, r *http.Request) {
	next := r.URL.Query().Get("next")
	for _, way := range a.social {
		if way.Key != r.PathValue("way") {
			continue
		}
		p, err := a.idp(r.Context(), way.spec())
		if err != nil {
			log.Printf("sign-in with %s: %v", way.Key, err)
			a.signInProblem(w, r, next, http.StatusBadGateway, way.Label+" couldn't be reached. Try again, or sign in another way.")
			return
		}
		a.beginAttempt(w, r, p, attempt{Way: way.Key, Next: next})
		return
	}
	http.NotFound(w, r)
}

type SSOView struct {
	Next, Email, Problem string
	// Asked is the organisation that sent a signed-in member here, when one did.
	Asked string
	Orgs  []Org
}

// ssoPageURL is where a member is sent when an organisation requires its sign-on. It
// is the page and not the provider: that member may have just come from the password
// form, and the content policy stops a form's redirects from ending at another site.
func ssoPageURL(org *Org, email, next string) string {
	return "/login/sso?" + url.Values{"org": {org.Slug}, "email": {email}, "next": {next}}.Encode()
}

// ssoPage asks for an address and lists the organisations whose sign-in speaks for
// it. It answers with links, never a redirect: the content policy's form-action
// would stop a form that ended at another site.
func (a *App) ssoPage(w http.ResponseWriter, r *http.Request) {
	if a.base == "" {
		http.NotFound(w, r)
		return
	}
	v := SSOView{Next: r.URL.Query().Get("next"), Email: strings.ToLower(strings.TrimSpace(r.URL.Query().Get("email")))}
	if v.Email != "" {
		v.Orgs, _ = a.s.SSOOrgsFor(emailDomain(v.Email))
		if len(v.Orgs) == 0 {
			v.Problem = "No organisation here uses single sign-on for that address. Sign in with your password instead."
		}
		for _, o := range v.Orgs {
			if o.Slug == r.URL.Query().Get("org") {
				v.Asked, v.Orgs = o.Name, []Org{o}
			}
		}
	}
	http.SetCookie(w, &http.Cookie{Name: askedCookie, Value: "1", Path: "/login/sso", MaxAge: 600, HttpOnly: true,
		Secure: a.secure(r), SameSite: http.SameSiteStrictMode})
	w.Header().Set("content-type", "text/html; charset=utf-8")
	if err := SSOPage(v).Render(r.Context(), w); err != nil {
		log.Print(err)
	}
}

// askedCookie marks a browser that has seen the single sign-on page. It is Strict, so
// it travels with a click on that page and not with a link from another site: nobody
// can be walked through a provider of somebody else's choosing, and into the account
// it names, by a link alone.
const askedCookie = "sso-asked"

func ssoStartURL(org *Org, email, next string) string {
	return "/login/sso/start?" + url.Values{"org": {org.Slug}, "email": {email}, "next": {next}}.Encode()
}

// ssoStart sends somebody to an organisation's provider, only from the page that
// offered it and only for an address that provider may speak for.
func (a *App) ssoStart(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	if _, err := r.Cookie(askedCookie); err != nil {
		http.Redirect(w, r, "/login/sso?"+url.Values{"email": {query.Get("email")}, "next": {query.Get("next")}}.Encode(), http.StatusSeeOther)
		return
	}
	email := strings.ToLower(strings.TrimSpace(query.Get("email")))
	org, err := a.s.OrgBySlug(query.Get("org"))
	var sso *SSO
	if err == nil {
		sso, err = a.s.SSO(org.ID)
	}
	if a.base == "" || err != nil || !sso.On() || !sso.Proves(email) {
		a.signInProblem(w, r, query.Get("next"), http.StatusBadRequest, "That organisation has no single sign-on for that address.")
		return
	}
	spec, err := a.s.spec(sso)
	if err != nil {
		log.Printf("single sign-on for %s: %v", org.Slug, err)
		a.signInProblem(w, r, query.Get("next"), http.StatusServiceUnavailable, unopenable(org))
		return
	}
	p, err := a.idp(r.Context(), spec)
	if err != nil {
		log.Printf("single sign-on for %s: %v", org.Slug, err)
		a.signInProblem(w, r, query.Get("next"), http.StatusBadGateway, org.Name+"'s sign-in couldn't be reached. Try again shortly.")
		return
	}
	a.beginAttempt(w, r, p, attempt{Way: "sso", Org: org.ID, Next: query.Get("next")}, oauth2.SetAuthURLParam("login_hint", email))
}

// unopenable is what people are told when a connection's kept secret can't be opened.
// It isn't an outage, and trying again won't help: the server needs its key back, or
// an owner has to type the secret again.
func unopenable(org *Org) string {
	return org.Name + "'s single sign-on isn't working here, and trying again won't help. Tell one of its owners, or whoever runs this server."
}

func (a *App) loginCallback(w http.ResponseWriter, r *http.Request) {
	att, err := a.resumeAttempt(w, r)
	if err != nil {
		a.signInProblem(w, r, "", http.StatusBadRequest, sentence(err))
		return
	}
	refuse := func(err error) {
		log.Printf("sign-in through %s refused: %v", att.Way, err)
		problem := "That sign-in couldn't be completed. Try again, or sign in another way."
		for _, known := range []error{errNoAccount, errUnvouched, errElsewhere, errNotMember, errWrongDomain, errDisowned} {
			if errors.Is(err, known) {
				problem = sentence(err)
			}
		}
		a.signInProblem(w, r, att.Next, http.StatusForbidden, problem)
	}
	if att.Way == "sso" {
		a.finishSSO(w, r, att, refuse)
		return
	}
	for _, way := range a.social {
		if way.Key != att.Way {
			continue
		}
		p, err := a.idp(r.Context(), way.spec())
		if err != nil {
			refuse(err)
			return
		}
		who, err := p.finish(r.Context(), att, r.URL.Query())
		if err != nil {
			refuse(err)
			return
		}
		u, err := a.s.SocialUser(who)
		if err != nil {
			refuse(err)
			return
		}
		token, err := a.s.NewSession(u.ID, 0)
		if err != nil {
			refuse(err)
			return
		}
		a.setSession(w, r, token)
		http.Redirect(w, r, safeNext(att.Next), http.StatusSeeOther)
		return
	}
	refuse(errNotStartedHere)
}

var (
	errWrongDomain = errors.New("that address isn't at a domain this organisation has shown is its own")
	errDisowned    = errors.New("the organisation's provider says that address hasn't been verified")
)

func (a *App) finishSSO(w http.ResponseWriter, r *http.Request, att attempt, refuse func(error)) {
	org, err := a.s.OrgByID(att.Org)
	if err != nil {
		refuse(err)
		return
	}
	sso, err := a.s.SSO(org.ID)
	if err != nil || !sso.On() {
		refuse(errors.Join(err, errors.New("the organisation's single sign-on is off")))
		return
	}
	spec, err := a.s.spec(sso)
	if err != nil {
		log.Printf("single sign-on for %s: %v", org.Slug, err)
		a.signInProblem(w, r, att.Next, http.StatusServiceUnavailable, unopenable(org))
		return
	}
	p, err := a.idp(r.Context(), spec)
	if err != nil {
		refuse(err)
		return
	}
	who, err := p.finish(r.Context(), att, r.URL.Query())
	if err != nil {
		refuse(err)
		return
	}
	email := who.Email
	// Entra sends no email for an account without a mailbox, and names it by sign-in address.
	if email == "" && strings.Contains(who.Username, "@") {
		email = who.Username
	}
	switch {
	case who.Disowned:
		err = errDisowned
	case !sso.Proves(email):
		err = errWrongDomain
	// Anyone can open a Google account under an address they read mail at; hd says
	// the organisation's own Workspace holds this one.
	case who.Issuer == googleIssuer && who.HostedBy != emailDomain(email):
		err = errWrongDomain
	}
	if err != nil {
		refuse(err)
		return
	}
	u, err := a.s.SSOUser(org, sso, who, email)
	if err != nil {
		refuse(err)
		return
	}
	// A browser already signed in as this person keeps its session and gains the proof.
	if sess, current := a.session(r); current != nil && current.ID == u.ID {
		err = a.s.AddProof(sess, org.ID)
	} else {
		var token string
		if token, err = a.s.NewSession(u.ID, org.ID); err == nil {
			a.setSession(w, r, token)
		}
	}
	if err != nil {
		refuse(err)
		return
	}
	next := safeNext(att.Next)
	if !strings.HasPrefix(next, "/o/"+org.Slug+"/") {
		m, _ := a.s.Membership(org.ID, u.ID)
		next = "/o/" + org.Slug + "/" + landing(m)
	}
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func sentence(err error) string {
	_, text := telling("single sign-on", err, http.StatusBadRequest)
	_, message := problemText(http.StatusBadRequest, text)
	return message
}

// ssoRequired is whether this member must come through the organisation's own sign-in
// and hasn't. Owners keep their password, so a broken provider can be put right.
func (a *App) ssoRequired(org *Org, u *User, m *Membership, sess *Session) bool {
	if m.Role == "owner" || sess.Proves(org.ID) {
		return false
	}
	// Most organisations require nothing, which one read of the connection settles.
	var c SSOConnection
	if err := a.s.db.Where("org_id = ?", org.ID).Limit(1).Find(&c).Error; err != nil || !c.Required {
		return false
	}
	sso, err := a.s.SSO(org.ID)
	return err == nil && sso.On() && sso.Proves(u.Email)
}

var domainRe = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)

// checkIssuer catches the mistakes an owner can make typing an issuer in.
func checkIssuer(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return fmt.Errorf("an issuer looks like https://login.example.com, not %q", raw)
	}
	if first, _, _ := strings.Cut(strings.TrimPrefix(u.Path, "/"), "/"); strings.EqualFold(u.Hostname(), "login.microsoftonline.com") &&
		(first == "common" || first == "organizations" || first == "consumers") {
		return errors.New("that is Microsoft's sign-in for every tenant, so it pins nobody to yours: use https://login.microsoftonline.com/<your tenant id>/v2.0")
	}
	return nil
}

type ssoIn struct {
	Issuer   string `json:"issuer"`
	ClientID string `json:"client_id"`
	Secret   string `json:"client_secret"`
	Required bool   `json:"required"`
	AutoJoin bool   `json:"auto_join"`
}

func (a *App) uiSSO(w http.ResponseWriter, r *http.Request) {
	var in ssoIn
	if !decode(w, r, &in) {
		return
	}
	if err := a.saveSSO(r.Context(), reqOf(r), in); err != nil {
		fail(w, err, http.StatusBadRequest)
		return
	}
	done(w, nil)
}

func (a *App) saveSSO(ctx context.Context, q *Req, in ssoIn) error {
	sso, err := a.s.SSO(q.Org.ID)
	if err != nil {
		return err
	}
	c := SSOConnection{OrgID: q.Org.ID, Issuer: strings.TrimSpace(in.Issuer), ClientID: strings.TrimSpace(in.ClientID),
		Required: in.Required, AutoJoin: in.AutoJoin}
	if c.Issuer == "" {
		return a.s.SaveSSO(&SSOConnection{OrgID: q.Org.ID}, q.Actor())
	}
	secret := strings.TrimSpace(in.Secret)
	switch {
	case secret != "" && a.s.keys == nil:
		return fmt.Errorf("a client secret is kept sealed, and %w", errNoKeys)
	case secret != "":
		// Sealed here, where it is known to have been typed: one that happens to look
		// sealed already would otherwise be stored as it came.
		if c.ClientSecret, err = a.s.keys.seal(secret); err != nil {
			return err
		}
	// The kept secret belongs to the provider and client it was typed for, and goes to
	// no other. It stays as stored and unopened, so the rest can be changed on a server
	// with no key, or with a key that no longer opens it.
	case c.Issuer == sso.Issuer && c.ClientID == sso.ClientID:
		c.ClientSecret = sso.ClientSecret
		// But nobody is made to depend on a sign-in that can't work.
		if _, oerr := a.s.keys.open(c.ClientSecret); oerr != nil && ((c.Required && !sso.Required) || (c.AutoJoin && !sso.AutoJoin)) {
			return errors.New("this server can't open the kept client secret, so single sign-on isn't working here: type the secret again before requiring it or letting people join")
		}
	}
	verified := false
	for _, d := range sso.Domains {
		verified = verified || d.VerifiedAt != nil
	}
	switch {
	case a.base == "":
		err = errors.New("single sign-on needs PROSPECTOR_URL set to this app's address, so the provider knows where to send people back")
	case c.ClientID == "" || c.ClientSecret == "":
		err = errors.New("the provider's client ID and client secret are both needed, and the secret again when the issuer or client ID changes")
	case (c.Required || c.AutoJoin) && !verified:
		err = errors.New("verify a domain first: requiring single sign-on, or letting people join, applies to addresses at your verified domains")
	default:
		err = checkIssuer(c.Issuer)
	}
	if err == nil {
		// Reading the provider now catches a mistyped issuer before anyone is sent to it.
		// Discovery sends no secret.
		if _, derr := a.probe(ctx, idpSpec{Issuer: c.Issuer, ClientID: c.ClientID}); errors.Is(derr, errManyTenants) {
			err = derr
		} else if derr != nil {
			err = elsewhere{fmt.Errorf("that issuer couldn't be read, so nothing was saved: %w", derr)}
		}
	}
	if err != nil {
		return err
	}
	return a.s.SaveSSO(&c, q.Actor())
}

type domainIn struct {
	Domain string `json:"domain"`
}

func (a *App) uiSSODomain(w http.ResponseWriter, r *http.Request) {
	var in domainIn
	if !decode(w, r, &in) {
		return
	}
	if _, err := a.addSSODomain(reqOf(r).Org.ID, in.Domain); err != nil {
		fail(w, err, http.StatusBadRequest)
		return
	}
	done(w, nil)
}

func (a *App) addSSODomain(org int64, raw string) (*SSODomain, error) {
	domain := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(raw)), "@")
	if !domainRe.MatchString(domain) {
		return nil, fmt.Errorf("a domain looks like example.com, not %q", raw)
	}
	var n int64
	logged(a.s.in(org).Model(&SSODomain{}).Count(&n))
	if n >= maxSSODomains {
		return nil, fmt.Errorf("an organisation can have %d domains", maxSSODomains)
	}
	d := &SSODomain{OrgID: org, Domain: domain, Token: randomToken(20)}
	if err := a.s.db.Create(d).Error; err != nil {
		if breaksRule(err) {
			err = conflict(domain + " is already listed")
		}
		return nil, err
	}
	return d, nil
}

func (a *App) uiSSODomainRemove(w http.ResponseWriter, r *http.Request) {
	var in domainIn
	if !decode(w, r, &in) {
		return
	}
	done(w, a.removeSSODomain(reqOf(r).Org.ID, in.Domain))
}

func (a *App) removeSSODomain(org int64, domain string) error {
	sso, err := a.s.SSO(org)
	if err != nil {
		return err
	}
	others := 0
	for _, d := range sso.Domains {
		if d.VerifiedAt != nil && d.Domain != domain {
			others++
		}
	}
	if others == 0 && (sso.Required || sso.AutoJoin) {
		return errors.New("that is the last verified domain: stop requiring single sign-on and letting people join first")
	}
	return a.s.in(org).Where("domain = ?", domain).Delete(&SSODomain{}).Error
}

func (a *App) lookupTXT(ctx context.Context, name string) ([]string, error) {
	if a.txt != nil {
		return a.txt(ctx, name)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return net.DefaultResolver.LookupTXT(ctx, name)
}

func (a *App) uiSSOVerify(w http.ResponseWriter, r *http.Request) {
	var in domainIn
	if !decode(w, r, &in) {
		return
	}
	_, err := a.verifySSODomain(r.Context(), reqOf(r).Org.ID, in.Domain)
	done(w, err)
}

func (a *App) verifySSODomain(ctx context.Context, org int64, domain string) (*SSODomain, error) {
	var d SSODomain
	if err := a.s.in(org).Where("domain = ?", domain).First(&d).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		return nil, errors.New("that domain isn't listed")
	}
	records, _ := a.lookupTXT(ctx, d.Record())
	for _, record := range records {
		if strings.TrimSpace(record) == d.Token {
			now := time.Now()
			if err := a.s.db.Model(&d).Update("verified_at", &now).Error; err != nil {
				return nil, err
			}
			d.VerifiedAt = &now
			return &d, nil
		}
	}
	return nil, fmt.Errorf("no TXT record at %s holds the value yet: DNS changes can take a while to show", d.Record())
}

// agentCommand sets up an organisation's agents from the shell. A key comes from the
// environment, so it never sits in shell history or a process listing.
func agentCommand(s *Store, rest []string) error {
	usage := fmt.Errorf("usage: prospector agent set <org> <name> <endpoint> <model> [<tools-url> [<tool>…]], or prospector agent show <org>")
	if len(rest) < 2 {
		return usage
	}
	o, err := s.OrgBySlug(rest[1])
	if err != nil {
		return fmt.Errorf("no organisation %q", rest[1])
	}
	switch rest[0] {
	case "set":
		if len(rest) < 5 {
			return usage
		}
		if s.keys, err = parseKeys(os.Getenv("PROSPECTOR_SECRET_KEYS")); err != nil {
			return err
		}
		in := AgentSettingsIn{Name: rest[2], Kind: os.Getenv("PROSPECTOR_AGENT_KIND"), BaseURL: rest[3], Model: rest[4],
			APIKey: os.Getenv("PROSPECTOR_AGENT_API_KEY"), Tools: []string{}}
		if len(rest) > 5 {
			in.ToolsURL, in.Tools = rest[5], rest[6:]
		}
		// The name says which: an agent already called that is changed, else one is made.
		if name, err := agentName(in.Name); err == nil {
			if c, err := s.agentNamed(o.ID, name); err == nil {
				in.ID, in.Fallback, in.FallbackMinutes, in.SearchURL, in.ThinkingOff = c.ID, c.FallbackID, c.FallbackMinutes, c.SearchURL, c.ThinkingOff
			}
		}
		if search, ok := os.LookupEnv("PROSPECTOR_AGENT_SEARCH_URL"); ok {
			in.SearchURL = search
		}
		switch thinking, ok := os.LookupEnv("PROSPECTOR_AGENT_THINKING"); {
		case !ok:
		case thinking == "off" || thinking == "on":
			in.ThinkingOff = thinking == "off"
		default:
			return fmt.Errorf("PROSPECTOR_AGENT_THINKING is off or on, not %q", thinking)
		}
		c, err := (&App{s: s}).saveAgent(context.Background(), o.ID, in, "cli")
		if err != nil {
			return err
		}
		if c.Problem != "" {
			fmt.Fprintln(os.Stderr, "saved, but", c.Problem)
		}
	case "show":
	default:
		return usage
	}
	agents, err := s.Agents(o.ID)
	if err != nil {
		return err
	}
	for _, c := range agents {
		fmt.Printf("%s\n  endpoint: %s\n  model: %s\n  key: %v\n  tools: %s\n  models listed: %d\n", c.Name, c.BaseURL, c.Model, c.APIKey != "", c.ToolsURL, len(c.ModelList()))
		for _, t := range c.ToolList() {
			mark := " "
			if c.Allows(t.Name) {
				mark = "✓"
			}
			fmt.Printf("    %s %s\n", mark, t.Name)
		}
	}
	return nil
}

// ssoCommand is the way back in when a required provider has broken and no owner can sign in.
func ssoCommand(s *Store, rest []string) error {
	if arg(rest, 0, "") != "off" || len(rest) < 2 {
		return fmt.Errorf("usage: prospector sso off <org>")
	}
	o, err := s.OrgBySlug(rest[1])
	if err != nil {
		return fmt.Errorf("no organisation %q", rest[1])
	}
	sso, err := s.SSO(o.ID)
	if err != nil {
		return err
	}
	if !sso.Required {
		fmt.Fprintln(os.Stderr, o.Slug, "doesn't require single sign-on")
		return nil
	}
	sso.Required = false
	if err := s.SaveSSO(&sso.SSOConnection, "cli"); err != nil {
		return err
	}
	fmt.Println(o.Slug, "no longer requires single sign-on; passwords work again")
	return nil
}
