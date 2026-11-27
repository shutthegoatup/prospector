package tickets

import (
	"context"
	"strings"

	"github.com/shutthegoatup/prospector/api"

	"github.com/shutthegoatup/prospector/internal/mailer"
	"github.com/shutthegoatup/prospector/internal/paging"
	"github.com/shutthegoatup/prospector/internal/request"
	"github.com/shutthegoatup/prospector/internal/say"
	"github.com/shutthegoatup/prospector/internal/store"
)

func messageOut(m store.EnquiryMessage) api.EnquiryMessage {
	return api.EnquiryMessage{Id: m.ID, EnquiryId: m.EnquiryID, Ours: m.Ours, Text: m.Text, By: m.By, SentAt: m.SentAt, CreatedAt: m.CreatedAt}
}

func (s TicketsAPI) ListEnquiryMessages(ctx context.Context, in api.ListEnquiryMessagesRequestObject) (api.ListEnquiryMessagesResponseObject, error) {
	thread, err := s.Enquiries.EnquiryThread(request.ReqFrom(ctx).Org.ID, in.Id)
	if err != nil {
		return nil, err
	}
	thread, next, err := paging.ListPage(thread, in.Params.Limit, in.Params.Cursor)
	if err != nil {
		return nil, err
	}
	out := api.ListEnquiryMessages200JSONResponse{Headers: api.ListEnquiryMessages200ResponseHeaders{Link: request.NextLink(ctx, next)}}
	out.Body.Items, out.Body.NextCursor = []api.EnquiryMessage{}, next
	for _, m := range thread {
		out.Body.Items = append(out.Body.Items, messageOut(m))
	}
	return out, nil
}

func (s TicketsAPI) ReplyToEnquiry(ctx context.Context, in api.ReplyToEnquiryRequestObject) (api.ReplyToEnquiryResponseObject, error) {
	if in.Body == nil {
		return nil, say.New("send the reply: {\"text\": …}")
	}
	q := request.ReqFrom(ctx)
	// Their answer goes to the member who wrote. A key has no mailbox of its own.
	answers := ""
	if q.Key == nil && q.User != nil {
		answers = q.User.Email
	}
	m, err := s.Mail.ReplyEnquiry(ctx, q.Org, in.Id, in.Body.Text, q.Actor(), answers)
	if err != nil {
		return nil, err
	}
	return api.ReplyToEnquiry201JSONResponse(messageOut(*m)), nil
}

func (s TicketsAPI) RecordEnquiryAnswer(ctx context.Context, in api.RecordEnquiryAnswerRequestObject) (api.RecordEnquiryAnswerResponseObject, error) {
	if in.Body == nil {
		return nil, say.New("send what they said: {\"text\": …}")
	}
	q := request.ReqFrom(ctx)
	m, err := s.Enquiries.HearEnquiry(q.Org.ID, in.Id, in.Body.Text, q.Actor())
	if err != nil {
		return nil, err
	}
	return api.RecordEnquiryAnswer201JSONResponse(messageOut(*m)), nil
}

func (s TicketsAPI) CountEnquiries(ctx context.Context, _ api.CountEnquiriesRequestObject) (api.CountEnquiriesResponseObject, error) {
	out := api.CountEnquiries200JSONResponse{Kinds: map[string]int{}, States: map[string]int{}}
	for key, n := range s.Enquiries.EnquiryCounts(request.ReqFrom(ctx).Org.ID) {
		state, isState := strings.CutPrefix(key, "state:")
		switch {
		case key == "":
			out.All = n
		case key == "spam":
			out.Spam = n
		case isState:
			out.States[state] = n
		default:
			out.Kinds[key] = n
		}
	}
	return out, nil
}

func enquiryOut(e store.Enquiry, around EnquiriesAround) api.Enquiry {
	out := api.Enquiry{Id: e.ID, Kind: e.Kind, Name: e.Name, Email: e.Email, Company: e.Company, Phone: e.Phone, Message: e.Message, Site: e.Site,
		Page: e.Page, Via: e.Via, Spam: e.Spam, ContactId: store.IDOf(e.ContactID), CompanyId: store.IDOf(e.CompanyID), CreatedAt: e.CreatedAt,
		State: e.State, Owner: e.Owner, Note: e.Note, CompanyName: around.Companies[store.IDOf(e.CompanyID)],
		InConversation: e.ContactID != nil && around.Talking[*e.ContactID], Thread: []api.EnquiryMessage{}}
	for _, m := range around.Threads[e.ID] {
		out.Thread = append(out.Thread, messageOut(m))
	}
	return out
}

func (s TicketsAPI) ListEnquiries(ctx context.Context, in api.ListEnquiriesRequestObject) (api.ListEnquiriesResponseObject, error) {
	p := in.Params
	page, err := paging.PageIn(p.Limit, p.Cursor)
	if err != nil {
		return nil, err
	}
	org := request.ReqFrom(ctx).Org.ID
	rows, next, err := s.Enquiries.EnquiriesBefore(org, EnquiryFilter{Kind: p.Kind, Spam: p.Spam, State: p.State}, page)
	if err != nil {
		return nil, err
	}
	around, err := s.Enquiries.AroundEnquiries(org, rows)
	if err != nil {
		return nil, err
	}
	via, _ := s.Mail.MailFor(org)
	out := api.ListEnquiries200JSONResponse{Headers: api.ListEnquiries200ResponseHeaders{Link: request.NextLink(ctx, next)}}
	out.Body.Items, out.Body.NextCursor, out.Body.CanReply = []api.Enquiry{}, next, via != nil
	for _, e := range rows {
		out.Body.Items = append(out.Body.Items, enquiryOut(e, around))
	}
	return out, nil
}

func (s TicketsAPI) ChangeEnquiry(ctx context.Context, in api.ChangeEnquiryRequestObject) (api.ChangeEnquiryResponseObject, error) {
	var changes []store.Change
	if b := in.Body; b != nil {
		for _, set := range []struct {
			field string
			value *string
		}{{"state", b.State}, {"owner", b.Owner}, {"note", b.Note}} {
			if set.value != nil {
				changes = append(changes, store.Change{Field: set.field, Value: *set.value})
			}
		}
	}
	if len(changes) == 0 {
		return nil, say.New("send what to change: state, owner or note")
	}
	q := request.ReqFrom(ctx)
	if err := s.Enquiries.SetEnquiryFields(q.Org.ID, in.Id, changes, q.Actor()); err != nil {
		return nil, err
	}
	e, err := s.Enquiries.EnquiryByID(q.Org.ID, in.Id)
	if err != nil {
		return nil, err
	}
	around, err := s.Enquiries.AroundEnquiries(q.Org.ID, []store.Enquiry{*e})
	if err != nil {
		return nil, err
	}
	return api.ChangeEnquiry200JSONResponse(enquiryOut(*e, around)), nil
}

func (s TicketsAPI) DeleteEnquiry(ctx context.Context, in api.DeleteEnquiryRequestObject) (api.DeleteEnquiryResponseObject, error) {
	if err := s.Enquiries.DeleteEnquiry(request.ReqFrom(ctx).Org.ID, in.Id); err != nil {
		return nil, err
	}
	return api.DeleteEnquiry204Response{}, nil
}

func (s TicketsAPI) ConverseEnquiry(ctx context.Context, in api.ConverseEnquiryRequestObject) (api.ConverseEnquiryResponseObject, error) {
	q := request.ReqFrom(ctx)
	c, err := s.Enquiries.ConverseEnquiry(q.Org, in.Id, q.Actor())
	if err != nil {
		return nil, err
	}
	return api.ConverseEnquiry201JSONResponse{Contact: c.ID}, nil
}

// TicketsAPI is the tickets product's operations: enquiries and their threads, which
// apiServer embeds.
type TicketsAPI struct {
	Enquiries *EnquiryStore
	Mail      ticketMail
}

// ticketMail is what tickets asks of the app: the server an organisation mails from,
// and sending a reply to whoever wrote in.
type ticketMail interface {
	MailFor(org int64) (*mailer.Server, error)
	ReplyEnquiry(ctx context.Context, o *store.Org, enquiry int64, text, actor, answers string) (*store.EnquiryMessage, error)
}

// AgentAddEnquiry records an enquiry sent with a key, as the form's would be but never
// held as spam by its trap: a key is no bot filling a form.
func (s TicketsAPI) AgentAddEnquiry(ctx context.Context, in api.AgentAddEnquiryRequestObject) (api.AgentAddEnquiryResponseObject, error) {
	q := request.ReqFrom(ctx)
	body, err := request.Recast[EnquiryIn](in.Body)
	if err != nil {
		return nil, err
	}
	body.Trap = ""
	e, err := s.Enquiries.AddEnquiry(q.Org.ID, q.Actor(), body)
	if err != nil {
		return nil, err
	}
	answer, err := request.Recast[api.AgentEnquiry](e)
	return api.AgentAddEnquiry200JSONResponse(answer), err
}
