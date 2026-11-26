package main

import (
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
)

var (
	oldKey = strings.Repeat("0a", 32)
	newKey = strings.Repeat("0b", 32)
)

func mustKeys(t *testing.T, spec string) *keyring {
	t.Helper()
	k, err := parseKeys(spec)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestSecretKeysAreRefusedWhenMalformed(t *testing.T) {
	for what, spec := range map[string]string{
		"no version":               oldKey,
		"a version that isn't one": "one:" + oldKey,
		"a version written oddly":  "01:" + oldKey,
		"a key that isn't hex":     "1:" + strings.Repeat("zz", 32),
		"a short key":              "1:0a0b0c",
		"a version given twice":    "1:" + oldKey + ",1:" + newKey,
		"the key before a version": newKey + ":1",
	} {
		_, err := parseKeys(spec)
		if err == nil {
			t.Errorf("%s was accepted", what)
			continue
		}
		// The refusal goes to the terminal and the server's log.
		for _, secret := range []string{oldKey, newKey, "0a0b0c", "zz"} {
			if strings.Contains(err.Error(), secret) {
				t.Errorf("%s: the refusal quotes the key: %v", what, err)
			}
		}
	}
	if k, err := parseKeys("  "); k != nil || err != nil {
		t.Fatalf("no keys at all: %v %v", k, err)
	}
	if fresh := newSecretKey(); len(fresh) != 64 || fresh == newSecretKey() {
		t.Fatalf("a new key: %q", fresh)
	} else if _, err := parseKeys("1:" + fresh); err != nil {
		t.Fatalf("a new key isn't accepted: %v", err)
	}
	if err := run([]string{"secret-key", "two"}); err == nil {
		t.Fatal("secret-key took a version that isn't a number")
	}
}

func TestTokensAreFoundByAKeyedLookup(t *testing.T) {
	s := tempStore(t)
	alpha := mustOrg(t, s, "alpha")
	u := mustUser(t, s, alpha, "ada@alpha.example", "Ada Lovelace", "owner")
	token, err := s.NewSession(u.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	secret, key, err := s.CreateKey(alpha.ID, "bot", []string{"read"}, "test")
	if err != nil {
		t.Fatal(err)
	}
	var sess Session
	s.db.First(&sess)
	for what, stored := range map[string]string{"session": sess.TokenHash, "API key": key.Hash} {
		if lookupVersion(stored) != 1 || strings.Contains(stored, hashToken(token)) || strings.Contains(stored, hashToken(secret)) {
			t.Fatalf("a %s is stored as %q, not under key 1", what, stored)
		}
	}
	if sess.TokenHash == s.keys.lookup(apiKeys, token) {
		t.Fatal("sessions and API keys share a key")
	}

	// Whoever can write to the database but hasn't the key can't mint either: a row
	// holding a plain hash, as before there was a key, signs nobody in.
	if err := s.db.Create(&Session{TokenHash: hashToken("made-up"), UserID: u.ID, ExpiresAt: time.Now().Add(time.Hour)}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.SessionUser("made-up"); err == nil {
		t.Fatal("a session row written with a plain hash signed somebody in")
	}

	// A new key: both are still found, and move to it as they are used.
	s.keys = mustKeys(t, "2:"+newKey+",1:"+oldKey)
	if _, err := s.SessionUser(token); err != nil {
		t.Fatalf("a session from before the key changed: %v", err)
	}
	if _, err := s.KeyFor(secret); err != nil {
		t.Fatalf("an API key from before the key changed: %v", err)
	}
	s.db.First(&sess, sess.ID)
	s.db.First(key, key.ID)
	if lookupVersion(sess.TokenHash) != 2 || lookupVersion(key.Hash) != 2 {
		t.Fatalf("after use under the new key: session %q, API key %q", sess.TokenHash, key.Hash)
	}
	s.keys = mustKeys(t, "2:"+newKey)
	if _, err := s.SessionUser(token); err != nil {
		t.Fatalf("a session after the old key is removed: %v", err)
	}
	if _, err := s.KeyFor(secret); err != nil {
		t.Fatalf("an API key after the old key is removed: %v", err)
	}
	// Changing a password still signs out every session but the one doing it.
	other, _ := s.NewSession(u.ID, 0)
	if err := s.ChangePassword(u, "correct horse battery", "another of my own", token); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SessionUser(token); err != nil {
		t.Fatal("the session that changed the password was signed out")
	}
	if _, err := s.SessionUser(other); err == nil {
		t.Fatal("another session outlived a password change")
	}
	if err := s.Logout(token); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SessionUser(token); err == nil {
		t.Fatal("signing out left the session open")
	}
}

func TestAnAgentsKeyOutlivesTheServerBeingGivenASecretKey(t *testing.T) {
	s := tempStore(t)
	alpha := mustOrg(t, s, "alpha")
	u := mustUser(t, s, alpha, "ada@alpha.example", "Ada Lovelace", "owner")
	keys := s.keys
	// As a server with no key leaves them.
	s.keys = nil
	token, _ := s.NewSession(u.ID, 0)
	secret, key, err := s.CreateKey(alpha.ID, "bot", []string{"read"}, "test")
	if err != nil || key.Hash != hashToken(secret) {
		t.Fatalf("an API key made with no secret key: %q %v", key.Hash, err)
	}
	if _, err := s.SessionUser(token); err != nil {
		t.Fatalf("a session on a server with no key: %v", err)
	}

	s.keys = keys
	// People sign in again, which they can. An agent can't, so its key still works, once
	// by its plain hash and from then on by the keyed one.
	if _, err := s.SessionUser(token); err == nil {
		t.Fatal("a session found by its plain hash once there is a key")
	}
	if _, err := s.KeyFor(secret); err != nil {
		t.Fatalf("an API key from before there was a key: %v", err)
	}
	s.db.First(key, key.ID)
	if lookupVersion(key.Hash) != 1 {
		t.Fatalf("the API key after its first use: %q", key.Hash)
	}
	if _, err := s.KeyFor(secret); err != nil {
		t.Fatalf("the API key the second time: %v", err)
	}
	if _, err := s.KeyFor("pk_not-a-key"); err == nil {
		t.Fatal("a made-up API key was accepted")
	}

	// A browser known before the server had a key is still known after: everyone has
	// to sign in again that day, and shouldn't be held up by somebody else's guessing.
	s.keys = nil
	cookie := newDeviceToken(u)
	s.keys = keys
	if _, ok := s.knownDevice(u.Email, cookie); !ok {
		t.Fatal("a browser known before there was a key was forgotten")
	}

	// Two servers part-way through a key change don't move a token back and forth: one
	// under a newer key is left where it is.
	s.keys = mustKeys(t, "2:"+newKey+",1:"+oldKey)
	fresh, _ := s.NewSession(u.ID, 0)
	if _, err := s.KeyFor(secret); err != nil {
		t.Fatal(err)
	}
	s.keys = mustKeys(t, "1:"+oldKey+",2:"+newKey)
	if _, err := s.SessionUser(fresh); err != nil {
		t.Fatalf("a session under the newer key, on a server still using the older: %v", err)
	}
	if _, err := s.KeyFor(secret); err != nil {
		t.Fatalf("an API key under the newer key, on a server still using the older: %v", err)
	}
	var sess Session
	s.db.Where("token_hash in ?", s.keys.lookups(sessions, fresh)).First(&sess)
	s.db.First(key, key.ID)
	if lookupVersion(sess.TokenHash) != 2 || lookupVersion(key.Hash) != 2 {
		t.Fatalf("moved back to the older key: session %q, API key %q", sess.TokenHash, key.Hash)
	}
}

func TestAClearSecretIsSealedWhateverItLooksLike(t *testing.T) {
	// A secret kept in clear that happens to look like a token's lookup is still in clear.
	k := mustKeys(t, "1:"+oldKey)
	for _, clear := range []string{"k7:abc", "k12345", "kept-in-clear"} {
		if !k.behind(clear) {
			t.Errorf("%q, kept in clear, passes for sealed under a key", clear)
		}
	}
	if lookupVersion("k12345") != -1 || lookupVersion(hashToken("x")) != -1 || lookupVersion("k3:abc") != 3 {
		t.Fatal("reading the key a lookup names")
	}
}

func TestSealedSecretsOutliveAKeyChange(t *testing.T) {
	before := mustKeys(t, "1:"+oldKey)
	stored, err := before.seal("s3cret-value")
	if err != nil || !sealed(stored) || strings.Contains(stored, "s3cret") {
		t.Fatalf("sealing: %q %v", stored, err)
	}
	if again, _ := before.seal("s3cret-value"); again == stored {
		t.Fatal("the same secret sealed twice reads the same")
	}

	// Published: the new key is held, and the old one still seals.
	published := mustKeys(t, "1:"+oldKey+",2:"+newKey)
	if plain, err := published.open(stored); err != nil || plain != "s3cret-value" || published.behind(stored) {
		t.Fatalf("after the new key is published: %q %v", plain, err)
	}
	// Promoted: the new key seals, and the old one still opens.
	promoted := mustKeys(t, "2:"+newKey+",1:"+oldKey)
	if plain, err := promoted.open(stored); err != nil || plain != "s3cret-value" || !promoted.behind(stored) {
		t.Fatalf("after the new key is promoted: %q %v", plain, err)
	}
	// Retired too early: a value still under the old key says which key it needs.
	retired := mustKeys(t, "2:"+newKey)
	if _, err := retired.open(stored); !errors.Is(err, errUnknownKey) {
		t.Fatalf("a value under a key no longer held: %v", err)
	}

	moved, _ := promoted.seal("s3cret-value")
	// A character of the tag that carries six bits, changed to one it isn't: the last
	// two can already read "AA", and the final one holds only two bits.
	at := len(moved) - 3
	swap := byte('A')
	if moved[at] == swap {
		swap = 'B'
	}
	tampered := moved[:at] + string(swap) + moved[at+1:]
	if _, err := promoted.open(tampered); err == nil {
		t.Fatal("a tampered value opened")
	}
	// A value kept in clear before there was a key reads back as it is, key or no key.
	var none *keyring
	if plain, err := none.open("kept-in-clear"); err != nil || plain != "kept-in-clear" {
		t.Fatalf("a clear value with no key: %q %v", plain, err)
	}
	if _, err := none.open(stored); !errors.Is(err, errNoKeys) {
		t.Fatalf("a sealed value with no key: %v", err)
	}
	if _, err := none.seal("x"); !errors.Is(err, errNoKeys) {
		t.Fatalf("sealing with no key: %v", err)
	}
}

func TestEachJobHasAKeyOfItsOwnDerivedFromTheMaster(t *testing.T) {
	k := mustKeys(t, "1:"+oldKey)
	master, _ := hex.DecodeString(oldKey)
	seen := map[string]purpose{hex.EncodeToString(master): "the master key"}
	for _, p := range purposes {
		// RFC 5869, with the job as info.
		want, _ := hkdf.Key(sha256.New, master, nil, string(p), 32)
		got := k.keys[0].derived[p]
		if hex.EncodeToString(got) != hex.EncodeToString(want) {
			t.Errorf("the key for %q isn't HKDF-SHA256 of the master", p)
		}
		if other, taken := seen[hex.EncodeToString(got)]; taken {
			t.Errorf("%q shares a key with %q", p, other)
		}
		seen[hex.EncodeToString(got)] = p
	}
	// What is stored opens with any JOSE library under the sealing key, and not under the master.
	stored, _ := k.seal("s3cret-value")
	obj, err := jose.ParseEncryptedCompact(stored, []jose.KeyAlgorithm{jose.DIRECT}, []jose.ContentEncryption{jose.A256GCM})
	if err != nil {
		t.Fatal(err)
	}
	if plain, err := obj.Decrypt(k.keys[0].derived[sealing]); err != nil || string(plain) != "s3cret-value" {
		t.Fatalf("opening with the sealing key: %q %v", plain, err)
	}
	if _, err := obj.Decrypt(master); err == nil {
		t.Fatal("the master key itself opens a sealed value")
	}

	signature := k.sign(attempts, "an attempt")
	promoted := mustKeys(t, "2:"+newKey+",1:"+oldKey)
	if !promoted.verify(attempts, "an attempt", signature) || promoted.verify(attempts, "another", signature) || promoted.sign(attempts, "an attempt") == signature {
		t.Fatal("a signature should stay good after a key change, and new ones be made under the new key")
	}
	if mustKeys(t, "2:"+newKey).verify(attempts, "an attempt", signature) {
		t.Fatal("a signature still verifies once its key is removed")
	}
}

func TestTheServerSealsWhatItFindsWhenItStartsWithAKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.sqlite")
	seed, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	alpha := mustOrg(t, seed, "alpha")
	if err := seed.db.Create(&SSOConnection{OrgID: alpha.ID, Issuer: "https://login.alpha.example", ClientID: "client", ClientSecret: "kept-in-clear"}).Error; err != nil {
		t.Fatal(err)
	}
	closeStore(seed)

	t.Setenv("DATABASE_URL", path)
	t.Setenv("PROSPECTOR_URL", "")
	// A social provider with nowhere to send people back stops serve before it listens.
	t.Setenv("PROSPECTOR_GOOGLE_CLIENT_ID", "id")
	t.Setenv("PROSPECTOR_GOOGLE_CLIENT_SECRET", "secret")
	t.Setenv("PROSPECTOR_SECRET_KEYS", newKey+":1")
	if err := run([]string{"serve", "127.0.0.1:0"}); err == nil || !strings.Contains(err.Error(), "PROSPECTOR_SECRET_KEYS") || strings.Contains(err.Error(), newKey) {
		t.Fatalf("starting with a malformed key: %v", err)
	}
	t.Setenv("PROSPECTOR_SECRET_KEYS", "1:"+newKey)
	if err := run([]string{"serve", "127.0.0.1:0"}); err == nil || !strings.Contains(err.Error(), "PROSPECTOR_URL") {
		t.Fatalf("serve should have stopped for want of an address: %v", err)
	}

	after, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeStore(after) })
	stored := clientSecret(t, after, alpha.ID)
	if plain, err := mustKeys(t, "1:"+newKey).open(stored); err != nil || plain != "kept-in-clear" || !sealed(stored) {
		t.Fatalf("after starting with a key: %q %v", stored, err)
	}
}

func clientSecret(t *testing.T, s *Store, org int64) string {
	t.Helper()
	var c SSOConnection
	if err := s.db.Where("org_id = ?", org).First(&c).Error; err != nil {
		t.Fatal(err)
	}
	return c.ClientSecret
}

func TestTheKeyIsChangedWithoutLosingAnOrganisationsSignIn(t *testing.T) {
	s := tempStore(t)
	alpha := mustOrg(t, s, "alpha")
	mustUser(t, s, alpha, "ada@alpha.example", "Ada Lovelace", "owner")
	okta := newIdP(t, "", false)
	dns := map[string][]string{}
	_, srv := ssoServer(t, s, dns, okta)
	connected(t, srv, dns, s, "alpha", "alpha.example", okta, nil)
	okta.says(map[string]any{"sub": "o-ada", "email": "ada@alpha.example"})
	start := "/login/sso/start?org=alpha&email=ada@alpha.example"
	signsIn := func(when string) {
		t.Helper()
		if code, _, _ := newBrowser(t, srv.URL).through(okta, start); code != http.StatusSeeOther {
			t.Fatalf("signing in %s: %d", when, code)
		}
	}
	signsIn("before the key changes")
	under1 := clientSecret(t, s, alpha.ID)

	// A sign-in in flight when the key changes still comes back to a server that knows it.
	b := newBrowser(t, srv.URL)
	b.get("/login/sso")
	_, _, sentTo := b.get(start)
	s.keys = mustKeys(t, "2:"+newKey+",1:"+oldKey)
	if code, _, _ := b.get("/login/callback?" + okta.approve(sentTo).Encode()); code != http.StatusSeeOther {
		t.Fatalf("a sign-in begun before the key changed: %d", code)
	}

	// The signature outlives the browser's cookie, so an old attempt is refused by its age.
	stale, _ := json.Marshal(attempt{State: "st", Verifier: "v", Nonce: "n", Way: "sso", Org: alpha.ID, At: time.Now().Add(-attemptLife - time.Minute).Unix()})
	value := base64.RawURLEncoding.EncodeToString(stale)
	home, _ := url.Parse(srv.URL)
	b.c.Jar.SetCookies(home, []*http.Cookie{{Name: attemptCookie, Value: value + "." + s.keys.sign(attempts, value), Path: "/login"}})
	asked := okta.asked
	if code, _, _ := b.get("/login/callback?state=st&code=x&iss=" + url.QueryEscape(okta.issuer())); code != http.StatusBadRequest || okta.asked != asked {
		t.Fatalf("an attempt signed more than ten minutes ago: %d", code)
	}

	signsIn("after the new key is promoted")
	if moved, err := s.Reseal(); err != nil || moved != 1 {
		t.Fatalf("resealing: %d %v", moved, err)
	}
	under2 := clientSecret(t, s, alpha.ID)
	if under2 == under1 || s.keys.behind(under2) || sealedVersion(under2) != 2 {
		t.Fatal("the stored secret wasn't moved to the new key")
	}
	if moved, err := s.Reseal(); err != nil || moved != 0 {
		t.Fatalf("resealing again, with nothing to do: %d %v", moved, err)
	}
	// Another server, restarted while it still lists the old key first, leaves alone what
	// is already under the newer one: otherwise retiring the old key would lose it.
	s.keys = mustKeys(t, "1:"+oldKey+",2:"+newKey)
	if moved, err := s.Reseal(); err != nil || moved != 0 || clientSecret(t, s, alpha.ID) != under2 {
		t.Fatalf("a server still sealing with the old key moved a secret back: %d %v", moved, err)
	}
	// A key swapped under the same version opens nothing, and the reseal says so
	// rather than calling it current.
	s.keys = mustKeys(t, "2:"+strings.Repeat("0d", 32))
	if moved, err := s.Reseal(); moved != 0 || err == nil {
		t.Fatalf("a different key given the same version: %d %v", moved, err)
	}
	// Its owner can still change the rest, such as no longer requiring it.
	owner := newBrowser(t, srv.URL)
	owner.login("ada@alpha.example", "correct horse battery")
	same := map[string]any{"issuer": okta.srv.URL, "client_id": "client"}
	if code, body := owner.send("PUT", "/o/alpha/api/v1/sso", same); code != http.StatusOK {
		t.Fatalf("saving a connection whose kept secret can't be opened: %d %s", code, body)
	}
	if clientSecret(t, s, alpha.ID) != under2 {
		t.Fatal("saving rewrote a secret that couldn't be opened")
	}
	// But nobody is newly made to depend on a sign-in that can't work.
	for _, rule := range []string{"required", "auto_join"} {
		in := map[string]any{"issuer": okta.srv.URL, "client_id": "client", rule: true}
		if code, body := owner.send("PUT", "/o/alpha/api/v1/sso", in); code != http.StatusBadRequest || !strings.Contains(body, "type the secret again") {
			t.Fatalf("turning on %s when the kept secret can't be opened: %d %s", rule, code, body)
		}
	}
	// A typed secret is sealed whatever it looks like, even like something sealed already.
	lookalike := "eyJhbGciOiJub25lIn0.a.b.c.d"
	typed := map[string]any{"issuer": okta.srv.URL, "client_id": "client", "client_secret": lookalike}
	if code, body := owner.send("PUT", "/o/alpha/api/v1/sso", typed); code != http.StatusOK {
		t.Fatalf("typing a secret shaped like a sealed one: %d %s", code, body)
	}
	if stored := clientSecret(t, s, alpha.ID); stored == lookalike {
		t.Fatal("a typed secret shaped like a sealed one was stored as it came")
	} else if plain, err := s.keys.open(stored); err != nil || plain != lookalike {
		t.Fatalf("opening it again: %q %v", plain, err)
	}
	if err := s.db.Model(&SSOConnection{}).Where("org_id = ?", alpha.ID).Update("client_secret", under2).Error; err != nil {
		t.Fatal(err)
	}
	s.keys = mustKeys(t, "2:"+newKey)
	signsIn("after the old key is removed")

	// Removed before the reseal, the old key takes the secret with it, and says so.
	s.keys = mustKeys(t, "3:"+strings.Repeat("0c", 32))
	if moved, err := s.Reseal(); moved != 0 || !errors.Is(err, errUnknownKey) {
		t.Fatalf("resealing what no held key opens: %d %v", moved, err)
	}
	// It isn't an outage, and people aren't told to try again.
	if code, body, _ := newBrowser(t, srv.URL).through(okta, start); code != http.StatusServiceUnavailable || !strings.Contains(body, "trying again won&#39;t help") || strings.Contains(body, "key") {
		t.Fatalf("signing in when the secret can't be opened: %d", code)
	}
	if clientSecret(t, s, alpha.ID) != under2 {
		t.Fatal("a secret that couldn't be opened was overwritten")
	}
}

func TestASecretKeptInClearIsSealedOnceThereIsAKey(t *testing.T) {
	s := tempStore(t)
	alpha := mustOrg(t, s, "alpha")
	mustUser(t, s, alpha, "ada@alpha.example", "Ada Lovelace", "owner")
	okta := newIdP(t, "", false)
	dns := map[string][]string{}
	_, srv := ssoServer(t, s, dns, okta)
	connected(t, srv, dns, s, "alpha", "alpha.example", okta, nil)
	// As the release before sealing left it, on a server not yet given a key.
	if err := s.db.Model(&SSOConnection{}).Where("org_id = ?", alpha.ID).Update("client_secret", "secret").Error; err != nil {
		t.Fatal(err)
	}
	keys := s.keys
	s.keys = nil
	okta.says(map[string]any{"sub": "o-ada", "email": "ada@alpha.example"})
	if code, _, _ := newBrowser(t, srv.URL).through(okta, "/login/sso/start?org=alpha&email=ada@alpha.example"); code != http.StatusSeeOther {
		t.Fatalf("signing in with a secret kept in clear and no key: %d", code)
	}
	if moved, err := s.Reseal(); moved != 0 || !errors.Is(err, errNoKeys) {
		t.Fatalf("resealing with no key: %d %v", moved, err)
	}

	// Without a key nothing new is kept: a secret is never written in clear again.
	owner := newBrowser(t, srv.URL)
	owner.login("ada@alpha.example", "correct horse battery")
	in := map[string]any{"issuer": okta.srv.URL, "client_id": "client", "client_secret": "another"}
	if code, body := owner.send("PUT", "/o/alpha/api/v1/sso", in); code != http.StatusBadRequest || !strings.Contains(body, "PROSPECTOR_SECRET_KEYS") {
		t.Fatalf("saving a connection with no key: %d %s", code, body)
	}
	// A connection made before there was a key can still be changed, short of its secret.
	if _, body, _ := owner.get("/o/alpha/api/v1/sso"); !strings.Contains(body, `"sealing":false`) || !strings.Contains(body, `"has_client_secret":true`) || !strings.Contains(body, `"issuer":"`+okta.srv.URL+`"`) {
		t.Fatalf("Settings should still offer an existing connection's rules, and say a key is needed for a new secret: %s", body)
	}
	in = map[string]any{"issuer": okta.srv.URL, "client_id": "client", "required": true}
	if code, body := owner.send("PUT", "/o/alpha/api/v1/sso", in); code != http.StatusOK {
		t.Fatalf("changing a rule with no key: %d %s", code, body)
	}
	if sso, _ := s.SSO(alpha.ID); !sso.Required || sso.ClientSecret != "secret" {
		t.Fatalf("after changing a rule with no key: %+v", sso.SSOConnection)
	}
	if code, body := owner.send("PUT", "/o/alpha/api/v1/sso", map[string]any{"issuer": ""}); code != http.StatusOK {
		t.Fatalf("turning single sign-on off needs no key: %d %s", code, body)
	}
	// Domains need no key, so they stay in reach with no key and no connection.
	_, sso, _ := owner.get("/o/alpha/api/v1/sso")
	code, domains, _ := owner.get("/o/alpha/api/v1/sso/domains")
	if !strings.Contains(sso, `"sealing":false`) || code != http.StatusOK || !strings.Contains(domains, `"domain":"alpha.example"`) {
		t.Fatalf("with no key and no connection, Settings should say a key is needed and still list the domains: %s %d %s", sso, code, domains)
	}
	if err := s.db.Create(&SSOConnection{OrgID: alpha.ID, Issuer: okta.srv.URL, ClientID: "client", ClientSecret: "secret"}).Error; err != nil {
		t.Fatal(err)
	}

	s.keys = keys
	if moved, err := s.Reseal(); err != nil || moved != 1 {
		t.Fatalf("sealing what was kept in clear: %d %v", moved, err)
	}
	if stored := clientSecret(t, s, alpha.ID); !sealed(stored) || strings.Contains(stored, "secret") {
		t.Fatalf("still in clear: %q", stored)
	}
	if code, _, _ := newBrowser(t, srv.URL).through(okta, "/login/sso/start?"+url.Values{"org": {"alpha"}, "email": {"ada@alpha.example"}}.Encode()); code != http.StatusSeeOther {
		t.Fatalf("signing in once it is sealed: %d", code)
	}
}
