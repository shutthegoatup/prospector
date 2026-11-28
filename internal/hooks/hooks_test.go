package hooks

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fake verifies by a header and does what its handle says.
type fake struct {
	handle   error
	verified []byte
	handled  []byte
}

func (f *fake) Verify(r *http.Request, body []byte) (int64, error) {
	f.verified = body
	if r.Header.Get("X-Signed") != "yes" {
		return 0, ErrUnauthorized
	}
	return 7, nil
}

func (f *fake) Handle(_ context.Context, org int64, _ *http.Request, body []byte) error {
	if org != 7 {
		return errors.New("the wrong organisation")
	}
	f.handled = body
	return f.handle
}

func post(t *testing.T, h http.Handler, provider string, signed bool, body []byte) int {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle("POST /hooks/{provider}", h)
	req := httptest.NewRequest(http.MethodPost, "/hooks/"+provider, bytes.NewReader(body))
	if signed {
		req.Header.Set("X-Signed", "yes")
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec.Code
}

func TestEveryProviderIsAnsweredTheSameWay(t *testing.T) {
	t.Parallel()
	kept, unreadable, failing := &fake{}, &fake{handle: ErrMalformed}, &fake{handle: errors.New("the database is away")}
	h := Handler(map[string]Provider{"kept": kept, "unreadable": unreadable, "failing": failing}, nil)
	for _, c := range []struct {
		name, provider string
		signed         bool
		body           []byte
		want           int
	}{
		{"kept", "kept", true, []byte(`{}`), http.StatusNoContent},
		{"no such provider", "nobody", true, []byte(`{}`), http.StatusNotFound},
		{"not signed", "kept", false, []byte(`{}`), http.StatusUnauthorized},
		{"too big", "kept", true, bytes.Repeat([]byte("x"), MaxBody+1), http.StatusRequestEntityTooLarge},
		{"malformed", "unreadable", true, []byte(`{}`), http.StatusUnprocessableEntity},
		{"not kept, so sent again", "failing", true, []byte(`{}`), http.StatusServiceUnavailable},
	} {
		if got := post(t, h, c.provider, c.signed, c.body); got != c.want {
			t.Errorf("%s: %d, want %d", c.name, got, c.want)
		}
	}
}

// TestASignatureIsCheckedOverTheBytesAsTheyCame: the verifier sees the body untouched;
// what is acted on has its NUL escapes taken out, as every other request's has.
func TestASignatureIsCheckedOverTheBytesAsTheyCame(t *testing.T) {
	t.Parallel()
	f := &fake{}
	body := []byte(`{"Email":"ada\u0000@larkmoor.example"}`)
	if code := post(t, Handler(map[string]Provider{"f": f}, nil), "f", true, body); code != http.StatusNoContent {
		t.Fatalf("%d", code)
	}
	if !bytes.Equal(f.verified, body) {
		t.Errorf("verified %q, not the body as sent", f.verified)
	}
	if strings.Contains(string(f.handled), `\u0000`) {
		t.Errorf("handled %q, with its NUL", f.handled)
	}
}
