package app

import "testing"

// TestOnlyALinkOthersCanSafelyCallIsHandedOut: https, or http on this host for a server
// run to be tested; never plain http anywhere else.
func TestOnlyALinkOthersCanSafelyCallIsHandedOut(t *testing.T) {
	t.Parallel()
	for base, want := range map[string]bool{
		"https://prospector.example.com": true,
		"http://127.0.0.1:8791":          true,
		"http://[::1]:8791":              true,
		"http://localhost:8766":          true,
		"http://prospector.example.com":  false,
		"http://10.0.0.5:8766":           false,
		"":                               false,
	} {
		if got := (&App{base: base}).secureBase() != nil; got != want {
			t.Errorf("secureBase for %q: %v, want %v", base, got, want)
		}
	}
}
