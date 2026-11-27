package research

import (
	"context"
	"slices"
	"strings"

	"github.com/shutthegoatup/prospector/api"

	"github.com/shutthegoatup/prospector/internal/crm"
	"github.com/shutthegoatup/prospector/internal/request"
	"github.com/shutthegoatup/prospector/internal/say"
	"github.com/shutthegoatup/prospector/internal/store"
)

func (s ResearchAPI) AddPerson(ctx context.Context, in api.AddPersonRequestObject) (api.AddPersonResponseObject, error) {
	q := request.ReqFrom(ctx)
	b := in.Body
	c, err := s.Records.QuickAddPerson(q.Org, crm.QuickAdd{Name: b.Name, Linkedin: b.Linkedin, Title: b.Title, Company: b.Company, Email: b.Email, Note: b.Note, Added: b.Added}, q.Actor())
	if err != nil {
		return nil, err
	}
	name := store.DisplayName(*c)
	said := say.Errorf("%s is in the LinkedIn queue.", name)
	if b.Added {
		said = say.Errorf("%s is marked added; their email comes up in %d days.", name, q.Org.Settings.WithDefaults().EmailAfterDays)
	}
	return api.AddPerson201JSONResponse{Id: c.ID, Name: name, Message: said.In(request.LanguageFrom(ctx))}, nil
}

func (s ResearchAPI) ChangeContact(ctx context.Context, in api.ChangeContactRequestObject) (api.ChangeContactResponseObject, error) {
	q := request.ReqFrom(ctx)
	changes, err := request.Recast[map[string]*string](in.Body)
	if err != nil {
		return nil, err
	}
	fields := make([]string, 0, len(changes))
	for f, v := range changes {
		if v != nil {
			fields = append(fields, f)
		}
	}
	if len(fields) == 0 {
		return nil, say.New("send at least one field to change")
	}
	// A time zone sent with a phone is the one wanted, not the phone's guess: it goes last.
	slices.SortFunc(fields, func(a, b string) int {
		if (a == "tz") != (b == "tz") {
			if a == "tz" {
				return 1
			}
			return -1
		}
		return strings.Compare(a, b)
	})
	ordered := make([]store.Change, len(fields))
	for i, f := range fields {
		ordered[i] = store.Change{Field: f, Value: *changes[f]}
	}
	if err := s.Records.ChangeContactFields(q.Org.ID, in.Id, ordered, q.Actor()); err != nil {
		return nil, err
	}
	return api.ChangeContact204Response{}, nil
}

func (s ResearchAPI) PickContact(ctx context.Context, in api.PickContactRequestObject) (api.PickContactResponseObject, error) {
	q := request.ReqFrom(ctx)
	if err := s.Records.PickContactFor(q.Org.ID, q.Org, in.Id, in.Params.Offering, q.Actor()); err != nil {
		return nil, err
	}
	return api.PickContact204Response{}, nil
}

func (s ResearchAPI) ReviewCompany(ctx context.Context, in api.ReviewCompanyRequestObject) (api.ReviewCompanyResponseObject, error) {
	q := request.ReqFrom(ctx)
	b := in.Body
	if err := s.Research.JudgeCompany(q.Org.ID, b.Offering, in.Id, string(b.Stage), b.Fit, b.Reason, q.Actor()); err != nil {
		return nil, err
	}
	return api.ReviewCompany204Response{}, nil
}

func (s ResearchAPI) DecideSuggestion(ctx context.Context, in api.DecideSuggestionRequestObject) (api.DecideSuggestionResponseObject, error) {
	q := request.ReqFrom(ctx)
	if err := s.Finding.DecideSuggestion(q.Org.ID, in.Id, string(in.Body.Decision), q.Actor()); err != nil {
		return nil, err
	}
	return api.DecideSuggestion204Response{}, nil
}
