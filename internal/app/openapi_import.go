package app

import (
	"context"

	"github.com/shutthegoatup/prospector/api"
	"github.com/shutthegoatup/prospector/internal/request"
)

// AgentImport is what `prospector push` sends: the old tracker's drafts, companies and
// sent log, carried into the store's types by their JSON.
func (s apiServer) AgentImport(ctx context.Context, in api.AgentImportRequestObject) (api.AgentImportResponseObject, error) {
	p, err := request.Recast[ImportPayload](in.Body)
	if err != nil {
		return nil, err
	}
	total, err := s.a.s.Import(request.ReqFrom(ctx).Org.ID, &p)
	if err != nil {
		return nil, err
	}
	out := api.AgentImport200JSONResponse{}
	out.Contacts = total
	return out, nil
}
