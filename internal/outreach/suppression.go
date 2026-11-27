package outreach

import (
	"fmt"

	"github.com/shutthegoatup/prospector/internal/say"
	"github.com/shutthegoatup/prospector/internal/store"
)

const SuppressUnsubscribed = "unsubscribed"

const SuppressAdded = "added"

// suppressRank orders the reasons: a stronger one replaces a weaker, never the other
// way. Their own word outranks a member's, which outranks a bounce alone.
var suppressRank = map[string]int{SuppressBounced: 0, SuppressAdded: 1, SuppressUnsubscribed: 2, SuppressComplained: 3}

var (
	ErrSuppressed = say.New("that address is on the list not to be mailed")
)

// MailGate holds what keeps marketing mail from an organisation's addresses, read once,
// so a whole audience is checked without a query a person.
type MailGate struct {
	o          *store.Org
	suppressed map[string]store.Suppression
	erased     map[string]bool
}

func (g *MailGate) why(address, link string) error {
	return g.WhyFor(KindMarketing, address, link)
}

// WhyFor is why for a campaign of a kind. A service notice reaches someone who
// unsubscribed from marketing, since it is about what they have; never an address that
// bounced or complained, anyone on the never-contact list, or anyone erased.
func (g *MailGate) WhyFor(kind, address, link string) error {
	addr, err := store.BareAddress(address)
	if err != nil {
		return err
	}
	// Mail that can't arrive, or that they called spam, stops service notices too.
	switch l, listed := g.suppressed[addr]; {
	case !listed:
	case l.Reason == SuppressComplained:
		return ErrComplained
	case l.BouncedAt != nil:
		return ErrBounced
	case kind != store.KindService:
		return ErrSuppressed
	}
	if err := g.o.Settings.Listed(addr, link); err != nil {
		return fmt.Errorf("%w: %v", store.ErrNeverContact, err)
	}
	for _, h := range store.Identifiers(g.o.ID, "", "", addr, link) {
		if g.erased[h] {
			return store.ErrErased
		}
	}
	return nil
}
