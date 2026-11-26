package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/shutthegoatup/prospector/api/signer"
)

// The signing API is served from its own spec (api/signer): no key or member session
// reaches it, and everything after opening goes by the signer cookie alone.

type signerServer struct{ a *App }

var _ signer.StrictServerInterface = signerServer{}

type signerRequestKey struct{}

func (a *App) routeSigning(mux *http.ServeMux) {
	strict := signer.NewStrictHandlerWithOptions(signerServer{a}, []signer.StrictMiddlewareFunc{
		func(f signer.StrictHandlerFunc, _ string) signer.StrictHandlerFunc {
			return func(ctx context.Context, w http.ResponseWriter, r *http.Request, in any) (any, error) {
				// The link is in the page's address: it goes nowhere else.
				w.Header().Set("Referrer-Policy", "no-referrer")
				return f(context.WithValue(ctx, signerRequestKey{}, r), w, r, in)
			}
		},
	}, signer.StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			writeProblem(w, http.StatusBadRequest, err.Error())
		},
		ResponseErrorHandlerFunc: apiFail,
	})
	signer.HandlerWithOptions(strict, signer.StdHTTPServerOptions{
		BaseRouter: mux,
		ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			writeProblem(w, http.StatusBadRequest, err.Error())
		},
	})
}

func signerRequest(ctx context.Context) *http.Request {
	return ctx.Value(signerRequestKey{}).(*http.Request)
}

// session is the signing the request's cookie names.
func (s signerServer) session(ctx context.Context) (*Signing, string, error) {
	c, err := signerRequest(ctx).Cookie(signerCookie)
	if err != nil {
		return nil, "", errNoSigning
	}
	g, err := s.a.s.SigningBySession(c.Value)
	return g, c.Value, err
}

func signerFrom(ctx context.Context) Who {
	r := signerRequest(ctx)
	return Who{IP: clientIP(r), Browser: browserOf(r)}
}

// view is what a session sees: before the code, only who is asking and what.
func (s signerServer) view(g *Signing) (signer.Signing, error) {
	out := signer.Signing{Org: g.Agreement.SentOrg, Agreement: g.Agreement.Name, SentTo: masked(g.Recipient.SentEmail), Proven: g.Session.Proven,
		State: stateOf(g), Consent: signingConsent, Signers: []struct {
			Name   string `json:"name"`
			Signed bool   `json:"signed"`
			You    bool   `json:"you"`
		}{}}
	if !g.Session.Proven {
		return out, nil
	}
	out.Name, out.Message, out.Pages, out.ExpiresAt = g.Recipient.SentName, g.Agreement.Message, g.Agreement.PageCount, g.Agreement.ExpiresAt
	var signers []AgreementRecipient
	if err := s.a.s.db.Where("org_id = ? and agreement_id = ? and role = ?", g.Org.ID, g.Agreement.ID, RecipientSigner).Order("position, id").Find(&signers).Error; err != nil {
		return out, err
	}
	for _, r := range signers {
		out.Signers = append(out.Signers, struct {
			Name   string `json:"name"`
			Signed bool   `json:"signed"`
			You    bool   `json:"you"`
		}{Name: r.SentName, Signed: r.Status == SignerSigned, You: r.ID == g.Recipient.ID})
	}
	var fields []AgreementField
	if err := s.a.s.db.Where("org_id = ? and agreement_id = ? and round = ? and recipient_id = ?", g.Org.ID, g.Agreement.ID, g.Agreement.Round, g.Recipient.ID).
		Order("page, y, x, id").Find(&fields).Error; err != nil {
		return out, err
	}
	for _, f := range fields {
		out.Fields = append(out.Fields, struct {
			H     float32                  `json:"h"`
			Id    int64                    `json:"id"`
			Kind  signer.SigningFieldsKind `json:"kind"`
			Label string                   `json:"label"`
			Page  int                      `json:"page"`
			W     float32                  `json:"w"`
			X     float32                  `json:"x"`
			Y     float32                  `json:"y"`
		}{H: float32(f.H), Id: f.ID, Kind: signer.SigningFieldsKind(f.Kind), Label: f.Label, Page: f.Page, W: float32(f.W), X: float32(f.X), Y: float32(f.Y)})
	}
	return out, nil
}

// stateOf is where a signing stands, for the one it was sent to.
func stateOf(g *Signing) signer.SigningState {
	a, r := g.Agreement, g.Recipient
	switch a.Status {
	case AgreementSent:
		if a.ExpiresAt != nil && time.Now().After(*a.ExpiresAt) {
			return signer.Expired
		}
		switch {
		case r.Role == RecipientCopy:
			return signer.Copy
		case r.Status == SignerDue:
			return signer.Sign
		case r.Status == SignerWaiting:
			return signer.Waiting
		case r.Status == SignerSigned:
			return signer.Signed
		}
		return signer.Declined
	case AgreementSealing:
		return signer.Sealing
	case AgreementCompleted:
		return signer.Completed
	case AgreementDeclined:
		return signer.Declined
	case AgreementExpired:
		return signer.Expired
	}
	return signer.Voided
}

func (s signerServer) OpenSigning(ctx context.Context, in signer.OpenSigningRequestObject) (signer.OpenSigningResponseObject, error) {
	if in.Body == nil {
		return nil, errNoSigning
	}
	current := ""
	if c, err := signerRequest(ctx).Cookie(signerCookie); err == nil {
		current = c.Value
	}
	token, g, err := s.a.s.OpenSigning(in.Body.Link, current)
	if err != nil {
		return nil, err
	}
	out, err := s.view(g)
	if err != nil {
		return nil, err
	}
	cookie := &http.Cookie{Name: signerCookie, Value: token, Path: "/s/", HttpOnly: true, Secure: s.a.secure(signerRequest(ctx)),
		SameSite: http.SameSiteStrictMode, MaxAge: int(sessionProven / time.Second)}
	return signer.OpenSigning200JSONResponse{Body: out, Headers: signer.OpenSigning200ResponseHeaders{SetCookie: cookie.String()}}, nil
}

func (s signerServer) GetSigning(ctx context.Context, _ signer.GetSigningRequestObject) (signer.GetSigningResponseObject, error) {
	g, _, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	out, err := s.view(g)
	return signer.GetSigning200JSONResponse(out), err
}

func (s signerServer) SendSigningCode(ctx context.Context, _ signer.SendSigningCodeRequestObject) (signer.SendSigningCodeResponseObject, error) {
	g, token, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	via, err := s.a.campaignMail(g.Org.ID)
	if err != nil || via == nil {
		return nil, errors.New("the code can't be emailed just now: ask whoever sent you this")
	}
	who := signerFrom(ctx)
	_, err = s.a.s.SigningCode(g, token, who.IP, who.Browser, func(to, code string) error {
		return s.a.deliver(ctx, *via, Message{To: to, Subject: fmt.Sprintf("Your code to sign %s: %s", g.Agreement.Name, code),
			Body: fmt.Sprintf("Your code to sign %s, from %s, is:\n\n%s\n\nIt lasts ten minutes. If you didn't ask for it, you can ignore this mail.\n", g.Agreement.Name, g.Agreement.SentOrg, code)})
	})
	if err != nil {
		return nil, err
	}
	return signer.SendSigningCode200JSONResponse{SentTo: masked(g.Recipient.SentEmail)}, nil
}

func (s signerServer) CheckSigningCode(ctx context.Context, in signer.CheckSigningCodeRequestObject) (signer.CheckSigningCodeResponseObject, error) {
	if in.Body == nil {
		return nil, errors.New("send the code")
	}
	g, token, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	who := signerFrom(ctx)
	if err := s.a.s.ProveSigning(g, token, in.Body.Code, who.IP, who.Browser); err != nil {
		return nil, err
	}
	if g, err = s.a.s.SigningBySession(token); err != nil {
		return nil, err
	}
	out, err := s.view(g)
	return signer.CheckSigningCode200JSONResponse(out), err
}

func (s signerServer) GetSigningDocument(ctx context.Context, _ signer.GetSigningDocumentRequestObject) (signer.GetSigningDocumentResponseObject, error) {
	g, _, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	_, data, err := s.a.s.SignerDocument(g, signerFrom(ctx))
	if err != nil {
		return nil, err
	}
	return signer.GetSigningDocument200ApplicationpdfResponse{Body: bytes.NewReader(data), ContentLength: int64(len(data))}, nil
}

func (s signerServer) GetSealedDocument(ctx context.Context, _ signer.GetSealedDocumentRequestObject) (signer.GetSealedDocumentResponseObject, error) {
	g, _, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	if err := proven(g); err != nil {
		return nil, err
	}
	ag, data, err := s.a.s.SealedDocument(g.Org.ID, g.Agreement.ID)
	if err != nil {
		return nil, err
	}
	return signer.GetSealedDocument200ApplicationpdfResponse{Body: bytes.NewReader(data), ContentLength: int64(len(data)),
		Headers: signer.GetSealedDocument200ResponseHeaders{ContentDisposition: sealedName(ag)}}, nil
}

func (s signerServer) SignAgreement(ctx context.Context, in signer.SignAgreementRequestObject) (signer.SignAgreementResponseObject, error) {
	if in.Body == nil {
		return nil, errors.New("send {\"typed_name\": …, \"consent\": true}")
	}
	g, token, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	values := map[int64]string{}
	for _, v := range in.Body.Values {
		values[v.Field] = v.Value
	}
	if _, err := s.a.s.SignerSigns(g, SignIn{Typed: in.Body.TypedName, Consent: in.Body.Consent, Values: values}, signerFrom(ctx)); err != nil {
		return nil, err
	}
	if g, err = s.a.s.SigningBySession(token); err != nil {
		return nil, err
	}
	out, err := s.view(g)
	return signer.SignAgreement200JSONResponse(out), err
}

func (s signerServer) DeclineAgreement(ctx context.Context, in signer.DeclineAgreementRequestObject) (signer.DeclineAgreementResponseObject, error) {
	g, token, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	reason := ""
	if in.Body != nil {
		reason = in.Body.Reason
	}
	if _, err := s.a.s.SignerDeclines(g, reason, signerFrom(ctx)); err != nil {
		return nil, err
	}
	if g, err = s.a.s.SigningBySession(token); err != nil {
		return nil, err
	}
	out, err := s.view(g)
	return signer.DeclineAgreement200JSONResponse(out), err
}
