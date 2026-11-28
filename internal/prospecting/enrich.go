package prospecting

import (
	"context"
	"maps"
	"net/http"
	"slices"
	"time"
)

// EnrichPerson is what a vendor is told about someone: no more than it needs to find them.
type EnrichPerson struct {
	Name, Domain, Company, Linkedin string
}

// VendorFind is what a vendor found for one field; an empty Value is a miss.
type VendorFind struct {
	Value       string
	EmailStatus string
}

type vendor interface {
	Name() string
	Fields() []string
	Find(ctx context.Context, c *http.Client, base, key string, p EnrichPerson, field string) (VendorFind, error)
}

// Vendors are the enrichment services an organisation can pick, by kind.
var Vendors = map[string]vendor{"hunter": hunter{}, "prospeo": prospeo{}}

func vendorKinds() []string { return slices.Sorted(maps.Keys(Vendors)) }

const EnrichAgain = 30 * 24 * time.Hour

const maxMonthlyLimit = 100_000

type EnricherIn struct {
	Kind         string
	APIKey       string
	Position     int
	MonthlyLimit int
}

const LookupFound = "found"

// Lookup is a vendor's latest answer about one detail of one person.
type Lookup struct {
	Field, Vendor, Outcome string
	At                     time.Time
}
