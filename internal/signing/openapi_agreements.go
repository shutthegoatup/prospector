package signing

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime"
	"strings"

	"github.com/shutthegoatup/prospector/internal/slicesx"

	"github.com/shutthegoatup/prospector/api"

	"github.com/shutthegoatup/prospector/internal/paging"
	"github.com/shutthegoatup/prospector/internal/request"
	"github.com/shutthegoatup/prospector/internal/say"
	"github.com/shutthegoatup/prospector/internal/store"
)

func AgreementOut(a store.Agreement) api.Agreement {
	return api.Agreement{Id: a.ID, CompanyId: a.CompanyID, Name: a.Name, Status: api.AgreementStatus(a.Status), FileName: a.FileName,
		FileSize: a.FileSize, FileHash: a.FileHash, PageCount: a.PageCount, Round: a.Round, InOrder: a.InOrder, CreatedBy: a.CreatedBy, UpdatedBy: a.UpdatedBy, CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt}
}

func (s SigningAPI) ListAgreements(ctx context.Context, in api.ListAgreementsRequestObject) (api.ListAgreementsResponseObject, error) {
	all, err := s.Store.Agreements(request.ReqFrom(ctx).Org.ID, in.Params.Company)
	if err != nil {
		return nil, err
	}
	page, next, err := paging.ListPage(all, in.Params.Limit, in.Params.Cursor)
	if err != nil {
		return nil, err
	}
	out := api.ListAgreements200JSONResponse{Headers: api.ListAgreements200ResponseHeaders{Link: request.NextLink(ctx, next)}}
	out.Body.Items, out.Body.NextCursor = slicesx.Map(page, AgreementOut), next
	return out, nil
}

func (s SigningAPI) CreateAgreement(ctx context.Context, in api.CreateAgreementRequestObject) (api.CreateAgreementResponseObject, error) {
	if in.Body == nil {
		return nil, say.New("send the agreement: {\"company\": id, \"name\": …}")
	}
	q := request.ReqFrom(ctx)
	a, err := s.Store.AddAgreement(q.Org.ID, in.Body.Company, in.Body.Name, q.Actor())
	if err != nil {
		return nil, err
	}
	return api.CreateAgreement201JSONResponse(AgreementOut(*a)), nil
}

func (s SigningAPI) GetAgreement(ctx context.Context, in api.GetAgreementRequestObject) (api.GetAgreementResponseObject, error) {
	q := request.ReqFrom(ctx)
	p, err := s.Store.Preparing(q.Org, in.Id)
	if err != nil {
		return nil, err
	}
	a := AgreementOut(p.Agreement)
	out := api.AgreementOverview{Id: a.Id, CompanyId: a.CompanyId, Name: a.Name, Status: api.AgreementOverviewStatus(a.Status), FileName: a.FileName, FileSize: a.FileSize,
		FileHash: a.FileHash, PageCount: a.PageCount, Round: a.Round, InOrder: a.InOrder, CreatedBy: a.CreatedBy, UpdatedBy: a.UpdatedBy, CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt,
		Company: p.Company.Name, Problem: p.Problem, People: []api.Invitable{}, Members: []api.Invitable{}}
	for _, c := range p.People {
		if !p.On(c.ID, 0) {
			out.People = append(out.People, api.Invitable{Id: c.ID, Name: store.DisplayName(c), Email: strings.TrimSpace(store.Deref(c.Email))})
		}
	}
	for _, m := range p.Members {
		if !p.On(0, m.UserID) {
			out.Members = append(out.Members, api.Invitable{Id: m.UserID, Name: m.Name, Email: m.Email})
		}
	}
	return api.GetAgreement200JSONResponse(out), nil
}

func (s SigningAPI) ChangeAgreement(ctx context.Context, in api.ChangeAgreementRequestObject) (api.ChangeAgreementResponseObject, error) {
	if in.Body == nil || (in.Body.Name == nil && in.Body.InOrder == nil) {
		return nil, say.New("send what changes: {\"name\": …} or {\"in_order\": true}")
	}
	q := request.ReqFrom(ctx)
	a, err := s.Store.ChangeAgreement(q.Org.ID, in.Id, in.Body.Name, in.Body.InOrder, q.Actor())
	if err != nil {
		return nil, err
	}
	return api.ChangeAgreement200JSONResponse(AgreementOut(*a)), nil
}

func recipientOut(r Party) api.AgreementRecipient {
	return api.AgreementRecipient{Id: r.ID, AgreementId: r.AgreementID, Position: r.Position, Role: api.AgreementRecipientRole(r.Role),
		ContactId: r.ContactID, MemberId: r.UserID, Name: r.Name, Email: r.Email, CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt}
}

// party is one recipient with who they are, as the list gives them.
func (s SigningAPI) party(org, id, recipient int64) (api.AgreementRecipient, error) {
	all, err := s.Store.AgreementRecipients(org, id)
	if err != nil {
		return api.AgreementRecipient{}, err
	}
	for _, r := range all {
		if r.ID == recipient {
			return recipientOut(r), nil
		}
	}
	return api.AgreementRecipient{}, store.NoSuch("no recipient %d on agreement %d", recipient, id)
}

func (s SigningAPI) ListAgreementRecipients(ctx context.Context, in api.ListAgreementRecipientsRequestObject) (api.ListAgreementRecipientsResponseObject, error) {
	all, err := s.Store.AgreementRecipients(request.ReqFrom(ctx).Org.ID, in.Id)
	if err != nil {
		return nil, err
	}
	page, next, err := paging.ListPage(all, in.Params.Limit, in.Params.Cursor)
	if err != nil {
		return nil, err
	}
	out := api.ListAgreementRecipients200JSONResponse{Headers: api.ListAgreementRecipients200ResponseHeaders{Link: request.NextLink(ctx, next)}}
	out.Body.Items, out.Body.NextCursor = slicesx.Map(page, recipientOut), next
	return out, nil
}

func (s SigningAPI) AddAgreementRecipient(ctx context.Context, in api.AddAgreementRecipientRequestObject) (api.AddAgreementRecipientResponseObject, error) {
	if in.Body == nil {
		return nil, say.New("send who: {\"role\": \"signer\", \"contact\": id} or {\"role\": \"copy\", \"member\": id}")
	}
	q := request.ReqFrom(ctx)
	r, err := s.Store.AddRecipient(q.Org, in.Id, string(in.Body.Role), in.Body.Contact, in.Body.Member, q.Actor())
	if err != nil {
		return nil, err
	}
	out, err := s.party(q.Org.ID, in.Id, r.ID)
	return api.AddAgreementRecipient201JSONResponse(out), err
}

func (s SigningAPI) ChangeAgreementRecipient(ctx context.Context, in api.ChangeAgreementRecipientRequestObject) (api.ChangeAgreementRecipientResponseObject, error) {
	if in.Body == nil || (in.Body.Role == nil && in.Body.Position == nil) {
		return nil, say.New("send what changes: {\"position\": 1} or {\"role\": \"copy\"}")
	}
	q := request.ReqFrom(ctx)
	var role *string
	if in.Body.Role != nil {
		r := string(*in.Body.Role)
		role = &r
	}
	if _, err := s.Store.ChangeRecipient(q.Org.ID, in.Id, in.Recipient, role, in.Body.Position); err != nil {
		return nil, err
	}
	out, err := s.party(q.Org.ID, in.Id, in.Recipient)
	return api.ChangeAgreementRecipient200JSONResponse(out), err
}

func (s SigningAPI) RemoveAgreementRecipient(ctx context.Context, in api.RemoveAgreementRecipientRequestObject) (api.RemoveAgreementRecipientResponseObject, error) {
	if err := s.Store.RemoveRecipient(request.ReqFrom(ctx).Org.ID, in.Id, in.Recipient); err != nil {
		return nil, err
	}
	return api.RemoveAgreementRecipient204Response{}, nil
}

func fieldOut(f store.AgreementField) api.AgreementField {
	out := api.AgreementField{Id: f.ID, Round: f.Round, RecipientId: f.RecipientID, Page: f.Page, Kind: api.AgreementFieldKind(f.Kind)}
	out.X, out.Y, out.W, out.H = float32(f.X), float32(f.Y), float32(f.W), float32(f.H)
	return out
}

func (s SigningAPI) ListAgreementFields(ctx context.Context, in api.ListAgreementFieldsRequestObject) (api.ListAgreementFieldsResponseObject, error) {
	all, err := s.Store.AgreementFields(request.ReqFrom(ctx).Org.ID, in.Id)
	if err != nil {
		return nil, err
	}
	page, next, err := paging.ListPage(all, in.Params.Limit, in.Params.Cursor)
	if err != nil {
		return nil, err
	}
	out := api.ListAgreementFields200JSONResponse{Headers: api.ListAgreementFields200ResponseHeaders{Link: request.NextLink(ctx, next)}}
	out.Body.Items, out.Body.NextCursor = slicesx.Map(page, fieldOut), next
	return out, nil
}

func (s SigningAPI) PutAgreementFields(ctx context.Context, in api.PutAgreementFieldsRequestObject) (api.PutAgreementFieldsResponseObject, error) {
	if in.Body == nil {
		return nil, say.New("send every field: {\"items\": [...]}")
	}
	fields := make([]store.AgreementField, 0, len(in.Body.Items))
	for _, f := range in.Body.Items {
		fields = append(fields, store.AgreementField{RecipientID: f.RecipientId, Page: f.Page, Kind: string(f.Kind), X: float64(f.X), Y: float64(f.Y), W: float64(f.W), H: float64(f.H)})
	}
	kept, err := s.Store.PutAgreementFields(request.ReqFrom(ctx).Org.ID, in.Id, fields)
	if err != nil {
		return nil, err
	}
	out := api.PutAgreementFields200JSONResponse{Items: []api.AgreementField{}}
	for _, f := range kept {
		out.Items = append(out.Items, fieldOut(f))
	}
	return out, nil
}

func (s SigningAPI) RemoveAgreement(ctx context.Context, in api.RemoveAgreementRequestObject) (api.RemoveAgreementResponseObject, error) {
	q := request.ReqFrom(ctx)
	if err := s.Store.RemoveAgreement(q.Org.ID, in.Id, q.Actor()); err != nil {
		return nil, err
	}
	return api.RemoveAgreement204Response{}, nil
}

func (s SigningAPI) PutAgreementDocument(ctx context.Context, in api.PutAgreementDocumentRequestObject) (api.PutAgreementDocumentResponseObject, error) {
	if in.Body == nil {
		return nil, say.New("send the PDF as the body")
	}
	// One byte past the limit is enough to know it is too big, without reading the rest.
	data, err := io.ReadAll(io.LimitReader(in.Body, AgreementFileLimit+1))
	if err != nil {
		return nil, fmt.Errorf("reading the document: %w", err)
	}
	q := request.ReqFrom(ctx)
	a, err := s.Store.PutAgreementFile(q.Org.ID, in.Id, in.Params.Name, data, q.Actor())
	if err != nil {
		return nil, err
	}
	return api.PutAgreementDocument200JSONResponse(AgreementOut(*a)), nil
}

func (s SigningAPI) GetAgreementDocument(ctx context.Context, in api.GetAgreementDocumentRequestObject) (api.GetAgreementDocumentResponseObject, error) {
	a, data, err := s.Store.AgreementDocument(request.ReqFrom(ctx).Org.ID, in.Id)
	if err != nil {
		return nil, err
	}
	// Always a download, whatever the browser would rather do with a PDF.
	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": a.FileName})
	return api.GetAgreementDocument200ApplicationpdfResponse{Body: bytes.NewReader(data), ContentLength: int64(len(data)),
		Headers: api.GetAgreementDocument200ResponseHeaders{ContentDisposition: disposition}}, nil
}

// SigningAPI is the signing product's operations, which apiServer embeds.
type SigningAPI struct{ Store *AgreementStore }
