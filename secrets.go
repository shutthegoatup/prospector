package main

import (
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"

	"gorm.io/gorm"
)

const (

	// attempts signs the cookie that carries a sign-in to a provider and back.
	attempts purpose = "prospector: sign-in attempts"
	// sessions and apiKeys key what a session token and an API key are stored and found by.
	sessions purpose = "prospector: sessions"
	apiKeys  purpose = "prospector: API keys"
)

var purposes = []purpose{sealing, attempts, sessions, apiKeys}

const keySize = 32

// parseKeys reads PROSPECTOR_SECRET_KEYS: comma-separated "<version>:<64 hex>" entries.
// Empty means no keys, and a nil keyring.
func parseKeys(spec string) (*keyring, error) {
	if strings.TrimSpace(spec) == "" {
		return nil, nil
	}
	k := &keyring{ByKID: map[string]secretKey{}}
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
		if _, twice := k.ByKID[version]; twice {
			return nil, fmt.Errorf("PROSPECTOR_SECRET_KEYS: version %d is given twice", v)
		}
		// Not wrapped: the error would quote the key.
		master, err := hex.DecodeString(material)
		if err != nil || len(master) != keySize {
			return nil, fmt.Errorf("PROSPECTOR_SECRET_KEYS: version %d must be %d hex characters", v, 2*keySize)
		}
		key := secretKey{Kid: version, Derived: map[purpose][]byte{}}
		for _, p := range purposes {
			if key.Derived[p], err = hkdf.Key(sha256.New, master, nil, string(p), keySize); err != nil {
				return nil, err
			}
		}
		k.Versions = append(k.Versions, key)
		k.ByKID[version] = key
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
	{"seal_keys", "key", "agreement sealing key", "org_id"},
}

type storedSecret struct {
	RowID  int64
	OrgID  int64
	Secret string
}

func (s *Store) stored(in sealedColumn) *gorm.DB {
	return s.DB.Table(in.table).Select(in.row + " as row_id, org_id, " + in.column + " as secret").Where(in.column + " <> ''")
}

// Reseal moves every stored secret onto the current key, sealing for the first time any
// stored before there was one. Once it reports nothing left, an older key can be removed.
func (s *Store) Reseal() (moved int, err error) {
	if s.Keyring == nil {
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
					if _, err = s.Keyring.Open(now[0].Secret); err == nil && s.Keyring.Behind(now[0].Secret) {
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
	plain, err := s.Keyring.Open(row.Secret)
	if err != nil || !s.Keyring.Behind(row.Secret) {
		return false, false, err
	}
	again, err := s.Keyring.Seal(plain)
	if err != nil {
		return false, false, err
	}
	// Only the value that was read is replaced, so an owner's change made meanwhile wins.
	res := s.DB.Table(in.table).Where(in.row+" = ? and "+in.column+" = ?", row.RowID, row.Secret).Update(in.column, again)
	return res.RowsAffected > 0, res.RowsAffected == 0, res.Error
}

// ResealAtStart runs when the server starts, since a new key only ever arrives with a restart.
func (s *Store) ResealAtStart() {
	if s.Keyring == nil {
		var clear int64
		_ = logged(s.DB.Model(&SSOConnection{}).Where("client_secret <> ''").Count(&clear))
		if clear > 0 {
			log.Printf("secrets: PROSPECTOR_SECRET_KEYS isn't set: single sign-on secrets kept as stored, which none can be changed from: %d", clear)
		}
		return
	}
	moved, err := s.Reseal()
	current := s.Keyring.Versions[0].Kid
	if err != nil {
		log.Printf("secrets: %v", err)
		log.Printf("secrets: not every stored secret is under key %s: keep the older keys", current)
	} else {
		log.Printf("secrets: every stored secret opens, and none is under a key older than %s (%d moved to it now)", current, moved)
	}
	// An API key can't be moved until it is next shown, so an old key that is removed
	// first takes with it whichever haven't been used since.
	var hashes []string
	_ = logged(s.DB.Model(&APIKey{}).Where("revoked_at is null").Pluck("hash", &hashes))
	waiting := 0
	for _, h := range hashes {
		if s.Keyring.LookupBehind(h) {
			waiting++
		}
	}
	if waiting > 0 {
		log.Printf("secrets: API keys still found by an older key or none: %d. Each moves to key %s when it is next used, and stops working if its key is removed first", waiting, current)
	}
}
