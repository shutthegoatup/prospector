package main

import (
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

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
	} {
		_, err := parseKeys(spec)
		if err == nil {
			t.Errorf("%s was accepted", what)
		} else if strings.Contains(err.Error(), "0a0b") || strings.Contains(err.Error(), "zz") {
			t.Errorf("%s: the refusal quotes the key: %v", what, err)
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
	if plain, err := published.open(stored); err != nil || plain != "s3cret-value" || !published.underCurrent(stored) {
		t.Fatalf("after the new key is published: %q %v", plain, err)
	}
	// Promoted: the new key seals, and the old one still opens.
	promoted := mustKeys(t, "2:"+newKey+",1:"+oldKey)
	if plain, err := promoted.open(stored); err != nil || plain != "s3cret-value" || promoted.underCurrent(stored) {
		t.Fatalf("after the new key is promoted: %q %v", plain, err)
	}
	// Retired too early: a value still under the old key says which key it needs.
	retired := mustKeys(t, "2:"+newKey)
	if _, err := retired.open(stored); !errors.Is(err, errUnknownKey) {
		t.Fatalf("a value under a key no longer held: %v", err)
	}

	moved, _ := promoted.seal("s3cret-value")
	tampered := moved[:len(moved)-2] + "AA"
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

	signsIn("after the new key is promoted")
	if moved, err := s.Reseal(); err != nil || moved != 1 {
		t.Fatalf("resealing: %d %v", moved, err)
	}
	under2 := clientSecret(t, s, alpha.ID)
	if under2 == under1 || !s.keys.underCurrent(under2) {
		t.Fatal("the stored secret wasn't moved to the new key")
	}
	if moved, err := s.Reseal(); err != nil || moved != 0 {
		t.Fatalf("resealing again, with nothing to do: %d %v", moved, err)
	}
	s.keys = mustKeys(t, "2:"+newKey)
	signsIn("after the old key is removed")

	// Removed before the reseal, the old key takes the secret with it, and says so.
	s.keys = mustKeys(t, "3:"+strings.Repeat("0c", 32))
	if moved, err := s.Reseal(); moved != 0 || !errors.Is(err, errUnknownKey) {
		t.Fatalf("resealing what no held key opens: %d %v", moved, err)
	}
	if code, body, _ := newBrowser(t, srv.URL).through(okta, start); code != http.StatusBadGateway || strings.Contains(body, "key") {
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
	if code, body := owner.post("/o/alpha/ui/sso", in); code != http.StatusBadRequest || !strings.Contains(body, "PROSPECTOR_SECRET_KEYS") {
		t.Fatalf("saving a connection with no key: %d %s", code, body)
	}
	if _, page, _ := owner.get("/o/alpha/settings"); !strings.Contains(page, "PROSPECTOR_SECRET_KEYS") || strings.Contains(page, `name="client_secret"`) {
		t.Fatal("Settings should say a key is needed, not offer the form")
	}
	if code, body := owner.post("/o/alpha/ui/sso", map[string]any{"issuer": ""}); code != http.StatusOK {
		t.Fatalf("turning single sign-on off needs no key: %d %s", code, body)
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
