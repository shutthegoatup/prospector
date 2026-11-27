package main

import (
	"context"
	"mime"
	"net/http"
	"time"

	"github.com/shutthegoatup/prospector/api"
	"github.com/shutthegoatup/prospector/internal/say"
)

// uploads holds the one operation that takes a file: the body must be a PDF and not
// declared past the limit, both told before any of it is read; and the answer gets its
// own time once the body is in, so a slow upload that was kept is not reported as failed.
func uploads(f api.StrictHandlerFunc, op string) api.StrictHandlerFunc {
	if op != "PutAgreementDocument" {
		return f
	}
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request, in any) (any, error) {
		if kind, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); kind != "application/pdf" {
			writeProblem(w, r, http.StatusUnsupportedMediaType, say.New("send the document as application/pdf"))
			return nil, nil
		}
		if r.ContentLength > agreementFileLimit {
			writeProblem(w, r, http.StatusRequestEntityTooLarge, say.Errorf("a document is %d MB at most", agreementFileLimit>>20))
			return nil, nil
		}
		out, err := f(ctx, w, r, in)
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(answerLimit))
		return out, err
	}
}
