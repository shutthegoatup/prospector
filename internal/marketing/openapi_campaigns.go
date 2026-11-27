package marketing

import (
	"context"

	"github.com/shutthegoatup/prospector/api"

	"github.com/shutthegoatup/prospector/internal/paging"
	"github.com/shutthegoatup/prospector/internal/request"
	"github.com/shutthegoatup/prospector/internal/say"
	"github.com/shutthegoatup/prospector/internal/store"
)

func campaignOut(c store.Campaign) api.Campaign {
	return api.Campaign{Id: c.ID, Name: c.Name, Kind: api.CampaignKind(c.Kind), Audience: api.CampaignAudience(c.Audience), Subject: c.Subject, Body: c.Body,
		Status: api.CampaignStatus(c.Status), CreatedBy: c.CreatedBy, UpdatedBy: c.UpdatedBy, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
		QueuedBy: c.QueuedBy, QueuedAt: c.QueuedAt, SentAt: c.SentAt, Paused: c.Paused, Mail: map[string]int{}}
}

// tallied gives campaigns their counts of mail.
func (s MarketingAPI) tallied(org int64, list []store.Campaign) ([]api.Campaign, error) {
	ids := make([]int64, 0, len(list))
	for _, c := range list {
		ids = append(ids, c.ID)
	}
	tally, err := s.Campaigns.CampaignTally(org, ids)
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

func (s MarketingAPI) oneTallied(org int64, c *store.Campaign) (api.Campaign, error) {
	out, err := s.tallied(org, []store.Campaign{*c})
	if err != nil {
		return api.Campaign{}, err
	}
	return out[0], nil
}

func (s MarketingAPI) SendCampaign(ctx context.Context, in api.SendCampaignRequestObject) (api.SendCampaignResponseObject, error) {
	q := request.ReqFrom(ctx)
	if err := s.App.Sendable(q.Org); err != nil {
		return nil, err
	}
	c, err := s.Campaigns.SendCampaign(q.Org, in.Id, q.Actor())
	if err != nil {
		return nil, err
	}
	out, err := s.oneTallied(q.Org.ID, c)
	if err != nil {
		return nil, err
	}
	return api.SendCampaign202JSONResponse(out), nil
}

func (s MarketingAPI) CancelCampaign(ctx context.Context, in api.CancelCampaignRequestObject) (api.CancelCampaignResponseObject, error) {
	q := request.ReqFrom(ctx)
	c, err := s.Campaigns.CancelCampaign(q.Org.ID, in.Id)
	if err != nil {
		return nil, err
	}
	out, err := s.oneTallied(q.Org.ID, c)
	if err != nil {
		return nil, err
	}
	return api.CancelCampaign200JSONResponse(out), nil
}

func (s MarketingAPI) ResumeCampaign(ctx context.Context, in api.ResumeCampaignRequestObject) (api.ResumeCampaignResponseObject, error) {
	q := request.ReqFrom(ctx)
	if err := s.App.Sendable(q.Org); err != nil {
		return nil, err
	}
	c, err := s.Campaigns.ResumeCampaign(q.Org.ID, in.Id)
	if err != nil {
		return nil, err
	}
	out, err := s.oneTallied(q.Org.ID, c)
	if err != nil {
		return nil, err
	}
	return api.ResumeCampaign200JSONResponse(out), nil
}

func (s MarketingAPI) ListCampaignMail(ctx context.Context, in api.ListCampaignMailRequestObject) (api.ListCampaignMailResponseObject, error) {
	rows, err := s.Campaigns.CampaignRecipients(request.ReqFrom(ctx).Org.ID, in.Id)
	if err != nil {
		return nil, err
	}
	page, next, err := paging.ListPage(rows, in.Params.Limit, in.Params.Cursor)
	if err != nil {
		return nil, err
	}
	out := api.ListCampaignMail200JSONResponse{Headers: api.ListCampaignMail200ResponseHeaders{Link: request.NextLink(ctx, next)}}
	out.Body.Items, out.Body.NextCursor = []api.CampaignMail{}, next
	for _, r := range page {
		out.Body.Items = append(out.Body.Items, api.CampaignMail{Id: r.ID, Address: r.Address, ContactId: store.IDOf(r.ContactID), Name: r.Name,
			Status: api.CampaignMailStatus(r.Status), Reason: r.Reason, SentAt: r.SentAt})
	}
	return out, nil
}

func (s MarketingAPI) ListCampaigns(ctx context.Context, in api.ListCampaignsRequestObject) (api.ListCampaignsResponseObject, error) {
	all, err := s.Campaigns.Campaigns(request.ReqFrom(ctx).Org.ID)
	if err != nil {
		return nil, err
	}
	page, next, err := paging.ListPage(all, in.Params.Limit, in.Params.Cursor)
	if err != nil {
		return nil, err
	}
	out := api.ListCampaigns200JSONResponse{Headers: api.ListCampaigns200ResponseHeaders{Link: request.NextLink(ctx, next)}}
	items, err := s.tallied(request.ReqFrom(ctx).Org.ID, page)
	if err != nil {
		return nil, err
	}
	out.Body.Items, out.Body.NextCursor = items, next
	if err := s.App.Sendable(request.ReqFrom(ctx).Org); err != nil {
		out.Body.NotReady = err.Error()
	}
	return out, nil
}

func (s MarketingAPI) CreateCampaign(ctx context.Context, in api.CreateCampaignRequestObject) (api.CreateCampaignResponseObject, error) {
	if in.Body == nil {
		return nil, say.New("send the campaign: {\"name\": …}")
	}
	q, b := request.ReqFrom(ctx), in.Body
	c, err := s.Campaigns.AddCampaign(q.Org.ID, CampaignIn{Name: b.Name, Kind: b.Kind, Audience: b.Audience, Subject: b.Subject, Body: b.Body}, q.Actor())
	if err != nil {
		return nil, err
	}
	return api.CreateCampaign201JSONResponse(campaignOut(*c)), nil
}

func (s MarketingAPI) ChangeCampaign(ctx context.Context, in api.ChangeCampaignRequestObject) (api.ChangeCampaignResponseObject, error) {
	var changes []store.Change
	if b := in.Body; b != nil {
		for _, set := range []struct {
			field string
			value *string
		}{{"name", b.Name}, {"kind", b.Kind}, {"audience", b.Audience}, {"subject", b.Subject}, {"body", b.Body}} {
			if set.value != nil {
				changes = append(changes, store.Change{Field: set.field, Value: *set.value})
			}
		}
	}
	if len(changes) == 0 {
		return nil, say.New("send what to change: name, audience, subject or body")
	}
	q := request.ReqFrom(ctx)
	c, err := s.Campaigns.ChangeCampaign(q.Org.ID, in.Id, changes, q.Actor())
	if err != nil {
		return nil, err
	}
	return api.ChangeCampaign200JSONResponse(campaignOut(*c)), nil
}

func (s MarketingAPI) RemoveCampaign(ctx context.Context, in api.RemoveCampaignRequestObject) (api.RemoveCampaignResponseObject, error) {
	if err := s.Campaigns.RemoveCampaign(request.ReqFrom(ctx).Org.ID, in.Id); err != nil {
		return nil, err
	}
	return api.RemoveCampaign204Response{}, nil
}

func (s MarketingAPI) ListCampaignAudience(ctx context.Context, in api.ListCampaignAudienceRequestObject) (api.ListCampaignAudienceResponseObject, error) {
	q := request.ReqFrom(ctx)
	c, err := s.Campaigns.Campaign(q.Org.ID, in.Id)
	if err != nil {
		return nil, err
	}
	people, err := s.Campaigns.Audience(q.Org, *c)
	if err != nil {
		return nil, err
	}
	page, next, err := paging.ListPage(people, in.Params.Limit, in.Params.Cursor)
	if err != nil {
		return nil, err
	}
	out := api.ListCampaignAudience200JSONResponse{Headers: api.ListCampaignAudience200ResponseHeaders{Link: request.NextLink(ctx, next)}}
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

// MarketingAPI is the marketing product's operations: campaigns and the suppression
// list, which apiServer embeds.
type MarketingAPI struct {
	Campaigns   *OutreachStore
	Suppression *SuppressionStore
	App         marketingApp
}

// marketingApp is what marketing asks of the app: whether an organisation can send a
// campaign from here.
type marketingApp interface {
	Sendable(o *store.Org) error
}
