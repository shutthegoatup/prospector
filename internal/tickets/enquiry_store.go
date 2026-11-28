package tickets

import (
	"errors"
	"net/mail"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/shutthegoatup/prospector/internal/crm"
	"github.com/shutthegoatup/prospector/internal/paging"
	"github.com/shutthegoatup/prospector/internal/prospecting"
	"github.com/shutthegoatup/prospector/internal/say"
	"github.com/shutthegoatup/prospector/internal/store"
)

// EnquiryStore is the enquiries area's queries: what people sent in through the website or a key, and the threads of replies.
type EnquiryStore struct {
	*store.Core
	// An enquiry can be from someone already here, or become someone new.
	CRM *prospecting.CRMStore
}

func (s *EnquiryStore) EnquiryByID(org, id int64) (*store.Enquiry, error) {
	var e store.Enquiry
	if err := s.InOrg(org).Where("id = ?", id).First(&e).Error; err != nil {
		return nil, store.Missing(err, "no enquiry %d", id)
	}
	return &e, nil
}

// SetEnquiry changes where answering an enquiry stands, who has it, or our note on it.
func (s *EnquiryStore) SetEnquiry(org, id int64, field, value, actor string) error {
	return s.SetEnquiryFields(org, id, []store.Change{{Field: field, Value: value}}, actor)
}

// SetEnquiryFields makes several of those changes or none.
func (s *EnquiryStore) SetEnquiryFields(org, id int64, changes []store.Change, actor string) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		for _, c := range changes {
			if err := s.setEnquiry(tx, org, id, c.Field, strings.TrimSpace(c.Value), actor); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *EnquiryStore) setEnquiry(tx *gorm.DB, org, id int64, field, value, actor string) error {
	{
		var e store.Enquiry
		if err := tx.Where("org_id = ? and id = ?", org, id).First(&e).Error; err != nil {
			return store.Missing(err, "no enquiry %d", id)
		}
		if e.Spam {
			return say.New("that was marked as spam, so there is nothing to answer")
		}
		logged := value
		switch field {
		case "state":
			if enquiryStateLabel(value) == "" {
				return say.Errorf("unknown state %q", value)
			}
		case "owner":
			if value != "" && value != e.Owner {
				var n int64
				if err := tx.Table("memberships").Joins("join users on users.id = memberships.user_id").
					Where("memberships.org_id = ? and users.name = ?", org, value).Count(&n).Error; err != nil {
					return err
				}
				if n == 0 {
					return say.Errorf("%q isn't a member of this organisation", value)
				}
			}
		case "note":
			value, logged = store.Clip(value, 2000), ""
		default:
			return say.Errorf("an enquiry has no %q to set", field)
		}
		if err := tx.Model(&e).Update(field, value).Error; err != nil {
			return err
		}
		// As when it arrived: history is a company's, so one matched to none has none.
		if e.CompanyID == nil {
			return nil
		}
		var contact int64
		if e.ContactID != nil {
			contact = *e.ContactID
		}
		return s.Log(tx, org, contact, e.CompanyID, actor, "enquiry:"+field, store.NonEmpty(logged))
	}
}

// AddEnquiry stores one enquiry. The same thing sent again within a few minutes is
// the same enquiry, so a form can retry until it hears back without making copies.
func (s *EnquiryStore) AddEnquiry(org int64, via string, in EnquiryIn) (*store.Enquiry, error) {
	e := store.Enquiry{OrgID: org, Kind: enquiryKind(in.Kind), Name: store.Clip(in.Name, 200), Email: strings.ToLower(store.Clip(in.Email, 320)),
		Company: store.Clip(in.Company, 200), Phone: store.Clip(in.Phone, 50), Message: store.Clip(in.Message, 10000),
		Site: store.Clip(in.Site, 200), Page: store.Clip(in.Page, 500), Via: via, Spam: strings.TrimSpace(in.Trap) != ""}
	if !e.Spam {
		e.State = EnquiryOpen
	}
	// One bare address and nothing round it: a name or a comment beside it would hide it
	// from the look-ups that erasure and matching to a contact make by address.
	if at, err := mail.ParseAddress(e.Email); err != nil || at.Address != e.Email || !store.EmailRe.MatchString(e.Email) || strings.ContainsAny(e.Email, " \t\r\n") {
		return nil, ErrEnquiryEmail
	}
	return &e, s.DB.Transaction(func(tx *gorm.DB) error {
		if err := store.HoldOrg(tx, org); err != nil {
			return err
		}
		var same store.Enquiry
		err := tx.Where("org_id = ? and email = ? and kind = ? and message = ? and created_at > ?",
			org, e.Email, e.Kind, e.Message, time.Now().Add(-enquiryRepeat)).First(&same).Error
		if err == nil {
			e = same
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var c store.Contact
		if tx.Where("org_id = ? and lower(email) = ? and key not like 'org:%'", org, e.Email).First(&c).Error == nil {
			e.ContactID, e.CompanyID = &c.ID, c.CompanyID
		}
		if _, domain, _ := strings.Cut(e.Email, "@"); e.CompanyID == nil {
			var co store.Company
			if tx.Where("org_id = ? and lower(domain) = ?", org, domain).First(&co).Error == nil {
				e.CompanyID = &co.ID
			}
		}
		if err := tx.Create(&e).Error; err != nil {
			return err
		}
		if e.Spam || e.CompanyID == nil {
			return nil
		}
		var contact int64
		if e.ContactID != nil {
			contact = *e.ContactID
		}
		return s.Log(tx, org, contact, e.CompanyID, via, "enquiry", new(kindLabel(e.Kind)))
	})
}

// DeleteEnquiry removes one for good: spam, or a person asking for what they sent to go.
// What it said goes with it; the history keeps only that one was removed.
func (s *EnquiryStore) DeleteEnquiry(org, id int64, actor string) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		var e store.Enquiry
		res := tx.Clauses(clause.Returning{}).Where("org_id = ? and id = ?", org, id).Delete(&e)
		if res.Error == nil && res.RowsAffected == 0 {
			return store.NoSuch("no enquiry %d", id)
		}
		if res.Error != nil {
			return res.Error
		}
		if err := tx.Where("org_id = ? and enquiry_id = ?", org, id).Delete(&store.EnquiryMessage{}).Error; err != nil {
			return err
		}
		return s.Log(tx, org, 0, e.CompanyID, actor, "enquiry:remove", nil)
	})
}

// Enquiries lists one page of the enquiries a filter matches, newest first.
func (s *EnquiryStore) Enquiries(org int64, f EnquiryFilter, page int) ([]store.Enquiry, prospecting.Page, error) {
	var total int64
	if err := s.enquiriesMatching(org, f).Count(&total).Error; err != nil {
		return nil, prospecting.Page{}, err
	}
	pg := prospecting.PageOf(page, EnquiryPageSize, int(total))
	var rows []store.Enquiry
	err := s.enquiriesMatching(org, f).Order("created_at desc, id desc").Limit(pg.Size).Offset(pg.Offset()).Find(&rows).Error
	return rows, pg, err
}

func (s *EnquiryStore) enquiriesMatching(org int64, f EnquiryFilter) *gorm.DB {
	q := s.InOrg(org).Model(&store.Enquiry{}).Where("spam = ?", f.Spam)
	if f.Kind != "" {
		q = q.Where("kind = ?", f.Kind)
	}
	if f.State != "" {
		q = q.Where("state = ?", f.State)
	}
	return q
}

// EnquiriesBefore is a page of the enquiries a filter matches, newest first, after
// the one a cursor names. The cursor is that enquiry's id, so one answered or marked
// as spam while the list is walked, or one arriving, moves no other between pages.
func (s *EnquiryStore) EnquiriesBefore(org int64, f EnquiryFilter, page paging.Page) (rows []store.Enquiry, next string, err error) {
	at, err := page.At(1)
	if err != nil {
		return nil, "", err
	}
	q := s.enquiriesMatching(org, f).Order("id desc").Limit(page.Size() + 1)
	if at[0] > 0 {
		q = q.Where("id < ?", at[0])
	}
	if err := q.Find(&rows).Error; err != nil {
		return nil, "", err
	}
	if len(rows) > page.Size() {
		rows = rows[:page.Size()]
		next = paging.Cursor(rows[len(rows)-1].ID)
	}
	return rows, next, nil
}

// EnquiryCounts is how many enquiries there are of each kind, with spam under "spam"
// and each state of answering under "state:" and its name.
func (s *EnquiryStore) EnquiryCounts(org int64) map[string]int {
	var rows []struct {
		Kind, State string
		Spam        bool
		N           int
	}
	_ = store.Logged(s.InOrg(org).Model(&store.Enquiry{}).Select("kind, state, spam, count(*) as n").Group("kind, state, spam").Scan(&rows))
	out := map[string]int{}
	for _, r := range rows {
		if r.Spam {
			out["spam"] += r.N
			continue
		}
		out[r.Kind] += r.N
		out[""] += r.N
		if r.State != "" {
			out["state:"+r.State] += r.N
		}
	}
	return out
}

func (s *EnquiryStore) AroundEnquiries(org int64, rows []store.Enquiry) (EnquiriesAround, error) {
	out := EnquiriesAround{Companies: map[int64]string{}, Talking: map[int64]bool{}}
	var ids, senders []int64
	for _, e := range rows {
		if e.CompanyID != nil {
			ids = append(ids, *e.CompanyID)
		}
		if e.ContactID != nil {
			senders = append(senders, *e.ContactID)
		}
	}
	if len(ids) > 0 {
		var companies []store.Company
		_ = store.Logged(s.InOrg(org).Where("id in ?", ids).Find(&companies))
		for _, c := range companies {
			out.Companies[c.ID] = c.Name
		}
	}
	if len(senders) > 0 {
		var on []int64
		if err := s.InOrg(org).Model(&store.Contact{}).Where("id in ?", senders).Where(store.InConversation).Pluck("id", &on).Error; err != nil {
			return out, err
		}
		for _, id := range on {
			out.Talking[id] = true
		}
	}
	var err error
	out.Threads, err = s.EnquiryThreads(org, EnquiryIDs(rows))
	return out, err
}

// ConverseEnquiry puts whoever sent an enquiry on the Conversations board: writing in is
// a reply to nothing we sent, but it is them talking to us, which is what the board is
// for. Someone not held yet is added first, under the never-contact and erasure rules
// that adding anyone by hand goes through.
func (s *EnquiryStore) ConverseEnquiry(o *store.Org, id int64, actor string) (*store.Contact, error) {
	var e store.Enquiry
	if err := s.InOrg(o.ID).Where("id = ?", id).First(&e).Error; err != nil {
		return nil, store.Missing(err, "no enquiry %d", id)
	}
	if e.Spam {
		return nil, say.New("that was marked as spam, so there is nobody to talk to")
	}
	var c store.Contact
	if e.ContactID != nil {
		if err := s.InOrg(o.ID).Where("id = ?", *e.ContactID).First(&c).Error; err != nil {
			return nil, store.Missing(err, "no contact %d", *e.ContactID)
		}
	} else {
		// Whoever holds that address now: they may have been added since this arrived,
		// by hand or from another of their enquiries.
		found := s.InOrg(o.ID).Where("lower(email) = ? and key not like 'org:%'", e.Email).Order("id").Limit(1).Find(&c)
		if found.Error != nil {
			return nil, found.Error
		}
		if found.RowsAffected == 0 {
			company, err := s.enquiryCompany(o.ID, e)
			if err != nil {
				return nil, err
			}
			added, err := s.CRM.QuickAddPerson(o, crm.QuickAdd{Name: senderName(e), Company: company, Email: e.Email}, actor)
			if err != nil {
				return nil, err
			}
			c = *added
		}
		// Matched now, so their export and erasure take this enquiry with them.
		if err := s.InOrg(o.ID).Model(&store.Enquiry{}).Where("id = ?", e.ID).
			Updates(map[string]any{"contact_id": c.ID, "company_id": c.CompanyID}).Error; err != nil {
			return nil, err
		}
	}
	// Someone put on the never-contact list since they were matched stays off the board.
	if err := s.CRM.Allowed(o, store.Deref(c.Email), store.Deref(c.Linkedin)); err != nil {
		return nil, err
	}
	if c.Declined != nil {
		return nil, say.New("they are marked as not interested, which keeps them off the board")
	}
	if c.Replied == nil {
		if err := s.CRM.Mark(o.ID, c.ID, "replied", store.Today(), actor); err != nil {
			return nil, err
		}
	}
	if store.Deref(c.CallNotes) == "" && e.Message != "" {
		// What they asked, on the card. It is their own text, shown as text like the rest.
		if err := s.CRM.Update(o.ID, c.ID, "call_notes", "Wrote in: "+store.Clip(e.Message, 500), actor); err != nil {
			return nil, err
		}
	}
	return &c, nil
}

// enquiryCompany is the company to put an enquiry's sender under. The one their
// address matched is used. The name they typed is used only when no company here has
// it: a form can claim to be from anyone, and that claim mustn't attach a stranger to
// a company already held. Otherwise they are added with none.
func (s *EnquiryStore) enquiryCompany(org int64, e store.Enquiry) (string, error) {
	if e.CompanyID != nil {
		var co store.Company
		if err := s.InOrg(org).Where("id = ?", *e.CompanyID).First(&co).Error; err != nil {
			return "", store.Missing(err, "no company %d", *e.CompanyID)
		}
		return co.Name, nil
	}
	typed := store.Clip(strings.Join(strings.Fields(e.Company), " "), 80)
	if typed == "" {
		return "", nil
	}
	var held int64
	if err := s.InOrg(org).Model(&store.Company{}).Where("lower(name) = ?", strings.ToLower(typed)).Count(&held).Error; err != nil {
		return "", err
	}
	if held > 0 {
		return "", nil
	}
	return typed, nil
}
