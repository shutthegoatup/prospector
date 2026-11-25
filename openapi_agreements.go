package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"

	"github.com/shutthegoatup/prospector/api"
)

func agreementOut(a Agreement) api.Agreement {
	return api.Agreement{Id: a.ID, CompanyId: a.CompanyID, Name: a.Name, Status: api.AgreementStatus(a.Status), FileName: a.FileName,
		FileSize: a.FileSize, FileHash: a.FileHash, CreatedBy: a.CreatedBy, UpdatedBy: a.UpdatedBy, CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt}
}

func (s apiServer) ListAgreements(ctx context.Context, in api.ListAgreementsRequestObject) (api.ListAgreementsResponseObject, error) {
	all, err := s.a.s.Agreements(reqFrom(ctx).Org.ID, in.Params.Company)
	if err != nil {
		return nil, err
	}
	page, next, err := listPage(all, in.Params.Limit, in.Params.Cursor)
	if err != nil {
		return nil, err
	}
	out := api.ListAgreements200JSONResponse{Headers: api.ListAgreements200ResponseHeaders{Link: nextLink(ctx, next)}}
	out.Body.Items, out.Body.NextCursor = []api.Agreement{}, next
	for _, a := range page {
		out.Body.Items = append(out.Body.Items, agreementOut(a))
	}
	return out, nil
}

func (s apiServer) CreateAgreement(ctx context.Context, in api.CreateAgreementRequestObject) (api.CreateAgreementResponseObject, error) {
	if in.Body == nil {
		return nil, errors.New("send the agreement: {\"company\": id, \"name\": …}")
	}
	q := reqFrom(ctx)
	a, err := s.a.s.AddAgreement(q.Org.ID, in.Body.Company, in.Body.Name, q.Actor())
	if err != nil {
		return nil, err
	}
	return api.CreateAgreement201JSONResponse(agreementOut(*a)), nil
}

func (s apiServer) RenameAgreement(ctx context.Context, in api.RenameAgreementRequestObject) (api.RenameAgreementResponseObject, error) {
	if in.Body == nil {
		return nil, errors.New("send the new name: {\"name\": …}")
	}
	q := reqFrom(ctx)
	a, err := s.a.s.RenameAgreement(q.Org.ID, in.Id, in.Body.Name, q.Actor())
	if err != nil {
		return nil, err
	}
	return api.RenameAgreement200JSONResponse(agreementOut(*a)), nil
}

func (s apiServer) RemoveAgreement(ctx context.Context, in api.RemoveAgreementRequestObject) (api.RemoveAgreementResponseObject, error) {
	if err := s.a.s.RemoveAgreement(reqFrom(ctx).Org.ID, in.Id); err != nil {
		return nil, err
	}
	return api.RemoveAgreement204Response{}, nil
}

func (s apiServer) PutAgreementDocument(ctx context.Context, in api.PutAgreementDocumentRequestObject) (api.PutAgreementDocumentResponseObject, error) {
	if in.Body == nil {
		return nil, errors.New("send the PDF as the body")
	}
	// One byte past the limit is enough to know it is too big, without reading the rest.
	data, err := io.ReadAll(io.LimitReader(in.Body, agreementFileLimit+1))
	if err != nil {
		return nil, fmt.Errorf("reading the document: %w", err)
	}
	q := reqFrom(ctx)
	a, err := s.a.s.PutAgreementFile(q.Org.ID, in.Id, in.Params.Name, data, q.Actor())
	if err != nil {
		return nil, err
	}
	return api.PutAgreementDocument200JSONResponse(agreementOut(*a)), nil
}

func (s apiServer) GetAgreementDocument(ctx context.Context, in api.GetAgreementDocumentRequestObject) (api.GetAgreementDocumentResponseObject, error) {
	a, data, err := s.a.s.AgreementDocument(reqFrom(ctx).Org.ID, in.Id)
	if err != nil {
		return nil, err
	}
	// Always a download, whatever the browser would rather do with a PDF.
	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": a.FileName})
	return api.GetAgreementDocument200ApplicationpdfResponse{Body: bytes.NewReader(data), ContentLength: int64(len(data)),
		Headers: api.GetAgreementDocument200ResponseHeaders{ContentDisposition: disposition}}, nil
}
