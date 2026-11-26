package main

import (
	"context"
	"errors"
	"slices"
	"strings"

	"gorm.io/gorm"

	"github.com/shutthegoatup/prospector/api"
)

func (s apiServer) AddPerson(ctx context.Context, in api.AddPersonRequestObject) (api.AddPersonResponseObject, error) {
	q := reqFrom(ctx)
	b := in.Body
	c, err := s.a.s.QuickAddPerson(q.Org, QuickAdd{Name: b.Name, Linkedin: b.Linkedin, Title: b.Title, Company: b.Company, Email: b.Email, Note: b.Note, Added: b.Added}, q.Actor())
	if err != nil {
		return nil, err
	}
	where := "is in the LinkedIn queue"
	if b.Added {
		where = "is marked added; their email comes up in " + plural(q.Org.Settings.withDefaults().EmailAfterDays, "day")
	}
	return api.AddPerson201JSONResponse{Id: c.ID, Name: displayName(*c), Message: displayName(*c) + " " + where + "."}, nil
}

func (s apiServer) ChangeContact(ctx context.Context, in api.ChangeContactRequestObject) (api.ChangeContactResponseObject, error) {
	q := reqFrom(ctx)
	changes, err := recast[map[string]*string](in.Body)
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
		return nil, errors.New("send at least one field to change")
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
	err = s.a.s.db.Transaction(func(tx *gorm.DB) error {
		for _, f := range fields {
			if err := s.a.s.set(tx, q.Org.ID, in.Id, f, *changes[f], q.Actor()); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return api.ChangeContact204Response{}, nil
}

func (s apiServer) PickContact(ctx context.Context, in api.PickContactRequestObject) (api.PickContactResponseObject, error) {
	q := reqFrom(ctx)
	if err := s.a.s.PickContactFor(q.Org.ID, q.Org, in.Id, in.Params.Offering, q.Actor()); err != nil {
		return nil, err
	}
	return api.PickContact204Response{}, nil
}

func (s apiServer) ReviewCompany(ctx context.Context, in api.ReviewCompanyRequestObject) (api.ReviewCompanyResponseObject, error) {
	q := reqFrom(ctx)
	b := in.Body
	if err := s.a.s.JudgeCompany(q.Org.ID, b.Offering, in.Id, string(b.Stage), b.Fit, b.Reason, q.Actor()); err != nil {
		return nil, err
	}
	return api.ReviewCompany204Response{}, nil
}

func (s apiServer) DecideSuggestion(ctx context.Context, in api.DecideSuggestionRequestObject) (api.DecideSuggestionResponseObject, error) {
	q := reqFrom(ctx)
	if err := s.a.s.DecideSuggestion(q.Org.ID, in.Id, string(in.Body.Decision), q.Actor()); err != nil {
		return nil, err
	}
	return api.DecideSuggestion204Response{}, nil
}
