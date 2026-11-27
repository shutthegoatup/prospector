package request

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/shutthegoatup/prospector/api"

	"github.com/shutthegoatup/prospector/internal/say"
)

// Problem is an API error as RFC 9457 lays one out.
type Problem struct {
	Type   api.ProblemType `json:"type"`
	Title  string          `json:"title"`
	Status int             `json:"status"`
	Detail string          `json:"detail,omitempty"`
}

// ProblemTitles are the titles of the problems a client acts on, one for each type as
// RFC 9457 asks.
var ProblemTitles = map[api.ProblemType]*say.Text{
	api.ProblemTypeSignedOut:          say.New("Signed out"),
	api.ProblemTypeSessionLimited:     say.New("Signed in for another organisation"),
	api.ProblemTypeWrongLogin:         say.New("Wrong email or password"),
	api.ProblemTypeThrottled:          say.New("Slow down"),
	api.ProblemTypeLinkUsed:           say.New("Link no longer works"),
	api.ProblemTypeMustChangePassword: say.New("Choose your own password"),
	api.ProblemTypeSsoRequired:        say.New("Single sign-on required"),
}

func WriteProblem(w http.ResponseWriter, r *http.Request, status int, detail say.Sayer) {
	WriteTyped(w, r, status, api.ProblemTypeUntyped, detail)
}

// WriteTyped answers with a Problem in r's language, which RFC 9457 lets its title and
// detail be.
func WriteTyped(w http.ResponseWriter, r *http.Request, status int, kind api.ProblemType, detail say.Sayer) {
	lang := speaking(w, r)
	title := http.StatusText(status)
	if t, ok := ProblemTitles[kind]; ok {
		title = t.In(lang)
	} else {
		kind = api.ProblemTypeUntyped
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(Problem{Type: kind, Title: title, Status: status, Detail: detail.In(lang)}); err != nil {
		log.Print(err)
	}
}

// Refused is an error about something the caller may not do, whatever it sends.
type Refused struct{ *say.Text }
