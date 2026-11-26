package main

import (
	"context"
	"errors"

	"github.com/shutthegoatup/prospector/api"
)

func campaignOut(c Campaign) api.Campaign {
	return api.Campaign{Id: c.ID, Name: c.Name, Kind: api.CampaignKind(c.Kind), Audience: api.CampaignAudience(c.Audience), Subject: c.Subject, Body: c.Body,
		Status: api.CampaignStatus(c.Status), CreatedBy: c.CreatedBy, UpdatedBy: c.UpdatedBy, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
		QueuedBy: c.QueuedBy, QueuedAt: c.QueuedAt, SentAt: c.SentAt, Paused: c.Paused, Mail: map[string]int{}}
}

// tallied gives campaigns their counts of mail.
func (s apiServer) tallied(org int64, list []Campaign) ([]api.Campaign, error) {
	ids := make([]int64, 0, len(list))
	for _, c := range list {
		ids = append(ids, c.ID)
	}
	tally, err := s.a.s.CampaignTally(org, ids)
	if err != nil {
		return nil, err
	}
	out := make([]api.Campaign, 0, len(list))
	for _, c := range list {
		o := campaignOut(c)
		for status, n := range tally[c.ID] {
			o.Mail[status] = n
		}
		out = append(out, o)
	}
	return out, nil
}

func (s apiServer) oneTallied(org int64, c *Campaign) (api.Campaign, error) {
	out, err := s.tallied(org, []Campaign{*c})
	if err != nil {
		return api.Campaign{}, err
	}
	return out[0], nil
}

func (s apiServer) SendCampaign(ctx context.Context, in api.SendCampaignRequestObject) (api.SendCampaignResponseObject, error) {
	q := reqFrom(ctx)
	if err := s.a.sendable(q.Org); err != nil {
		return nil, err
	}
	c, err := s.a.s.SendCampaign(q.Org, in.Id, q.Actor())
	if err != nil {
		return nil, err
	}
	out, err := s.oneTallied(q.Org.ID, c)
	if err != nil {
		return nil, err
	}
	return api.SendCampaign202JSONResponse(out), nil
}

func (s apiServer) CancelCampaign(ctx context.Context, in api.CancelCampaignRequestObject) (api.CancelCampaignResponseObject, error) {
	q := reqFrom(ctx)
	c, err := s.a.s.CancelCampaign(q.Org.ID, in.Id)
	if err != nil {
		return nil, err
	}
	out, err := s.oneTallied(q.Org.ID, c)
	if err != nil {
		return nil, err
	}
	return api.CancelCampaign200JSONResponse(out), nil
}

func (s apiServer) ResumeCampaign(ctx context.Context, in api.ResumeCampaignRequestObject) (api.ResumeCampaignResponseObject, error) {
	q := reqFrom(ctx)
	if err := s.a.sendable(q.Org); err != nil {
		return nil, err
	}
	c, err := s.a.s.ResumeCampaign(q.Org.ID, in.Id)
	if err != nil {
		return nil, err
	}
	out, err := s.oneTallied(q.Org.ID, c)
	if err != nil {
		return nil, err
	}
	return api.ResumeCampaign200JSONResponse(out), nil
}

func (s apiServer) ListCampaignMail(ctx context.Context, in api.ListCampaignMailRequestObject) (api.ListCampaignMailResponseObject, error) {
	rows, err := s.a.s.CampaignRecipients(reqFrom(ctx).Org.ID, in.Id)
	if err != nil {
		return nil, err
	}
	page, next, err := listPage(rows, in.Params.Limit, in.Params.Cursor)
	if err != nil {
		return nil, err
	}
	out := api.ListCampaignMail200JSONResponse{Headers: api.ListCampaignMail200ResponseHeaders{Link: nextLink(ctx, next)}}
	out.Body.Items, out.Body.NextCursor = []api.CampaignMail{}, next
	for _, r := range page {
		out.Body.Items = append(out.Body.Items, api.CampaignMail{Id: r.ID, Address: r.Address, ContactId: idOf(r.ContactID), Name: r.Name,
			Status: api.CampaignMailStatus(r.Status), Reason: r.Reason, SentAt: r.SentAt})
	}
	return out, nil
}

func (s apiServer) ListCampaigns(ctx context.Context, in api.ListCampaignsRequestObject) (api.ListCampaignsResponseObject, error) {
	all, err := s.a.s.Campaigns(reqFrom(ctx).Org.ID)
	if err != nil {
		return nil, err
	}
	page, next, err := listPage(all, in.Params.Limit, in.Params.Cursor)
	if err != nil {
		return nil, err
	}
	out := api.ListCampaigns200JSONResponse{Headers: api.ListCampaigns200ResponseHeaders{Link: nextLink(ctx, next)}}
	items, err := s.tallied(reqFrom(ctx).Org.ID, page)
	if err != nil {
		return nil, err
	}
	out.Body.Items, out.Body.NextCursor = items, next
	return out, nil
}

func (s apiServer) CreateCampaign(ctx context.Context, in api.CreateCampaignRequestObject) (api.CreateCampaignResponseObject, error) {
	if in.Body == nil {
		return nil, errors.New("send the campaign: {\"name\": …}")
	}
	q, b := reqFrom(ctx), in.Body
	c, err := s.a.s.AddCampaign(q.Org.ID, CampaignIn{Name: b.Name, Kind: b.Kind, Audience: b.Audience, Subject: b.Subject, Body: b.Body}, q.Actor())
	if err != nil {
		return nil, err
	}
	return api.CreateCampaign201JSONResponse(campaignOut(*c)), nil
}

func (s apiServer) ChangeCampaign(ctx context.Context, in api.ChangeCampaignRequestObject) (api.ChangeCampaignResponseObject, error) {
	var changes []Change
	if b := in.Body; b != nil {
		for _, set := range []struct {
			field string
			value *string
		}{{"name", b.Name}, {"kind", b.Kind}, {"audience", b.Audience}, {"subject", b.Subject}, {"body", b.Body}} {
			if set.value != nil {
				changes = append(changes, Change{set.field, *set.value})
			}
		}
	}
	if len(changes) == 0 {
		return nil, errors.New("send what to change: name, audience, subject or body")
	}
	q := reqFrom(ctx)
	c, err := s.a.s.ChangeCampaign(q.Org.ID, in.Id, changes, q.Actor())
	if err != nil {
		return nil, err
	}
	return api.ChangeCampaign200JSONResponse(campaignOut(*c)), nil
}

func (s apiServer) RemoveCampaign(ctx context.Context, in api.RemoveCampaignRequestObject) (api.RemoveCampaignResponseObject, error) {
	if err := s.a.s.RemoveCampaign(reqFrom(ctx).Org.ID, in.Id); err != nil {
		return nil, err
	}
	return api.RemoveCampaign204Response{}, nil
}

func (s apiServer) ListCampaignAudience(ctx context.Context, in api.ListCampaignAudienceRequestObject) (api.ListCampaignAudienceResponseObject, error) {
	q := reqFrom(ctx)
	c, err := s.a.s.Campaign(q.Org.ID, in.Id)
	if err != nil {
		return nil, err
	}
	people, err := s.a.s.Audience(q.Org, *c)
	if err != nil {
		return nil, err
	}
	page, next, err := listPage(people, in.Params.Limit, in.Params.Cursor)
	if err != nil {
		return nil, err
	}
	out := api.ListCampaignAudience200JSONResponse{Headers: api.ListCampaignAudience200ResponseHeaders{Link: nextLink(ctx, next)}}
	out.Body.Items, out.Body.NextCursor = []api.Recipient{}, next
	for _, p := range people {
		if p.LeftOut == "" {
			out.Body.Going++
		} else {
			out.Body.LeftOut++
		}
	}
	for _, p := range page {
		out.Body.Items = append(out.Body.Items, api.Recipient{ContactId: p.ContactID, Name: p.Name, Company: p.Company, Address: p.Address, LeftOut: p.LeftOut})
	}
	return out, nil
}
