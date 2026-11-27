package prospecting

import (
	"time"

	"gorm.io/gorm"

	"github.com/shutthegoatup/prospector/internal/store"
)

// ResearchRow is one company in the Research tab with what's known about it.
type ResearchRow struct {
	store.Company
	Facts      []store.Fact
	People     []store.Contact
	Pending    map[int64][]PendingDetail
	Lookups    map[int64][]Lookup
	Incomplete bool
}

var ResearchFilters = []struct{ Key, Label string }{
	{"review", "To review"}, {"contact", "Needs a contact"}, {"verify", "Needs verifying"}, {"details", "Details to check"}, {"rejected", "Rejected"},
}

const ResearchPageSize = 30

// searchFitCap is how many companies to review one search may have and still be ranked
// by fit; past it, they come newest first as everywhere else.
const searchFitCap = 500

func furthest(p store.Contact) int {
	switch {
	case p.Declined != nil:
		// Someone who declined before anyone reached them was never in the pipeline.
		if p.Touched() || p.Replied != nil {
			return 2
		}
		return 0
	case p.Replied != nil:
		switch p.Stage {
		case "lost":
			return 1
		case "meeting":
			return 4
		case "proposal":
			return 5
		case "won":
			return 6
		}
		return 3
	case p.Touched():
		return 2
	}
	return 0
}

// Pipeline is where a company stands, from its own stage and its furthest contact.
func Pipeline(c store.Company, people []store.Contact) string {
	best := 0
	for _, p := range people {
		best = max(best, furthest(p))
	}
	return pipelineAt(c, best)
}

type AgentEvent struct {
	At     time.Time
	Actor  string
	Action string
	Value  string
}

const ActivityPageSize = 20

// notListed keeps the companies the list covers out of a query, as coversCompany does.
func notListed(q *gorm.DB, set store.OrgSettings) *gorm.DB {
	for _, host := range set.NeverDomains() {
		q = q.Where("not (lower(domain) = ? or lower(domain) like ?)", host, "%."+host)
	}
	return q
}
