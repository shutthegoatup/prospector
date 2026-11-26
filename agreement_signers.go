package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"net/mail"
	"strings"
	"time"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"gorm.io/gorm"
)

// Preparing an agreement for signing: who signs it and in what order, who gets a copy,
// and where on its pages each signer's fields go. All of it changes only while the
// agreement is a draft; sending it freezes the lot.

func init() {
	// pdfcpu otherwise writes a configuration directory under the user's home.
	model.ConfigPath = "disable"
}

const (
	RecipientSigner = "signer"
	RecipientCopy   = "copy"

	// agreementRecipientLimit and agreementFieldLimit bound what one draft holds.
	agreementRecipientLimit = 20
	agreementFieldLimit     = 200
)

// AgreementRecipient is someone an agreement goes to: a signer, in Position order when
// the agreement is signed in order, or someone who gets a copy once it is signed. They
// are a person at the company or a member of the organisation, never a bare address.
type AgreementRecipient struct {
	ID          int64     `gorm:"primaryKey" json:"id"`
	OrgID       int64     `gorm:"index:idx_agreement_recipients,priority:1;not null" json:"-"`
	AgreementID int64     `gorm:"index:idx_agreement_recipients,priority:2;not null" json:"agreement_id"`
	Position    int       `gorm:"not null;default:0" json:"position"`
	Role        string    `gorm:"not null;default:'signer'" json:"role"`
	ContactID   *int64    `gorm:"index" json:"contact_id"`
	UserID      *int64    `gorm:"index" json:"member_id"`
	CreatedBy   string    `gorm:"not null;default:''" json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
}

// AgreementField is a place on a page that one signer fills in. X, Y, W and H are
// fractions of the page as it is shown, rotation applied, from its top left, so they
// don't depend on the units or size the PDF uses.
type AgreementField struct {
	ID          int64   `gorm:"primaryKey" json:"id"`
	OrgID       int64   `gorm:"index:idx_agreement_fields,priority:1;not null" json:"-"`
	AgreementID int64   `gorm:"index:idx_agreement_fields,priority:2;not null" json:"agreement_id"`
	Round       int     `gorm:"not null;default:1" json:"round"`
	RecipientID int64   `gorm:"index;not null" json:"recipient_id"`
	Page        int     `gorm:"not null;default:1" json:"page"`
	Kind        string  `gorm:"not null;default:'signature'" json:"kind"`
	X           float64 `gorm:"not null;default:0" json:"x"`
	Y           float64 `gorm:"not null;default:0" json:"y"`
	W           float64 `gorm:"not null;default:0" json:"w"`
	H           float64 `gorm:"not null;default:0" json:"h"`
}

// fieldKinds are what a signer can be asked to fill in. Signature is the one every
// signer needs before an agreement can go.
var fieldKinds = []string{"signature", "initials", "name", "date", "text", "tick"}

// Party is an agreement recipient with who they are now: a contact's or member's current name
// and address, which sending will freeze.
type Party struct {
	AgreementRecipient
	Name  string `json:"name"`
	Email string `json:"email"`
}

// pdfPages is how many pages a document has, refusing one Prospector couldn't later fill
// in and seal: encrypted, already signed, or one it can't read at all. It is read, not
// validated: real PDFs often break a rule of their own version that doesn't matter here.
func pdfPages(data []byte) (int, error) {
	ctx, err := api.ReadContext(context.Background(), bytes.NewReader(data), model.NewDefaultConfiguration())
	if err != nil {
		if errors.Is(err, pdfcpu.ErrWrongPassword) || strings.Contains(err.Error(), "password") {
			return 0, errors.New("that PDF is password-protected: save it without a password first")
		}
		return 0, fmt.Errorf("that PDF can't be read to place fields on it: %v", err)
	}
	if ctx.XRefTable.Encrypt != nil || ctx.E != nil {
		return 0, errors.New("that PDF is encrypted: save it without protection first")
	}
	if signedPDF(ctx.XRefTable, data) {
		return 0, errors.New("that PDF is already signed: upload the unsigned version")
	}
	if err := ctx.EnsurePageCount(); err != nil {
		return 0, fmt.Errorf("that PDF can't be read to place fields on it: %v", err)
	}
	if ctx.PageCount < 1 {
		return 0, errors.New("that PDF has no pages")
	}
	return ctx.PageCount, nil
}

// signedPDF is whether a document carries a signature: its form says so, or a signature's
// byte range is in it, which sealing it again would break.
func signedPDF(x *model.XRefTable, data []byte) bool {
	if bytes.Contains(data, []byte("/ByteRange")) {
		return true
	}
	root, err := x.Catalog()
	if err != nil {
		return false
	}
	form, err := x.DereferenceDict(root["AcroForm"])
	if err != nil || form == nil {
		return false
	}
	flags := form.IntEntry("SigFlags")
	return flags != nil && *flags&1 != 0
}

// agreementPages is a draft's page count, worked out from its document for one uploaded
// before pages were counted.
func (s *Store) agreementPages(org int64, a *Agreement) (int, error) {
	if a.PageCount > 0 || a.FileHash == "" {
		return a.PageCount, nil
	}
	_, data, err := s.AgreementDocument(org, a.ID)
	if err != nil {
		return 0, err
	}
	n, err := pdfPages(data)
	if err != nil {
		return 0, err
	}
	a.PageCount = n
	return n, s.in(org).Model(&Agreement{}).Where("id = ? and file_hash = ?", a.ID, a.FileHash).Update("page_count", n).Error
}

func (s *Store) draft(tx *gorm.DB, org, id int64) (*Agreement, error) {
	var a Agreement
	if err := tx.Where("org_id = ? and id = ?", org, id).First(&a).Error; err != nil {
		return nil, missing(err, "no agreement %d", id)
	}
	if a.Status != AgreementDraft {
		return nil, errNotDraft
	}
	return &a, nil
}

// AgreementRecipients are an agreement's signers in their order, then those who get a copy.
func (s *Store) AgreementRecipients(org, id int64) ([]Party, error) {
	if _, err := s.Agreement(org, id); err != nil {
		return nil, err
	}
	out := []Party{}
	err := s.db.Table("agreement_recipients").
		Select("agreement_recipients.*, coalesce(users.name, contacts.person, '') as name, coalesce(users.email, contacts.email, '') as email").
		Joins("left join contacts on contacts.id = agreement_recipients.contact_id and contacts.org_id = agreement_recipients.org_id").
		Joins("left join users on users.id = agreement_recipients.user_id").
		Where("agreement_recipients.org_id = ? and agreement_recipients.agreement_id = ?", org, id).
		Order("case when agreement_recipients.role = 'signer' then 0 else 1 end, agreement_recipients.position, agreement_recipients.id").
		Scan(&out).Error
	return out, err
}

// AddRecipient puts a contact or a member on a draft, last in its order. A contact has
// to be a person at the agreement's company with one address, and not on the
// never-contact list; a member has to be in the organisation.
func (s *Store) AddRecipient(o *Org, id int64, role string, contact, member *int64, actor string) (*AgreementRecipient, error) {
	if role != RecipientSigner && role != RecipientCopy {
		return nil, errors.New(`a recipient is a "signer" or gets a "copy"`)
	}
	if (contact == nil) == (member == nil) {
		return nil, errors.New("name one contact or one member")
	}
	var r AgreementRecipient
	return &r, s.db.Transaction(func(tx *gorm.DB) error {
		a, err := s.draft(tx, o.ID, id)
		if err != nil {
			return err
		}
		var on []AgreementRecipient
		if err := tx.Where("org_id = ? and agreement_id = ?", o.ID, id).Find(&on).Error; err != nil {
			return err
		}
		if len(on) >= agreementRecipientLimit {
			return fmt.Errorf("an agreement goes to %d people at most", agreementRecipientLimit)
		}
		if contact != nil {
			var c Contact
			if err := tx.Where("org_id = ? and id = ?", o.ID, *contact).First(&c).Error; err != nil {
				return missing(err, "no contact %d", *contact)
			}
			if err := signable(o, a.CompanyID, c); err != nil {
				return err
			}
		} else if err := tx.Where("org_id = ? and user_id = ?", o.ID, *member).First(&Membership{}).Error; err != nil {
			return missing(err, "no member %d in this organisation", *member)
		}
		last := 0
		for _, x := range on {
			if (contact != nil && x.ContactID != nil && *x.ContactID == *contact) || (member != nil && x.UserID != nil && *x.UserID == *member) {
				return errors.New("they are on this agreement already")
			}
			last = max(last, x.Position)
		}
		r = AgreementRecipient{OrgID: o.ID, AgreementID: id, Position: last + 1, Role: role, ContactID: contact, UserID: member, CreatedBy: actor}
		return tx.Create(&r).Error
	})
}

// signable is whether a contact may be sent an agreement for a company: a person, picked,
// at that company, with one address, and not on the never-contact list.
func signable(o *Org, company int64, c Contact) error {
	switch {
	case strings.HasPrefix(c.Key, "org:"):
		return errors.New("that row stands for a company, not a person")
	case c.CompanyID == nil || *c.CompanyID != company:
		return errors.New("that person is not at the agreement's company")
	case c.Status == ContactSuggested:
		return errors.New("this contact is still a suggestion in Research; a person picks them first")
	}
	if _, err := mail.ParseAddress(strings.TrimSpace(deref(c.Email))); err != nil {
		return errors.New("they have no single email address to send the agreement to")
	}
	return o.Settings.listed(deref(c.Email), deref(c.Linkedin))
}

// ChangeRecipient moves a recipient to a place in the order, or changes what they get.
// Someone made a copy recipient loses the fields they had.
func (s *Store) ChangeRecipient(org, id, recipient int64, role *string, position *int) (*AgreementRecipient, error) {
	var r AgreementRecipient
	return &r, s.db.Transaction(func(tx *gorm.DB) error {
		if _, err := s.draft(tx, org, id); err != nil {
			return err
		}
		if err := tx.Where("org_id = ? and agreement_id = ? and id = ?", org, id, recipient).First(&r).Error; err != nil {
			return missing(err, "no recipient %d on agreement %d", recipient, id)
		}
		if role != nil {
			if *role != RecipientSigner && *role != RecipientCopy {
				return errors.New(`a recipient is a "signer" or gets a "copy"`)
			}
			if *role == RecipientCopy {
				if err := tx.Where("org_id = ? and recipient_id = ?", org, r.ID).Delete(&AgreementField{}).Error; err != nil {
					return err
				}
			}
			r.Role = *role
		}
		if position != nil {
			var all []AgreementRecipient
			if err := tx.Where("org_id = ? and agreement_id = ?", org, id).Order("position, id").Find(&all).Error; err != nil {
				return err
			}
			order := make([]AgreementRecipient, 0, len(all))
			for _, x := range all {
				if x.ID != r.ID {
					order = append(order, x)
				}
			}
			at := min(max(*position, 1), len(all)) - 1
			order = append(order[:at], append([]AgreementRecipient{r}, order[at:]...)...)
			for i, x := range order {
				if err := tx.Model(&AgreementRecipient{}).Where("id = ?", x.ID).Update("position", i+1).Error; err != nil {
					return err
				}
				if x.ID == r.ID {
					r.Position = i + 1
				}
			}
		}
		return tx.Model(&AgreementRecipient{}).Where("id = ?", r.ID).Update("role", r.Role).Error
	})
}

// RemoveRecipient takes someone off a draft, with the fields they had.
func (s *Store) RemoveRecipient(org, id, recipient int64) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		if _, err := s.draft(tx, org, id); err != nil {
			return err
		}
		res := tx.Where("org_id = ? and agreement_id = ? and id = ?", org, id, recipient).Delete(&AgreementRecipient{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return noSuch("no recipient %d on agreement %d", recipient, id)
		}
		return tx.Where("org_id = ? and recipient_id = ?", org, recipient).Delete(&AgreementField{}).Error
	})
}

// dropRecipients takes the recipients a query matches off their drafts, with their
// fields, when the person or member they stand for goes.
func dropRecipients(tx *gorm.DB, matching *gorm.DB) error {
	var ids []int64
	if err := matching.Model(&AgreementRecipient{}).Pluck("id", &ids).Error; err != nil || len(ids) == 0 {
		return err
	}
	if err := tx.Where("recipient_id in ?", ids).Delete(&AgreementField{}).Error; err != nil {
		return err
	}
	return tx.Where("id in ?", ids).Delete(&AgreementRecipient{}).Error
}

// AgreementFields are the places on a draft's pages, for the round it is on.
func (s *Store) AgreementFields(org, id int64) ([]AgreementField, error) {
	a, err := s.Agreement(org, id)
	if err != nil {
		return nil, err
	}
	out := []AgreementField{}
	return out, s.in(org).Where("agreement_id = ? and round = ?", id, a.Round).Order("page, y, x, id").Find(&out).Error
}

// PutAgreementFields replaces every field on a draft: the editor sends them all at once.
// Each belongs to one of its signers and lies wholly on one of its pages.
func (s *Store) PutAgreementFields(org, id int64, fields []AgreementField) ([]AgreementField, error) {
	if len(fields) > agreementFieldLimit {
		return nil, fmt.Errorf("an agreement has %d fields at most", agreementFieldLimit)
	}
	a, err := s.Agreement(org, id)
	if err != nil {
		return nil, err
	}
	if a.Status != AgreementDraft {
		return nil, errNotDraft
	}
	if a.FileHash == "" {
		return nil, errors.New("upload the PDF before placing fields on it")
	}
	pages, err := s.agreementPages(org, a)
	if err != nil {
		return nil, err
	}
	out := []AgreementField{}
	err = s.db.Transaction(func(tx *gorm.DB) error {
		a, err := s.draft(tx, org, id)
		if err != nil {
			return err
		}
		var signers []AgreementRecipient
		if err := tx.Where("org_id = ? and agreement_id = ? and role = ?", org, id, RecipientSigner).Find(&signers).Error; err != nil {
			return err
		}
		ok := map[int64]bool{}
		for _, r := range signers {
			ok[r.ID] = true
		}
		for i, f := range fields {
			if err := fieldFits(f, pages, ok); err != nil {
				return fmt.Errorf("field %d: %w", i+1, err)
			}
			out = append(out, AgreementField{OrgID: org, AgreementID: id, Round: a.Round, RecipientID: f.RecipientID, Page: f.Page, Kind: f.Kind, X: f.X, Y: f.Y, W: f.W, H: f.H})
		}
		if err := tx.Where("org_id = ? and agreement_id = ? and round = ?", org, id, a.Round).Delete(&AgreementField{}).Error; err != nil {
			return err
		}
		if len(out) == 0 {
			return nil
		}
		return tx.Create(&out).Error
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// fieldFits is whether a field is one a signer can fill in: a kind there is, for one of
// the agreement's signers, big enough to use and wholly on a page it has.
func fieldFits(f AgreementField, pages int, signers map[int64]bool) error {
	const least = 0.01
	switch {
	case !signers[f.RecipientID]:
		return fmt.Errorf("recipient %d isn't one of this agreement's signers", f.RecipientID)
	case !contains(fieldKinds, f.Kind):
		return fmt.Errorf("a field is one of %s", strings.Join(fieldKinds, ", "))
	case f.Page < 1 || f.Page > pages:
		return fmt.Errorf("the document has pages 1 to %d", pages)
	}
	for _, v := range []float64{f.X, f.Y, f.W, f.H} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return errors.New("a field's place is four numbers between 0 and 1")
		}
	}
	if f.W < least || f.H < least || f.X < 0 || f.Y < 0 || f.X+f.W > 1+1e-9 || f.Y+f.H > 1+1e-9 {
		return errors.New("a field has to lie wholly on its page")
	}
	return nil
}

// Readiness is what a draft still lacks before it could be sent.
func (s *Store) Readiness(org int64, a Agreement) ([]string, error) {
	var missingParts []string
	if a.FileHash == "" {
		missingParts = append(missingParts, "Upload the PDF.")
	}
	rs, err := s.AgreementRecipients(org, a.ID)
	if err != nil {
		return nil, err
	}
	fs, err := s.AgreementFields(org, a.ID)
	if err != nil {
		return nil, err
	}
	signs := map[int64]bool{}
	for _, f := range fs {
		if f.Kind == "signature" {
			signs[f.RecipientID] = true
		}
	}
	signers := 0
	for _, r := range rs {
		if r.Role != RecipientSigner {
			continue
		}
		signers++
		if !signs[r.ID] {
			missingParts = append(missingParts, fmt.Sprintf("Place a signature for %s.", orElse(r.Name, r.Email)))
		}
	}
	if signers == 0 {
		missingParts = append(missingParts, "Add someone to sign it.")
	}
	return missingParts, nil
}

func orElse(s ...string) string {
	for _, v := range s {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
