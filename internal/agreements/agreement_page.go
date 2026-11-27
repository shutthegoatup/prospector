package agreements

import (
	"github.com/shutthegoatup/prospector/internal/identity"
	"github.com/shutthegoatup/prospector/internal/store"
)

// PreparePage is a draft being set out for signing: who signs and gets a copy, the people
// and members who could be added, and what it still lacks.
type PreparePage struct {
	Agreement store.Agreement
	Company   store.Company
	Parties   []Party
	People    []store.Contact
	Members   []identity.Member
	Missing   []string
	Problem   string
}

// On is whether a contact or member is on the agreement already, so they aren't offered twice.
func (p PreparePage) On(contact, member int64) bool {
	for _, r := range p.Parties {
		if (r.ContactID != nil && *r.ContactID == contact) || (r.UserID != nil && *r.UserID == member) {
			return true
		}
	}
	return false
}
