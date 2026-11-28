// Package egress is how the server reaches an address someone typed in: a sign-in
// provider, an agent's model endpoint, a page an agent asked for, a webhook endpoint.
package egress

import (
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"

	"github.com/shutthegoatup/prospector/internal/say"
)

// notPublic is address space nothing outside lives in: this host, private networks,
// link-local (where cloud metadata services sit) and the reserved ranges.
var notPublic = func() []netip.Prefix {
	var out []netip.Prefix
	for _, p := range []string{"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12",
		"192.0.0.0/24", "192.0.2.0/24", "192.168.0.0/16", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4",
		"240.0.0.0/4", "2001::/23", "2001:db8::/32", "2002::/16", "3fff::/20"} {
		out = append(out, netip.MustParsePrefix(p))
	}
	return out
}()

var publicV6 = netip.MustParsePrefix("2000::/3")

// Public is whether addr is on the public internet. An IPv4 address mapped into IPv6
// is judged as IPv4, and IPv6 outside global unicast isn't public.
func Public(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsValid() || (addr.Is6() && !publicV6.Contains(addr)) {
		return false
	}
	for _, p := range notPublic {
		if p.Contains(addr) {
			return false
		}
	}
	return true
}

// Options shape a Client. Zero timeouts take the defaults.
type Options struct {
	// AllowPrivate lets the client reach this host and private networks, for an
	// operator who runs what it calls beside the server.
	AllowPrivate bool
	// Timeout bounds a whole request; 0 leaves it to the caller's context.
	Timeout time.Duration
	// DialTimeout bounds making the connection and its TLS handshake; 0 is 10 seconds.
	DialTimeout time.Duration
}

// Client reaches only public addresses, checked as each connection is made, so a name
// that resolves somewhere else the second time gets no further. A redirect, being a
// second destination, isn't followed, and no proxy is used.
func Client(o Options) *http.Client {
	if o.DialTimeout == 0 {
		o.DialTimeout = 10 * time.Second
	}
	dialer := &net.Dialer{Timeout: o.DialTimeout}
	if !o.AllowPrivate {
		dialer.Control = func(_, address string, _ syscall.RawConn) error {
			ap, err := netip.ParseAddrPort(address)
			if err != nil || !Public(ap.Addr()) {
				return say.Errorf("%s isn't a public address", address)
			}
			return nil
		}
	}
	return &http.Client{
		Timeout:       o.Timeout,
		Transport:     &http.Transport{DialContext: dialer.DialContext, TLSHandshakeTimeout: o.DialTimeout, IdleConnTimeout: 90 * time.Second},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}
