package egress

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestOnlyPublicAddressesArePublic(t *testing.T) {
	t.Parallel()
	for addr, want := range map[string]bool{"8.8.8.8": true, "2606:4700::1111": true, "127.0.0.1": false, "10.1.2.3": false,
		"169.254.169.254": false, "192.168.1.1": false, "100.64.0.1": false, "::1": false, "fd00::1": false, "fe80::1": false,
		"::ffff:127.0.0.1": false, "0.0.0.0": false, "172.16.0.1": false} {
		if got := Public(netip.MustParseAddr(addr)); got != want {
			t.Errorf("Public(%s) = %v", addr, got)
		}
	}
}

// TestTheClientReachesNothingInside: a server on this host is refused unless allowed,
// and a redirect is handed back rather than followed.
func TestTheClientReachesNothingInside(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/", http.StatusFound)
	}))
	t.Cleanup(srv.Close)
	if res, err := Client(Options{}).Get(srv.URL); err == nil {
		res.Body.Close()
		t.Fatal("reached a server on this host")
	}
	res, err := Client(Options{AllowPrivate: true}).Get(srv.URL)
	if err != nil {
		t.Fatalf("allowed, but refused: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusFound {
		t.Fatalf("a redirect was followed: %d", res.StatusCode)
	}
}
