package store

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/shutthegoatup/prospector/internal/say"
)

// Erasure is what stays behind when a person is erased: hashes of what identified
// them, so an import, a restore or an agent can't bring them back. The hash is salted
// with the organisation, so the same address erased in two can't be matched up.
type Erasure struct {
	ID    int64  `gorm:"primaryKey"`
	OrgID int64  `gorm:"uniqueIndex:idx_erasure;not null"`
	Hash  string `gorm:"uniqueIndex:idx_erasure;not null"`
	At    time.Time
}

var ErrErased = say.New("this person was erased on request and can't be added again")

var EmailRe = regexp.MustCompile(`[^\s,;:<>()"']+@[^\s,;:<>()"']+`)

// Identifiers are the hashes for a name at a company, the addresses in an email field
// and a LinkedIn profile. A name alone would ban a namesake elsewhere, and only a
// profile counts as a link: every company placeholder shares one search link.
func Identifiers(org int64, person, maker, email, link string) []string {
	var out []string
	add := func(kind, v string) {
		if v != "" {
			sum := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%s\x00%s", org, kind, v)))
			out = append(out, hex.EncodeToString(sum[:]))
		}
	}
	if name, co := strings.ToLower(PersonKey(person)), strings.ToLower(CompanyName(maker)); name != "" && co != "" {
		add("name", name+"\x00"+co)
	}
	for _, e := range EmailRe.FindAllString(strings.ToLower(email), -1) {
		add("email", strings.Trim(e, "."))
	}
	if p := strings.Split(LinkedinPath(link), "/"); len(p) >= 2 && p[0] == "in" {
		add("linkedin", p[1])
	}
	return out
}
