package store

import (
	"testing"
	"time"
)

func TestAddressDomain(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"ada@larkmoor.example":    "larkmoor.example",
		" Ada@Larkmoor.Example. ": "larkmoor.example",
		`"a@b"@larkmoor.example`:  "larkmoor.example",
		"ada@bücher.example":      "xn--bcher-kva.example",
		"ada@localhost":           "",
		"no address":              "",
		"ada@":                    "",
	} {
		if got := AddressDomain(in); got != want {
			t.Errorf("AddressDomain(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestSignOnProvesOnlyAddressesAtItsDomains: the same domain whatever its case or
// script, and nothing that isn't one address.
func TestSignOnProvesOnlyAddressesAtItsDomains(t *testing.T) {
	t.Parallel()
	now := time.Now()
	sso := SSO{Domains: []SSODomain{{Domain: "larkmoor.example", VerifiedAt: &now}, {Domain: "xn--bcher-kva.example", VerifiedAt: &now}, {Domain: "unproven.example"}}}
	for email, want := range map[string]bool{
		"ada@larkmoor.example":                  true,
		" Ada@Larkmoor.Example ":                true, //nolint:gocritic // The spaces are what is trimmed.
		"ada@bücher.example":                    true,
		"ada@sub.larkmoor.example":              false,
		"ada@unproven.example":                  false,
		"ada@intruder.example@larkmoor.example": false,
		"":                                      false,
	} {
		if got := sso.Proves(email); got != want {
			t.Errorf("Proves(%q) = %v, want %v", email, got, want)
		}
	}
}
