package privacy

import (
	"context"

	"github.com/shutthegoatup/prospector/api"

	"github.com/shutthegoatup/prospector/internal/request"
)

func (s PrivacyAPI) ExportPerson(ctx context.Context, in api.ExportPersonRequestObject) (api.ExportPersonResponseObject, error) {
	q := request.ReqFrom(ctx)
	held, err := s.Store.ExportPerson(q.Org.ID, in.Id, q.Actor())
	if err != nil {
		return nil, err
	}
	out, err := request.Recast[map[string]any](held)
	if err != nil {
		return nil, err
	}
	return api.ExportPerson200JSONResponse(out), nil
}

func (s PrivacyAPI) ErasePerson(ctx context.Context, in api.ErasePersonRequestObject) (api.ErasePersonResponseObject, error) {
	q := request.ReqFrom(ctx)
	if err := s.Store.ErasePerson(q.Org.ID, in.Id, q.Actor()); err != nil {
		return nil, err
	}
	return api.ErasePerson204Response{}, nil
}

// PrivacyAPI is a person's export and erasure, which apiServer embeds.
type PrivacyAPI struct{ Store *PrivacyStore }
