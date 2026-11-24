package main

import (
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/go-jose/go-jose/v4"
)

// purpose is a job one derived key does, and the HKDF info (RFC 5869 §3.2) that
// derives it, so the master key itself is never used and no key does two jobs.
type purpose string

const (
	// sealing encrypts what the database holds but mustn't be able to read by itself.
	sealing purpose = "prospector: sealed secrets"
	// attempts signs the cookie that carries a sign-in to a provider and back.
	attempts purpose = "prospector: sign-in attempts"
)

var purposes = []purpose{sealing, attempts}

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
	// header is how every value sealed under the current key begins.
	header string
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
			return nil, fmt.Errorf("PROSPECTOR_SECRET_KEYS: version %q isn't a plain whole number", version)
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
	probe, err := k.seal("")
	if err != nil {
		return nil, err
	}
	k.header = probe[:strings.IndexByte(probe, '.')+1]
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

func (k *keyring) underCurrent(stored string) bool {
	return k != nil && strings.HasPrefix(stored, k.header)
}

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

// Reseal moves every stored secret onto the current key, sealing for the first time any
// stored before there was one. Once it reports nothing left, an older key can be removed.
func (s *Store) Reseal() (moved int, err error) {
	var conns []SSOConnection
	if err := s.db.Where("client_secret <> ''").Find(&conns).Error; err != nil {
		return 0, err
	}
	var failed []error
	for _, c := range conns {
		if s.keys.underCurrent(c.ClientSecret) {
			continue
		}
		plain, err := s.keys.open(c.ClientSecret)
		if err == nil {
			var again string
			if again, err = s.keys.seal(plain); err == nil {
				// Only the value that was read is replaced, so an owner's change made meanwhile wins.
				err = s.db.Model(&SSOConnection{}).Where("org_id = ? and client_secret = ?", c.OrgID, c.ClientSecret).Update("client_secret", again).Error
			}
		}
		if err != nil {
			failed = append(failed, fmt.Errorf("organisation %d's single sign-on secret: %w", c.OrgID, err))
			continue
		}
		moved++
	}
	return moved, errors.Join(failed...)
}

// resealAtStart runs when the server starts, since a new key only ever arrives with a restart.
func (s *Store) resealAtStart() {
	moved, err := s.Reseal()
	if err != nil {
		log.Printf("secrets: %v", err)
	}
	switch {
	case s.keys == nil:
	case err == nil:
		log.Printf("secrets: every stored secret is sealed under key %s (%d moved to it now); older keys can be removed", s.keys.keys[0].kid, moved)
	default:
		log.Printf("secrets: %d moved to key %s, but not all: keep the older keys", moved, s.keys.keys[0].kid)
	}
}
