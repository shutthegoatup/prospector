// Package hooks takes what other services post to Prospector: one route,
// /hooks/{provider}, and the same answers for every provider, so a provider retries
// what wasn't kept and nothing else.
package hooks

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"

	"github.com/shutthegoatup/prospector/internal/cleantext"
)

// MaxBody is the most a provider may post. The body is read whole before it can be
// verified, so it is bounded first.
const MaxBody = 1 << 20

// A Provider is a service that posts to /hooks/{its name}.
type Provider interface {
	// Verify says which organisation posted r, from the body exactly as it came: a
	// signature is over those bytes. ErrUnauthorized when it can't be told.
	Verify(r *http.Request, body []byte) (org int64, err error)
	// Handle acts on what was posted, its text cleaned as every request's is. It
	// returns nil for anything it doesn't act on, so that isn't posted again;
	// ErrMalformed for what it can't read; anything else for a failure of its own,
	// which the provider should try again.
	Handle(ctx context.Context, org int64, r *http.Request, body []byte) error
}

var (
	ErrUnauthorized = errors.New("not signed by a sender this organisation set up")
	ErrMalformed    = errors.New("not something this provider sends")
)

// Handler serves POST /hooks/{provider} for the providers named.
//
//	401 not verified · 404 no such provider · 413 too big · 422 malformed
//	503 not kept, so try again · 204 kept, or nothing to keep
func Handler(providers map[string]Provider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("provider")
		p, ok := providers[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBody))
		if err != nil {
			if _, tooBig := errors.AsType[*http.MaxBytesError](err); tooBig {
				http.Error(w, "that is more than a webhook sends", http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, "the body couldn't be read", http.StatusBadRequest)
			return
		}
		org, err := p.Verify(r, body)
		if err != nil {
			w.Header().Set("WWW-Authenticate", `Basic realm="prospector"`)
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		clean, _ := cleantext.DropNULEscapes(body, true)
		switch err := p.Handle(r.Context(), org, r, clean); {
		case err == nil:
			w.WriteHeader(http.StatusNoContent)
		case errors.Is(err, ErrMalformed):
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		default:
			log.Printf("a %s webhook for organisation %d wasn't kept: %v", name, org, err)
			http.Error(w, "it wasn't kept; send it again", http.StatusServiceUnavailable)
		}
	}
}
