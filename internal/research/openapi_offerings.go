package research

import (
	"context"

	"github.com/shutthegoatup/prospector/internal/slicesx"

	"github.com/shutthegoatup/prospector/api"

	"github.com/shutthegoatup/prospector/internal/paging"
	"github.com/shutthegoatup/prospector/internal/prospecting"
	"github.com/shutthegoatup/prospector/internal/request"
	"github.com/shutthegoatup/prospector/internal/say"
	"github.com/shutthegoatup/prospector/internal/store"
)

func offeringOut(o store.Offering) api.Offering {
	return api.Offering{Id: o.ID, Name: o.Name, LookFor: o.LookFor, LookingFor: LookingFor(store.OrgSettings{LookFor: o.LookFor}), Pitch: o.Pitch, NoteTemplate: o.NoteTemplate,
		HasRoles: o.HasRoles, UpdatedBy: o.UpdatedBy, CreatedAt: o.CreatedAt, UpdatedAt: o.UpdatedAt}
}

func verdictOut(v VerdictRow) api.Verdict {
	return api.Verdict{OfferingId: v.OfferingID, CompanyId: v.CompanyID, Company: v.Company, Stage: api.VerdictStage(v.Stage),
		Fit: v.Fit, Reason: v.Reason, By: v.By, UpdatedAt: v.UpdatedAt}
}

func (s ResearchAPI) ListOfferings(ctx context.Context, in api.ListOfferingsRequestObject) (api.ListOfferingsResponseObject, error) {
	rows, err := s.Research.Offerings(request.ReqFrom(ctx).Org.ID)
	if err != nil {
		return nil, err
	}
	rows, next, err := paging.ListPage(rows, in.Params.Limit, in.Params.Cursor)
	if err != nil {
		return nil, err
	}
	out := api.ListOfferings200JSONResponse{Headers: api.ListOfferings200ResponseHeaders{Link: request.NextLink(ctx, next)}}
	out.Body.Items, out.Body.NextCursor = slicesx.Map(rows, offeringOut), next
	return out, nil
}

func (s ResearchAPI) AddOffering(ctx context.Context, in api.AddOfferingRequestObject) (api.AddOfferingResponseObject, error) {
	if in.Body == nil {
		return nil, say.New("send the offering: {\"name\": …}")
	}
	q, b := request.ReqFrom(ctx), in.Body
	o, err := s.Research.AddOffering(q.Org.ID, store.Offering{Name: b.Name, LookFor: b.LookFor, Pitch: b.Pitch, NoteTemplate: b.NoteTemplate, HasRoles: b.HasRoles}, q.Actor())
	if err != nil {
		return nil, err
	}
	return api.AddOffering201JSONResponse(offeringOut(*o)), nil
}

func (s ResearchAPI) ChangeOffering(ctx context.Context, in api.ChangeOfferingRequestObject) (api.ChangeOfferingResponseObject, error) {
	if in.Body == nil {
		return nil, say.New("send what changes, such as {\"name\": …}")
	}
	q, c := request.ReqFrom(ctx), in.Body
	o, err := s.Research.ChangeOffering(q.Org.ID, in.Id, func(o *store.Offering) {
		request.Put(&o.Name, c.Name)
		request.Put(&o.LookFor, c.LookFor)
		request.Put(&o.Pitch, c.Pitch)
		request.Put(&o.NoteTemplate, c.NoteTemplate)
		request.Put(&o.HasRoles, c.HasRoles)
	}, q.Actor())
	if err != nil {
		return nil, err
	}
	return api.ChangeOffering200JSONResponse(offeringOut(*o)), nil
}

func (s ResearchAPI) RemoveOffering(ctx context.Context, in api.RemoveOfferingRequestObject) (api.RemoveOfferingResponseObject, error) {
	q := request.ReqFrom(ctx)
	if err := s.Research.RemoveOffering(q.Org.ID, in.Id, q.Actor()); err != nil {
		return nil, err
	}
	return api.RemoveOffering204Response{}, nil
}

func (s ResearchAPI) ListVerdicts(ctx context.Context, in api.ListVerdictsRequestObject) (api.ListVerdictsResponseObject, error) {
	switch stage := string(in.Params.Stage); stage {
	case "", prospecting.CompanyReview, prospecting.CompanyQualified, prospecting.CompanyRejected:
	default:
		return nil, say.Errorf("stage is review, qualified or rejected, not %q", stage)
	}
	rows, err := s.Research.Judged(request.ReqFrom(ctx).Org.ID, in.Id, string(in.Params.Stage))
	if err != nil {
		return nil, err
	}
	rows, next, err := paging.ListPage(rows, in.Params.Limit, in.Params.Cursor)
	if err != nil {
		return nil, err
	}
	out := api.ListVerdicts200JSONResponse{Headers: api.ListVerdicts200ResponseHeaders{Link: request.NextLink(ctx, next)}}
	out.Body.Items, out.Body.NextCursor = slicesx.Map(rows, verdictOut), next
	return out, nil
}

func (s ResearchAPI) ListCompanyVerdicts(ctx context.Context, in api.ListCompanyVerdictsRequestObject) (api.ListCompanyVerdictsResponseObject, error) {
	rows, err := s.Research.Verdicts(request.ReqFrom(ctx).Org.ID, in.Id)
	if err != nil {
		return nil, err
	}
	rows, next, err := paging.ListPage(rows, in.Params.Limit, in.Params.Cursor)
	if err != nil {
		return nil, err
	}
	out := api.ListCompanyVerdicts200JSONResponse{Headers: api.ListCompanyVerdicts200ResponseHeaders{Link: request.NextLink(ctx, next)}}
	out.Body.Items, out.Body.NextCursor = slicesx.Map(rows, verdictOut), next
	return out, nil
}

func roleOut(r store.Role) api.Role {
	return api.Role{Id: r.ID, OfferingId: r.OfferingID, CompanyId: r.CompanyID, Title: r.Title, Url: r.URL, Details: r.Details,
		Stage: api.RoleStage(r.Stage), Reason: r.Reason, By: r.By, FoundBy: r.FoundBy, TaskId: r.TaskID, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
}

func (s ResearchAPI) ListCompanyRoles(ctx context.Context, in api.ListCompanyRolesRequestObject) (api.ListCompanyRolesResponseObject, error) {
	rows, err := s.Research.Roles(request.ReqFrom(ctx).Org.ID, in.Id, in.Params.Offering)
	if err != nil {
		return nil, err
	}
	rows, next, err := paging.ListPage(rows, in.Params.Limit, in.Params.Cursor)
	if err != nil {
		return nil, err
	}
	out := api.ListCompanyRoles200JSONResponse{Headers: api.ListCompanyRoles200ResponseHeaders{Link: request.NextLink(ctx, next)}}
	out.Body.Items, out.Body.NextCursor = slicesx.Map(rows, roleOut), next
	return out, nil
}

func (s ResearchAPI) AddRole(ctx context.Context, in api.AddRoleRequestObject) (api.AddRoleResponseObject, error) {
	if in.Body == nil {
		return nil, say.New("send the role: {\"offering\": …, \"title\": …}")
	}
	q, b := request.ReqFrom(ctx), in.Body
	r, err := s.Research.AddRole(q.Org.ID, b.Offering, in.Id, RoleIn{Title: b.Title, SourceURL: b.SourceUrl, Details: b.Details}, q.Actor())
	if err != nil {
		return nil, err
	}
	return api.AddRole201JSONResponse(roleOut(*r)), nil
}

func (s ResearchAPI) JudgeRole(ctx context.Context, in api.JudgeRoleRequestObject) (api.JudgeRoleResponseObject, error) {
	if in.Body == nil {
		return nil, say.New("send the stage: {\"stage\": \"pursued\"}")
	}
	q := request.ReqFrom(ctx)
	r, err := s.Research.JudgeRole(q.Org.ID, in.Id, string(in.Body.Stage), in.Body.Reason, q.Actor())
	if err != nil {
		return nil, err
	}
	return api.JudgeRole200JSONResponse(roleOut(*r)), nil
}
