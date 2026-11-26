package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/shutthegoatup/prospector/api"
)

func agreementOut(a Agreement) api.Agreement {
	return api.Agreement{Id: a.ID, CompanyId: a.CompanyID, Name: a.Name, Status: api.AgreementStatus(a.Status), FileName: a.FileName,
		FileSize: a.FileSize, FileHash: a.FileHash, PageCount: a.PageCount, Round: a.Round, InOrder: a.InOrder, CreatedBy: a.CreatedBy, UpdatedBy: a.UpdatedBy, CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt,
		SentAt: a.SentAt, SentBy: a.SentBy, ExpiresAt: a.ExpiresAt, Subject: a.Subject, Message: a.Message, SealedHash: a.SealedHash, CompletedAt: a.CompletedAt, SealProblem: a.SealProblem}
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

func (s apiServer) GetAgreement(ctx context.Context, in api.GetAgreementRequestObject) (api.GetAgreementResponseObject, error) {
	q := reqFrom(ctx)
	p, err := s.a.s.Preparing(q.Org, in.Id)
	if err != nil {
		return nil, err
	}
	a := agreementOut(p.Agreement)
	out := api.AgreementOverview{Id: a.Id, CompanyId: a.CompanyId, Name: a.Name, Status: api.AgreementOverviewStatus(a.Status), FileName: a.FileName, FileSize: a.FileSize,
		FileHash: a.FileHash, PageCount: a.PageCount, Round: a.Round, InOrder: a.InOrder, CreatedBy: a.CreatedBy, UpdatedBy: a.UpdatedBy, CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt,
		SentAt: a.SentAt, SentBy: a.SentBy, ExpiresAt: a.ExpiresAt, Subject: a.Subject, Message: a.Message, SealedHash: a.SealedHash, CompletedAt: a.CompletedAt, SealProblem: a.SealProblem,
		Company: p.Company.Name, Problem: p.Problem, People: []api.Invitable{}, Members: []api.Invitable{}, Missing: p.Missing, CantSend: s.a.canSend(q.Org), Consent: signingConsent}
	if out.Missing == nil {
		out.Missing = []string{}
	}
	for _, r := range p.Parties {
		if q.User == nil || r.UserID == nil || *r.UserID != q.User.ID || r.Role != RecipientSigner {
			continue
		}
		out.MyRecipient = r.ID
		out.MyTurn = r.Status == SignerDue && p.Agreement.Status == AgreementSent
	}
	for _, c := range p.People {
		if !p.on(c.ID, 0) {
			out.People = append(out.People, api.Invitable{Id: c.ID, Name: displayName(c), Email: strings.TrimSpace(deref(c.Email))})
		}
	}
	for _, m := range p.Members {
		if !p.on(0, m.UserID) {
			out.Members = append(out.Members, api.Invitable{Id: m.UserID, Name: m.Name, Email: m.Email})
		}
	}
	return api.GetAgreement200JSONResponse(out), nil
}

func (s apiServer) ChangeAgreement(ctx context.Context, in api.ChangeAgreementRequestObject) (api.ChangeAgreementResponseObject, error) {
	if in.Body == nil || (in.Body.Name == nil && in.Body.InOrder == nil) {
		return nil, errors.New("send what changes: {\"name\": …} or {\"in_order\": true}")
	}
	q := reqFrom(ctx)
	a, err := s.a.s.ChangeAgreement(q.Org.ID, in.Id, in.Body.Name, in.Body.InOrder, q.Actor())
	if err != nil {
		return nil, err
	}
	return api.ChangeAgreement200JSONResponse(agreementOut(*a)), nil
}

func recipientOut(r Party) api.AgreementRecipient {
	// Once sent, who it went to is as it was sent: frozen, and kept when they're detached.
	name, email := r.Name, r.Email
	if r.SentEmail != "" {
		name, email = r.SentName, r.SentEmail
	}
	return api.AgreementRecipient{Id: r.ID, AgreementId: r.AgreementID, Position: r.Position, Role: api.AgreementRecipientRole(r.Role),
		ContactId: r.ContactID, MemberId: r.UserID, Name: name, Email: email, CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt,
		Status: api.AgreementRecipientStatus(r.Status), SignedAt: r.SignedAt, DeclineReason: r.DeclineReason}
}

// party is one recipient with who they are, as the list gives them.
func (s apiServer) party(org, id, recipient int64) (api.AgreementRecipient, error) {
	all, err := s.a.s.AgreementRecipients(org, id)
	if err != nil {
		return api.AgreementRecipient{}, err
	}
	for _, r := range all {
		if r.ID == recipient {
			return recipientOut(r), nil
		}
	}
	return api.AgreementRecipient{}, noSuch("no recipient %d on agreement %d", recipient, id)
}

func (s apiServer) ListAgreementRecipients(ctx context.Context, in api.ListAgreementRecipientsRequestObject) (api.ListAgreementRecipientsResponseObject, error) {
	all, err := s.a.s.AgreementRecipients(reqFrom(ctx).Org.ID, in.Id)
	if err != nil {
		return nil, err
	}
	page, next, err := listPage(all, in.Params.Limit, in.Params.Cursor)
	if err != nil {
		return nil, err
	}
	problems, err := s.a.s.MailProblems(reqFrom(ctx).Org.ID, in.Id)
	if err != nil {
		return nil, err
	}
	out := api.ListAgreementRecipients200JSONResponse{Headers: api.ListAgreementRecipients200ResponseHeaders{Link: nextLink(ctx, next)}}
	out.Body.Items, out.Body.NextCursor = []api.AgreementRecipient{}, next
	for _, r := range page {
		item := recipientOut(r)
		item.MailProblem = problems[r.ID]
		out.Body.Items = append(out.Body.Items, item)
	}
	return out, nil
}

func (s apiServer) AddAgreementRecipient(ctx context.Context, in api.AddAgreementRecipientRequestObject) (api.AddAgreementRecipientResponseObject, error) {
	if in.Body == nil {
		return nil, errors.New("send who: {\"role\": \"signer\", \"contact\": id} or {\"role\": \"copy\", \"member\": id}")
	}
	q := reqFrom(ctx)
	r, err := s.a.s.AddRecipient(q.Org, in.Id, string(in.Body.Role), in.Body.Contact, in.Body.Member, q.Actor())
	if err != nil {
		return nil, err
	}
	out, err := s.party(q.Org.ID, in.Id, r.ID)
	return api.AddAgreementRecipient201JSONResponse(out), err
}

func (s apiServer) ChangeAgreementRecipient(ctx context.Context, in api.ChangeAgreementRecipientRequestObject) (api.ChangeAgreementRecipientResponseObject, error) {
	if in.Body == nil || (in.Body.Role == nil && in.Body.Position == nil) {
		return nil, errors.New("send what changes: {\"position\": 1} or {\"role\": \"copy\"}")
	}
	q := reqFrom(ctx)
	var role *string
	if in.Body.Role != nil {
		r := string(*in.Body.Role)
		role = &r
	}
	if _, err := s.a.s.ChangeRecipient(q.Org.ID, in.Id, in.Recipient, role, in.Body.Position); err != nil {
		return nil, err
	}
	out, err := s.party(q.Org.ID, in.Id, in.Recipient)
	return api.ChangeAgreementRecipient200JSONResponse(out), err
}

func (s apiServer) RemoveAgreementRecipient(ctx context.Context, in api.RemoveAgreementRecipientRequestObject) (api.RemoveAgreementRecipientResponseObject, error) {
	if err := s.a.s.RemoveRecipient(reqFrom(ctx).Org.ID, in.Id, in.Recipient); err != nil {
		return nil, err
	}
	return api.RemoveAgreementRecipient204Response{}, nil
}

func fieldOut(f AgreementField) api.AgreementField {
	out := api.AgreementField{Id: f.ID, Round: f.Round, RecipientId: f.RecipientID, Page: f.Page, Kind: api.AgreementFieldKind(f.Kind), Label: f.Label, Value: f.Value}
	out.X, out.Y, out.W, out.H = float32(f.X), float32(f.Y), float32(f.W), float32(f.H)
	return out
}

func (s apiServer) ListAgreementFields(ctx context.Context, in api.ListAgreementFieldsRequestObject) (api.ListAgreementFieldsResponseObject, error) {
	all, err := s.a.s.AgreementFields(reqFrom(ctx).Org.ID, in.Id)
	if err != nil {
		return nil, err
	}
	page, next, err := listPage(all, in.Params.Limit, in.Params.Cursor)
	if err != nil {
		return nil, err
	}
	out := api.ListAgreementFields200JSONResponse{Headers: api.ListAgreementFields200ResponseHeaders{Link: nextLink(ctx, next)}}
	out.Body.Items, out.Body.NextCursor = []api.AgreementField{}, next
	for _, f := range page {
		out.Body.Items = append(out.Body.Items, fieldOut(f))
	}
	return out, nil
}

func (s apiServer) PutAgreementFields(ctx context.Context, in api.PutAgreementFieldsRequestObject) (api.PutAgreementFieldsResponseObject, error) {
	if in.Body == nil {
		return nil, errors.New("send every field: {\"items\": [...]}")
	}
	fields := make([]AgreementField, 0, len(in.Body.Items))
	for _, f := range in.Body.Items {
		fields = append(fields, AgreementField{RecipientID: f.RecipientId, Page: f.Page, Kind: string(f.Kind), X: float64(f.X), Y: float64(f.Y), W: float64(f.W), H: float64(f.H), Label: f.Label})
	}
	kept, err := s.a.s.PutAgreementFields(reqFrom(ctx).Org.ID, in.Id, fields)
	if err != nil {
		return nil, err
	}
	out := api.PutAgreementFields200JSONResponse{Items: []api.AgreementField{}}
	for _, f := range kept {
		out.Items = append(out.Items, fieldOut(f))
	}
	return out, nil
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

// uploads holds the one operation that takes a file: the body must be a PDF and not
// declared past the limit, both told before any of it is read; and the answer gets its
// own time once the body is in, so a slow upload that was kept is not reported as failed.
func uploads(f api.StrictHandlerFunc, op string) api.StrictHandlerFunc {
	if op != "PutAgreementDocument" {
		return f
	}
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request, in any) (any, error) {
		if kind, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); kind != "application/pdf" {
			writeProblem(w, http.StatusUnsupportedMediaType, "send the document as application/pdf")
			return nil, nil
		}
		if r.ContentLength > agreementFileLimit {
			writeProblem(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("a document is %d MB at most", agreementFileLimit>>20))
			return nil, nil
		}
		out, err := f(ctx, w, r, in)
		http.NewResponseController(w).SetWriteDeadline(time.Now().Add(answerLimit))
		return out, err
	}
}
