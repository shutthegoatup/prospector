package agreements

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/shutthegoatup/prospector/internal/say"
	"github.com/shutthegoatup/prospector/internal/store"
)

var ErrBrokenChain = say.New("the agreement's record has been changed since it was written")

// EventHash is the SHA-256 of everything an event says, the hash before it included.
func EventHash(e store.AgreementEvent) string {
	recipient := int64(0)
	if e.RecipientID != nil {
		recipient = *e.RecipientID
	}
	// A fixed list in a fixed order: what is hashed can't change with how the struct does.
	b, _ := json.Marshal([]string{
		strconv.FormatInt(e.OrgID, 10), strconv.FormatInt(e.AgreementID, 10), strconv.Itoa(e.Round),
		e.At.UTC().Format(time.RFC3339Nano), e.Kind, e.Actor, strconv.FormatInt(recipient, 10),
		e.Detail, e.IP, e.UserAgent, e.Prev,
	})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// CheckChain is whether events, in order, each follow the one before and say what their
// hash says they did.
func CheckChain(events []store.AgreementEvent) error {
	prev := ""
	for _, e := range events {
		if e.Prev != prev || EventHash(e) != e.Hash {
			return fmt.Errorf("agreement %d, event %d: %w", e.AgreementID, e.ID, ErrBrokenChain)
		}
		prev = e.Hash
	}
	return nil
}
