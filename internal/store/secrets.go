package store

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/go-jose/go-jose/v4"

	"github.com/shutthegoatup/prospector/internal/say"
)

// Purpose is a job one derived key does, and the HKDF info (RFC 5869 §3.2) that
// derives it, so the master key itself is never used and no key does two jobs.
type Purpose string

const // Sealing encrypts what the database holds but mustn't be able to read by itself.
Sealing Purpose = "prospector: sealed secrets"

var ErrNoKeys = say.New("no secret key is configured: set PROSPECTOR_SECRET_KEYS (`prospector secret-key` makes one)")

var ErrUnknownKey = say.New("sealed under a secret key this server no longer holds")

type SecretKey struct {
	Kid     string
	Derived map[Purpose][]byte
}

// Keyring is the server's master keys, newest first. The first seals and signs; every
// one opens and verifies what it made, which is what lets a key be replaced.
type Keyring struct {
	Versions []SecretKey
	ByKID    map[string]SecretKey
}

// Seal is a compact JWE (RFC 7516): AES-256-GCM directly under the current sealing
// key, whose version the header names as its kid. Any JOSE library opens it given the key.
func (k *Keyring) Seal(plain string) (string, error) {
	if k == nil {
		return "", ErrNoKeys
	}
	current := k.Versions[0]
	enc, err := jose.NewEncrypter(jose.A256GCM, jose.Recipient{Algorithm: jose.DIRECT, Key: current.Derived[Sealing], KeyID: current.Kid}, nil)
	if err != nil {
		return "", err
	}
	obj, err := enc.Encrypt([]byte(plain))
	if err != nil {
		return "", err
	}
	return obj.CompactSerialize()
}

// Sealed is a stored value that is a JWE. A protected header is a JSON object, so its
// base64url always begins eyJ. Anything else was stored before there was a key.
func Sealed(stored string) bool {
	return strings.HasPrefix(stored, "eyJ") && strings.Count(stored, ".") == 4
}

// Open returns what a stored value was sealed from, by whichever key sealed it. One
// stored in clear, before there was a key, comes back as it is.
func (k *Keyring) Open(stored string) (string, error) {
	if !Sealed(stored) {
		return stored, nil
	}
	if k == nil {
		return "", ErrNoKeys
	}
	obj, err := jose.ParseEncryptedCompact(stored, []jose.KeyAlgorithm{jose.DIRECT}, []jose.ContentEncryption{jose.A256GCM})
	if err != nil {
		return "", fmt.Errorf("reading a sealed secret: %w", err)
	}
	key, ok := k.ByKID[obj.Header.KeyID]
	if !ok {
		return "", fmt.Errorf("key %q: %w", obj.Header.KeyID, ErrUnknownKey)
	}
	plain, err := obj.Decrypt(key.Derived[Sealing])
	if err != nil {
		return "", fmt.Errorf("opening a sealed secret: %w", err)
	}
	return string(plain), nil
}

// PlainVersion reads a version written the one way parseKeys takes it. -1 otherwise.
func PlainVersion(kid string) int {
	if v, err := strconv.Atoi(kid); err == nil && v >= 0 && strconv.Itoa(v) == kid {
		return v
	}
	return -1
}

// SealedVersion is the key version a sealed secret's header names. -1 for a value
// kept in clear, whatever it looks like.
func SealedVersion(stored string) int {
	if !Sealed(stored) {
		return -1
	}
	var header struct {
		KID string `json:"kid"`
	}
	raw, _ := base64.RawURLEncoding.DecodeString(stored[:strings.IndexByte(stored, '.')])
	_ = json.Unmarshal(raw, &header)
	return PlainVersion(header.KID)
}

// LookupVersion is the key version a token's lookup names, as "k<version>:<hmac>". -1
// for a plain hash.
func LookupVersion(stored string) int {
	rest, ok := strings.CutPrefix(stored, "k")
	kid, _, colon := strings.Cut(rest, ":")
	if !ok || !colon {
		return -1
	}
	return PlainVersion(kid)
}

func (k *Keyring) Version() int { return PlainVersion(k.Versions[0].Kid) }

// Behind is a sealed secret under an older key than this server uses, or one kept in
// clear. One under a newer key isn't: another server was given the next key first,
// and its work isn't to be undone.
func (k *Keyring) Behind(stored string) bool {
	return k != nil && SealedVersion(stored) < k.Version()
}

// LookupBehind is behind, for a token's lookup.
func (k *Keyring) LookupBehind(stored string) bool {
	return k != nil && LookupVersion(stored) < k.Version()
}

// Lookups is what a token is stored and found by: its HMAC under each key held, the
// current one first, each naming its key. A token made before a key was replaced is
// still found until that key is removed. With no key it is the token's SHA-256.
func (k *Keyring) Lookups(p Purpose, token string) []string {
	if k == nil {
		return []string{HashToken(token)}
	}
	out := make([]string, 0, len(k.Versions))
	for _, key := range k.Versions {
		out = append(out, "k"+key.Kid+":"+Mac(key.Derived[p], token))
	}
	return out
}

// Lookup is the one a new token is stored under.
func (k *Keyring) Lookup(p Purpose, token string) string { return k.Lookups(p, token)[0] }

func Mac(key []byte, message string) string {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(message))
	return hex.EncodeToString(h.Sum(nil))
}

// Sign is an HMAC under the current key's key for p.
func (k *Keyring) Sign(p Purpose, message string) string {
	return Mac(k.Versions[0].Derived[p], message)
}

// Verify accepts a signature made under any key held, so what was signed before a key
// was replaced stays good until that key is removed.
func (k *Keyring) Verify(p Purpose, message, signature string) bool {
	ok := false
	for _, key := range k.Versions {
		ok = hmac.Equal([]byte(Mac(key.Derived[p], message)), []byte(signature)) || ok
	}
	return ok
}
