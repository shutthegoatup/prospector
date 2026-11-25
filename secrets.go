package main

import (
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/go-jose/go-jose/v4"
	"gorm.io/gorm"
)

// purpose is a job one derived key does, and the HKDF info (RFC 5869 §3.2) that
// derives it, so the master key itself is never used and no key does two jobs.
type purpose string

const (
	// sealing encrypts what the database holds but mustn't be able to read by itself.
	sealing purpose = "prospector: sealed secrets"
	// attempts signs the cookie that carries a sign-in to a provider and back.
	attempts purpose = "prospector: sign-in attempts"
	// sessions and apiKeys key what a session token and an API key are stored and found by.
	sessions purpose = "prospector: sessions"
	apiKeys  purpose = "prospector: API keys"
)

var purposes = []purpose{sealing, attempts, sessions, apiKeys}

const keySize = 32

var (
	errNoKeys     = errors.New("no secret key is configured: set PROSPECTOR_SECRET_KEYS (`prospector secret-key` makes one)")
	errUnknownKey = errors.New("sealed under a secret key this server no longer holds")
)

type secretKey struct {
	kid     string
	derived map[purpose][]byte
}

// keyring is the server's master keys, newest first. The first seals and signs; every
// one opens and verifies what it made, which is what lets a key be replaced.
type keyring struct {
	keys  []secretKey
	byKID map[string]secretKey
}

// parseKeys reads PROSPECTOR_SECRET_KEYS: comma-separated "<version>:<64 hex>" entries.
// Empty means no keys, and a nil keyring.
func parseKeys(spec string) (*keyring, error) {
	if strings.TrimSpace(spec) == "" {
		return nil, nil
	}
	k := &keyring{byKID: map[string]secretKey{}}
	for _, entry := range strings.Split(spec, ",") {
		version, material, ok := strings.Cut(strings.TrimSpace(entry), ":")
		if !ok {
			return nil, errors.New(`PROSPECTOR_SECRET_KEYS: an entry looks like "<version>:<64 hex characters>"`)
		}
		v, err := strconv.Atoi(version)
		if err != nil || v < 0 || strconv.Itoa(v) != version {
			// The entry isn't quoted: written the wrong way round, its version is the key.
			return nil, errors.New(`PROSPECTOR_SECRET_KEYS: a version is a plain whole number, as in "1:<64 hex characters>"`)
		}
		if _, twice := k.byKID[version]; twice {
			return nil, fmt.Errorf("PROSPECTOR_SECRET_KEYS: version %d is given twice", v)
		}
		// Not wrapped: the error would quote the key.
		master, err := hex.DecodeString(material)
		if err != nil || len(master) != keySize {
			return nil, fmt.Errorf("PROSPECTOR_SECRET_KEYS: version %d must be %d hex characters", v, 2*keySize)
		}
		key := secretKey{kid: version, derived: map[purpose][]byte{}}
		for _, p := range purposes {
			if key.derived[p], err = hkdf.Key(sha256.New, master, nil, string(p), keySize); err != nil {
				return nil, err
			}
		}
		k.keys = append(k.keys, key)
		k.byKID[version] = key
	}
	return k, nil
}

func newSecretKey() string {
	b := make([]byte, keySize)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// seal is a compact JWE (RFC 7516): AES-256-GCM directly under the current sealing
// key, whose version the header names as its kid. Any JOSE library opens it given the key.
func (k *keyring) seal(plain string) (string, error) {
	if k == nil {
		return "", errNoKeys
	}
	current := k.keys[0]
	enc, err := jose.NewEncrypter(jose.A256GCM, jose.Recipient{Algorithm: jose.DIRECT, Key: current.derived[sealing], KeyID: current.kid}, nil)
	if err != nil {
		return "", err
	}
	obj, err := enc.Encrypt([]byte(plain))
	if err != nil {
		return "", err
	}
	return obj.CompactSerialize()
}

// sealed is a stored value that is a JWE. A protected header is a JSON object, so its
// base64url always begins eyJ. Anything else was stored before there was a key.
func sealed(stored string) bool {
	return strings.HasPrefix(stored, "eyJ") && strings.Count(stored, ".") == 4
}

// open returns what a stored value was sealed from, by whichever key sealed it. One
// stored in clear, before there was a key, comes back as it is.
func (k *keyring) open(stored string) (string, error) {
	if !sealed(stored) {
		return stored, nil
	}
	if k == nil {
		return "", errNoKeys
	}
	obj, err := jose.ParseEncryptedCompact(stored, []jose.KeyAlgorithm{jose.DIRECT}, []jose.ContentEncryption{jose.A256GCM})
	if err != nil {
		return "", fmt.Errorf("reading a sealed secret: %w", err)
	}
	key, ok := k.byKID[obj.Header.KeyID]
	if !ok {
		return "", fmt.Errorf("key %q: %w", obj.Header.KeyID, errUnknownKey)
	}
	plain, err := obj.Decrypt(key.derived[sealing])
	if err != nil {
		return "", fmt.Errorf("opening a sealed secret: %w", err)
	}
	return string(plain), nil
}

// plainVersion reads a version written the one way parseKeys takes it. -1 otherwise.
func plainVersion(kid string) int {
	if v, err := strconv.Atoi(kid); err == nil && v >= 0 && strconv.Itoa(v) == kid {
		return v
	}
	return -1
}

// sealedVersion is the key version a sealed secret's header names. -1 for a value
// kept in clear, whatever it looks like.
func sealedVersion(stored string) int {
	if !sealed(stored) {
		return -1
	}
	var header struct {
		KID string `json:"kid"`
	}
	raw, _ := base64.RawURLEncoding.DecodeString(stored[:strings.IndexByte(stored, '.')])
	json.Unmarshal(raw, &header)
	return plainVersion(header.KID)
}

// lookupVersion is the key version a token's lookup names, as "k<version>:<hmac>". -1
// for a plain hash.
func lookupVersion(stored string) int {
	rest, ok := strings.CutPrefix(stored, "k")
	kid, _, colon := strings.Cut(rest, ":")
	if !ok || !colon {
		return -1
	}
	return plainVersion(kid)
}

func (k *keyring) version() int { return plainVersion(k.keys[0].kid) }

// behind is a sealed secret under an older key than this server uses, or one kept in
// clear. One under a newer key isn't: another server was given the next key first,
// and its work isn't to be undone.
func (k *keyring) behind(stored string) bool {
	return k != nil && sealedVersion(stored) < k.version()
}

// lookupBehind is behind, for a token's lookup.
func (k *keyring) lookupBehind(stored string) bool {
	return k != nil && lookupVersion(stored) < k.version()
}

// lookups is what a token is stored and found by: its HMAC under each key held, the
// current one first, each naming its key. A token made before a key was replaced is
// still found until that key is removed. With no key it is the token's SHA-256.
func (k *keyring) lookups(p purpose, token string) []string {
	if k == nil {
		return []string{hashToken(token)}
	}
	out := make([]string, 0, len(k.keys))
	for _, key := range k.keys {
		out = append(out, "k"+key.kid+":"+mac(key.derived[p], token))
	}
	return out
}

// lookup is the one a new token is stored under.
func (k *keyring) lookup(p purpose, token string) string { return k.lookups(p, token)[0] }

func mac(key []byte, message string) string {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(message))
	return hex.EncodeToString(h.Sum(nil))
}

// sign is an HMAC under the current key's key for p.
func (k *keyring) sign(p purpose, message string) string {
	return mac(k.keys[0].derived[p], message)
}

// verify accepts a signature made under any key held, so what was signed before a key
// was replaced stays good until that key is removed.
func (k *keyring) verify(p purpose, message, signature string) bool {
	ok := false
	for _, key := range k.keys {
		ok = hmac.Equal([]byte(mac(key.derived[p], message)), []byte(signature)) || ok
	}
	return ok
}

// sealedColumn is where one kind of sealed secret is stored, a row to an organisation.
// sealedColumn is where one kind of sealed secret is stored, and the column that tells
// its rows apart: an organisation's, where it has one, or each row's own id.
type sealedColumn struct {
	table             string
	column, what, row string
}

var sealedColumns = []sealedColumn{
	{"sso_connections", "client_secret", "single sign-on secret", "org_id"},
	{"org_mails", "password", "mail password", "org_id"},
	{"agents", "api_key", "agent endpoint key", "id"},
}

type storedSecret struct {
	RowID  int64
	OrgID  int64
	Secret string
}

func (s *Store) stored(in sealedColumn) *gorm.DB {
	return s.db.Table(in.table).Select(in.row + " as row_id, org_id, " + in.column + " as secret").Where(in.column + " <> ''")
}

// Reseal moves every stored secret onto the current key, sealing for the first time any
// stored before there was one. Once it reports nothing left, an older key can be removed.
func (s *Store) Reseal() (moved int, err error) {
	if s.keys == nil {
		return 0, errNoKeys
	}
	var failed []error
	for _, in := range sealedColumns {
		var rows []storedSecret
		if err := s.stored(in).Scan(&rows).Error; err != nil {
			return moved, err
		}
		for _, row := range rows {
			was, raced, err := s.resealOne(in, row)
			// Somebody else changed it meanwhile: another server sealing it too, or its owner.
			// What is there now is what counts.
			if err == nil && raced {
				var now []storedSecret
				if err = s.stored(in).Where(in.row+" = ?", row.RowID).Scan(&now).Error; err == nil && len(now) > 0 {
					if _, err = s.keys.open(now[0].Secret); err == nil && s.keys.behind(now[0].Secret) {
						err = errors.New("changed while it was being sealed again, and is still under an older key: restart to move it")
					}
				}
			}
			if err != nil {
				failed = append(failed, fmt.Errorf("organisation %d's %s: %w", row.OrgID, in.what, err))
			} else if was {
				moved++
			}
		}
	}
	return moved, errors.Join(failed...)
}

// resealOne moves one stored secret to the current key, if it isn't under it already.
// raced is the row having changed between being read and being written.
func (s *Store) resealOne(in sealedColumn, row storedSecret) (moved, raced bool, err error) {
	// One that names the current key is still opened: a key replaced under the same
	// version would otherwise pass for current and open nothing.
	plain, err := s.keys.open(row.Secret)
	if err != nil || !s.keys.behind(row.Secret) {
		return false, false, err
	}
	again, err := s.keys.seal(plain)
	if err != nil {
		return false, false, err
	}
	// Only the value that was read is replaced, so an owner's change made meanwhile wins.
	res := s.db.Table(in.table).Where(in.row+" = ? and "+in.column+" = ?", row.RowID, row.Secret).Update(in.column, again)
	return res.RowsAffected > 0, res.RowsAffected == 0, res.Error
}

// resealAtStart runs when the server starts, since a new key only ever arrives with a restart.
func (s *Store) resealAtStart() {
	if s.keys == nil {
		var clear int64
		logged(s.db.Model(&SSOConnection{}).Where("client_secret <> ''").Count(&clear))
		if clear > 0 {
			log.Printf("secrets: PROSPECTOR_SECRET_KEYS isn't set: single sign-on secrets kept as stored, which none can be changed from: %d", clear)
		}
		return
	}
	moved, err := s.Reseal()
	current := s.keys.keys[0].kid
	if err != nil {
		log.Printf("secrets: %v", err)
		log.Printf("secrets: not every stored secret is under key %s: keep the older keys", current)
	} else {
		log.Printf("secrets: every stored secret opens, and none is under a key older than %s (%d moved to it now)", current, moved)
	}
	// An API key can't be moved until it is next shown, so an old key that is removed
	// first takes with it whichever haven't been used since.
	var hashes []string
	logged(s.db.Model(&APIKey{}).Where("revoked_at is null").Pluck("hash", &hashes))
	waiting := 0
	for _, h := range hashes {
		if s.keys.lookupBehind(h) {
			waiting++
		}
	}
	if waiting > 0 {
		log.Printf("secrets: API keys still found by an older key or none: %d. Each moves to key %s when it is next used, and stops working if its key is removed first", waiting, current)
	}
}
