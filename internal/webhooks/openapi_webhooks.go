package webhooks

import (
	"context"

	"github.com/shutthegoatup/prospector/api"
	"github.com/shutthegoatup/prospector/internal/paging"
	"github.com/shutthegoatup/prospector/internal/request"
	"github.com/shutthegoatup/prospector/internal/say"
	"github.com/shutthegoatup/prospector/internal/slicesx"
	"github.com/shutthegoatup/prospector/internal/store"
)

// WebhooksAPI serves the endpoints an organisation's events are sent to.
type WebhooksAPI struct{ Endpoints *WebhookStore }

func endpointOut(e store.WebhookEndpoint, names map[int64]string) api.WebhookEndpoint {
	return api.WebhookEndpoint{Id: e.ID, Url: e.URL, Key: e.KeyID, KeyName: names[e.KeyID], Events: slicesx.NonNil(e.EventList()),
		CreatedBy: e.CreatedBy, CreatedAt: e.CreatedAt, DisabledAt: e.DisabledAt, DisabledWhy: e.DisabledWhy}
}

func (s WebhooksAPI) one(org int64, e *store.WebhookEndpoint) (api.WebhookEndpoint, error) {
	names, err := s.Endpoints.KeyNames(org)
	if err != nil {
		return api.WebhookEndpoint{}, err
	}
	return endpointOut(*e, names), nil
}

func (s WebhooksAPI) ListWebhookEvents(ctx context.Context, in api.ListWebhookEventsRequestObject) (api.ListWebhookEventsResponseObject, error) {
	page, next, err := paging.ListPage(store.EventTypes, in.Params.Limit, in.Params.Cursor)
	if err != nil {
		return nil, err
	}
	out := api.ListWebhookEvents200JSONResponse{Headers: api.ListWebhookEvents200ResponseHeaders{Link: request.NextLink(ctx, next)}}
	out.Body.Items = slicesx.Map(page, func(t store.EventType) api.WebhookEventType {
		return api.WebhookEventType{Name: t.Name, About: t.About.In(request.LanguageFrom(ctx))}
	})
	out.Body.NextCursor = next
	return out, nil
}

func (s WebhooksAPI) ListWebhooks(ctx context.Context, in api.ListWebhooksRequestObject) (api.ListWebhooksResponseObject, error) {
	org := request.ReqFrom(ctx).Org.ID
	all, err := s.Endpoints.Webhooks(org)
	if err != nil {
		return nil, err
	}
	page, next, err := paging.ListPage(all, in.Params.Limit, in.Params.Cursor)
	if err != nil {
		return nil, err
	}
	names, err := s.Endpoints.KeyNames(org)
	if err != nil {
		return nil, err
	}
	out := api.ListWebhooks200JSONResponse{Headers: api.ListWebhooks200ResponseHeaders{Link: request.NextLink(ctx, next)}}
	out.Body.Items = slicesx.Map(page, func(e store.WebhookEndpoint) api.WebhookEndpoint { return endpointOut(e, names) })
	out.Body.NextCursor = next
	return out, nil
}

func (s WebhooksAPI) CreateWebhook(ctx context.Context, in api.CreateWebhookRequestObject) (api.CreateWebhookResponseObject, error) {
	if in.Body == nil {
		return nil, say.New(`send {"url": "https://…", "key": 1, "events": ["company.added"]}`)
	}
	q := request.ReqFrom(ctx)
	e, secret, err := s.Endpoints.AddWebhook(q.Org.ID, in.Body.Key, in.Body.Url, in.Body.Events, q.Actor())
	if err != nil {
		return nil, err
	}
	out, err := s.one(q.Org.ID, e)
	if err != nil {
		return nil, err
	}
	return api.CreateWebhook201JSONResponse{Webhook: out, Secret: secret}, nil
}

func (s WebhooksAPI) GetWebhook(ctx context.Context, in api.GetWebhookRequestObject) (api.GetWebhookResponseObject, error) {
	org := request.ReqFrom(ctx).Org.ID
	e, err := s.Endpoints.Webhook(org, in.Id)
	if err != nil {
		return nil, err
	}
	out, err := s.one(org, e)
	if err != nil {
		return nil, err
	}
	return api.GetWebhook200JSONResponse(out), nil
}

func (s WebhooksAPI) ChangeWebhook(ctx context.Context, in api.ChangeWebhookRequestObject) (api.ChangeWebhookResponseObject, error) {
	if in.Body == nil {
		return nil, say.New(`send what changes, such as {"enabled": true}`)
	}
	q := request.ReqFrom(ctx)
	e, err := s.Endpoints.ChangeWebhook(q.Org.ID, in.Id, WebhookChange{URL: in.Body.Url, Events: in.Body.Events, Enabled: in.Body.Enabled}, q.Actor())
	if err != nil {
		return nil, err
	}
	out, err := s.one(q.Org.ID, e)
	if err != nil {
		return nil, err
	}
	return api.ChangeWebhook200JSONResponse(out), nil
}

func (s WebhooksAPI) DeleteWebhook(ctx context.Context, in api.DeleteWebhookRequestObject) (api.DeleteWebhookResponseObject, error) {
	q := request.ReqFrom(ctx)
	if err := s.Endpoints.DeleteWebhook(q.Org.ID, in.Id, q.Actor()); err != nil {
		return nil, err
	}
	return api.DeleteWebhook204Response{}, nil
}

func (s WebhooksAPI) RotateWebhookSecret(ctx context.Context, in api.RotateWebhookSecretRequestObject) (api.RotateWebhookSecretResponseObject, error) {
	q := request.ReqFrom(ctx)
	secret, err := s.Endpoints.RotateWebhookSecret(q.Org.ID, in.Id, q.Actor())
	if err != nil {
		return nil, err
	}
	return api.RotateWebhookSecret200JSONResponse{Secret: secret}, nil
}

func (s WebhooksAPI) ListWebhookDeliveries(ctx context.Context, in api.ListWebhookDeliveriesRequestObject) (api.ListWebhookDeliveriesResponseObject, error) {
	all, err := s.Endpoints.Deliveries(request.ReqFrom(ctx).Org.ID, in.Id)
	if err != nil {
		return nil, err
	}
	page, next, err := paging.ListPage(all, in.Params.Limit, in.Params.Cursor)
	if err != nil {
		return nil, err
	}
	out := api.ListWebhookDeliveries200JSONResponse{Headers: api.ListWebhookDeliveries200ResponseHeaders{Link: request.NextLink(ctx, next)}}
	out.Body.Items = slicesx.Map(page, func(d store.WebhookDelivery) api.WebhookDelivery {
		return api.WebhookDelivery{Id: d.ID, Type: d.Type, MessageId: d.MessageID, Status: api.WebhookDeliveryStatus(d.Status), Attempts: d.Attempts,
			LastCode: d.LastCode, LastError: d.LastError, NextAt: d.NextAt, CreatedAt: d.CreatedAt, DeliveredAt: d.DeliveredAt}
	})
	out.Body.NextCursor = next
	return out, nil
}
