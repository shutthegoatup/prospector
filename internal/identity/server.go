package identity

import (
	"net/url"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// dummyHash keeps a wrong email as slow as a wrong password.
// passwordCost is how hard a password hash is to compute; the tests lower it.
var PasswordCost = bcrypt.DefaultCost

var DummyHash, _ = bcrypt.GenerateFromPassword([]byte("unused"), PasswordCost)

// BrowserOrigin is an address as a browser names it in Origin: lower case, and
// without the port when it is the scheme's own.
func BrowserOrigin(address string) string {
	u, err := url.Parse(address)
	if err != nil {
		return address
	}
	host := u.Host
	if (u.Scheme == "https" && u.Port() == "443") || (u.Scheme == "http" && u.Port() == "80") {
		host = strings.TrimSuffix(host, ":"+u.Port())
	}
	return strings.ToLower(u.Scheme + "://" + host)
}
