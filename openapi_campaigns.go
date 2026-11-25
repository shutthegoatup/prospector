package main

import (
	"context"
	"errors"

	"github.com/shutthegoatup/prospector/api"
)

func campaignOut(c Campaign) api.Campaign {
	return api.Campaign{Id: c.ID, Name: c.Name, Audience: api.CampaignAudience(c.Audience), Subject: c.Subject, Body: c.Body,
		Status: api.CampaignStatus(c.Status), CreatedBy: c.CreatedBy, UpdatedBy: c.UpdatedBy, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt}
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
	out.Body.Items, out.Body.NextCursor = []api.Campaign{}, next
	for _, c := range page {
		out.Body.Items = append(out.Body.Items, campaignOut(c))
	}
	return out, nil
}

func (s apiServer) CreateCampaign(ctx context.Context, in api.CreateCampaignRequestObject) (api.CreateCampaignResponseObject, error) {
	if in.Body == nil {
		return nil, errors.New("send the campaign: {\"name\": …}")
	}
	q, b := reqFrom(ctx), in.Body
	c, err := s.a.s.AddCampaign(q.Org.ID, CampaignIn{Name: b.Name, Audience: b.Audience, Subject: b.Subject, Body: b.Body}, q.Actor())
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
		}{{"name", b.Name}, {"audience", b.Audience}, {"subject", b.Subject}, {"body", b.Body}} {
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
