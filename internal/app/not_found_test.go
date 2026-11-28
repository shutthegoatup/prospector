package app

import (
	"net/http"
	"testing"
)

// TestChangingWhatIsntThereIsNotFound: a member or key the organisation hasn't got is a
// 404, as on every other operation, not a bad request.
func TestChangingWhatIsntThereIsNotFound(t *testing.T) {
	t.Parallel()
	_, _, srv, ada := signedIn(t)
	base := srv.URL + "/o/alpha/api/v1/"
	for _, try := range []struct {
		name, method, path string
		body               any
	}{
		{"removing a member", "DELETE", "members/999999", nil},
		{"changing a member's areas", "PATCH", "members/999999", map[string]any{"areas": []string{"sales"}}},
		{"changing a key", "PATCH", "keys/999999", map[string]any{"scopes": []string{"read"}}},
	} {
		ctype := ""
		if try.body != nil {
			ctype = "application/json"
		}
		if code, body := call(t, ada, try.method, base+try.path, ctype, try.body); code != http.StatusNotFound {
			t.Errorf("%s that isn't there: %d %s", try.name, code, body)
		}
	}
}
