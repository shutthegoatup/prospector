package signing

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/shutthegoatup/prospector/internal/identity"
	"github.com/shutthegoatup/prospector/internal/prospecting"
	"github.com/shutthegoatup/prospector/internal/say"
	"github.com/shutthegoatup/prospector/internal/store"
)

// AgreementStore is the agreements area's queries: contracts as PDFs, who signs them, where, and the seal.
type AgreementStore struct {
	*store.Core
	// An agreement is with a company, signed by its people or the organisation's members.
	CRM      *prospecting.CRMStore
	Identity *identity.IdentityStore
}

func (s *AgreementStore) AddAgreement(org, company int64, name, actor string) (*store.Agreement, error) {
	name, err := agreementName(name)
	if err != nil {
		return nil, err
	}
	if err := s.InOrg(org).Where("id = ?", company).First(&store.Company{}).Error; err != nil {
		return nil, store.Missing(err, "no company %d", company)
	}
	a := store.Agreement{OrgID: org, CompanyID: company, Name: name, Status: identity.AgreementDraft, Round: 1, InOrder: true, CreatedBy: actor, UpdatedBy: actor}
	return &a, s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&a).Error; err != nil {
			return err
		}
		return s.Log(tx, org, 0, &company, actor, store.ActAgreementAdd, &a.Name)
	})
}

func (s *AgreementStore) Agreement(org, id int64) (*store.Agreement, error) {
	var a store.Agreement
	if err := s.InOrg(org).Where("id = ?", id).First(&a).Error; err != nil {
		return nil, store.Missing(err, "no agreement %d", id)
	}
	return &a, nil
}

// Agreements are an organisation's, or one company's when company isn't 0, newest first.
func (s *AgreementStore) Agreements(org, company int64) ([]store.Agreement, error) {
	q := s.InOrg(org)
	if company != 0 {
		q = q.Where("company_id = ?", company)
	}
	out := []store.Agreement{}
	return out, q.Order("id desc").Find(&out).Error
}

// ChangeAgreement renames a draft, or says whether its signers sign in order.
func (s *AgreementStore) ChangeAgreement(org, id int64, name *string, inOrder *bool, actor string) (*store.Agreement, error) {
	change := map[string]any{"updated_by": actor}
	if name != nil {
		n, err := agreementName(*name)
		if err != nil {
			return nil, err
		}
		change["name"] = n
	}
	if inOrder != nil {
		change["in_order"] = *inOrder
	}
	res := s.InOrg(org).Model(&store.Agreement{}).Where("id = ? and status = ?", id, identity.AgreementDraft).Updates(change)
	if res.Error != nil {
		return nil, res.Error
	}
	a, err := s.Agreement(org, id)
	if err != nil {
		return nil, err
	}
	if res.RowsAffected == 0 {
		return nil, errNotDraft
	}
	return a, nil
}

func (s *AgreementStore) RemoveAgreement(org, id int64, actor string) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		var a store.Agreement
		res := tx.Clauses(clause.Returning{}).Where("org_id = ? and id = ? and status = ?", org, id, identity.AgreementDraft).Delete(&a)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			if err := tx.Where("org_id = ? and id = ?", org, id).First(&store.Agreement{}).Error; err != nil {
				return store.Missing(err, "no agreement %d", id)
			}
			return say.New("only a draft agreement can be removed; one that was sent is kept")
		}
		if err := s.Log(tx, org, 0, &a.CompanyID, actor, store.ActAgreementRemove, &a.Name); err != nil {
			return err
		}
		for _, part := range []any{&store.AgreementFile{}, &store.AgreementRecipient{}, &store.AgreementField{}, &store.AgreementEvent{}} {
			if err := tx.Where("org_id = ? and agreement_id = ?", org, id).Delete(part).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// PutAgreementFile keeps a draft's document, replacing any before it. It has to be a
// PDF Prospector can later fill in and seal, so what is signed is what was seen, and it
// is hashed as it arrives. Fields on pages the new one doesn't have go.
func (s *AgreementStore) PutAgreementFile(org, id int64, name string, data []byte, actor string) (*store.Agreement, error) {
	if len(data) > AgreementFileLimit {
		return nil, say.Errorf("a document is %d MB at most", AgreementFileLimit>>20)
	}
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		return nil, say.New("that isn't a PDF: save the agreement as a PDF first")
	}
	pages, err := pdfPages(data)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	var a store.Agreement
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&store.Agreement{}).Where("org_id = ? and id = ? and status = ?", org, id, identity.AgreementDraft).
			Updates(map[string]any{"file_name": FileName(name), "file_size": int64(len(data)), "file_hash": hex.EncodeToString(sum[:]), "page_count": pages, "updated_by": actor})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			if err := tx.Where("org_id = ? and id = ?", org, id).First(&a).Error; err != nil {
				return store.Missing(err, "no agreement %d", id)
			}
			return errNotDraft
		}
		if err := tx.Where("org_id = ? and agreement_id = ?", org, id).Delete(&store.AgreementFile{}).Error; err != nil {
			return err
		}
		if err := tx.Create(&store.AgreementFile{AgreementID: id, OrgID: org, Data: data}).Error; err != nil {
			return err
		}
		if err := tx.Where("org_id = ? and agreement_id = ? and page > ?", org, id, pages).Delete(&store.AgreementField{}).Error; err != nil {
			return err
		}
		return tx.Where("org_id = ? and id = ?", org, id).First(&a).Error
	})
	return &a, err
}

// AgreementFile is a document's bytes, checked against the hash it was kept with.
func (s *AgreementStore) AgreementDocument(org, id int64) (*store.Agreement, []byte, error) {
	if _, err := s.Agreement(org, id); err != nil {
		return nil, nil, err
	}
	// The record and its file in one statement, so a replacement committing between two
	// reads can't pass for tampering.
	var both struct {
		store.Agreement
		Data []byte
	}
	read := s.DB.Model(&store.Agreement{}).Select("agreements.*, agreement_files.data as data").
		Joins("join agreement_files on agreement_files.agreement_id = agreements.id and agreement_files.org_id = agreements.org_id").
		Where("agreements.org_id = ? and agreements.id = ?", org, id).Limit(1).Scan(&both)
	if read.Error != nil {
		return nil, nil, read.Error
	}
	if read.RowsAffected == 0 {
		return nil, nil, store.NoSuch("agreement %d has no document yet", id)
	}
	a, f := &both.Agreement, store.AgreementFile{Data: both.Data}
	if sum := sha256.Sum256(f.Data); hex.EncodeToString(sum[:]) != a.FileHash {
		return nil, nil, fmt.Errorf("agreement %d: %w", id, ErrTampered)
	}
	return a, f.Data, nil
}

// agreementPages is a draft's page count, worked out from its document for one uploaded
// before pages were counted.
func (s *AgreementStore) agreementPages(org int64, a *store.Agreement) (int, error) {
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
	return n, s.InOrg(org).Model(&store.Agreement{}).Where("id = ? and file_hash = ?", a.ID, a.FileHash).Update("page_count", n).Error
}

func (s *AgreementStore) draft(tx *gorm.DB, org, id int64) (*store.Agreement, error) {
	var a store.Agreement
	if err := tx.Where("org_id = ? and id = ?", org, id).First(&a).Error; err != nil {
		return nil, store.Missing(err, "no agreement %d", id)
	}
	if a.Status != identity.AgreementDraft {
		return nil, errNotDraft
	}
	return &a, nil
}

// AgreementRecipients are an agreement's signers in their order, then those who get a copy.
func (s *AgreementStore) AgreementRecipients(org, id int64) ([]Party, error) {
	if _, err := s.Agreement(org, id); err != nil {
		return nil, err
	}
	out := []Party{}
	err := s.DB.Table("agreement_recipients").
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
func (s *AgreementStore) AddRecipient(o *store.Org, id int64, role string, contact, member *int64, actor string) (*store.AgreementRecipient, error) {
	if role != RecipientSigner && role != RecipientCopy {
		return nil, say.New(`a recipient is a "signer" or gets a "copy"`)
	}
	if (contact == nil) == (member == nil) {
		return nil, say.New("name one contact or one member")
	}
	var r store.AgreementRecipient
	return &r, s.DB.Transaction(func(tx *gorm.DB) error {
		a, err := s.draft(tx, o.ID, id)
		if err != nil {
			return err
		}
		var on []store.AgreementRecipient
		if err := tx.Where("org_id = ? and agreement_id = ?", o.ID, id).Find(&on).Error; err != nil {
			return err
		}
		if len(on) >= agreementRecipientLimit {
			return say.Errorf("an agreement goes to %d people at most", agreementRecipientLimit)
		}
		if contact != nil {
			var c store.Contact
			if err := tx.Where("org_id = ? and id = ?", o.ID, *contact).First(&c).Error; err != nil {
				return store.Missing(err, "no contact %d", *contact)
			}
			if err := signable(o, a.CompanyID, c); err != nil {
				return err
			}
		} else if err := tx.Where("org_id = ? and user_id = ?", o.ID, *member).First(&store.Membership{}).Error; err != nil {
			return store.Missing(err, "no member %d in this organisation", *member)
		}
		last := 0
		for _, x := range on {
			if (contact != nil && x.ContactID != nil && *x.ContactID == *contact) || (member != nil && x.UserID != nil && *x.UserID == *member) {
				return say.New("they are on this agreement already")
			}
			last = max(last, x.Position)
		}
		r = store.AgreementRecipient{OrgID: o.ID, AgreementID: id, Position: last + 1, Role: role, ContactID: contact, UserID: member, CreatedBy: actor}
		return tx.Create(&r).Error
	})
}

// ChangeRecipient moves a recipient to a place in the order, or changes what they get.
// Someone made a copy recipient loses the fields they had.
func (s *AgreementStore) ChangeRecipient(org, id, recipient int64, role *string, position *int) (*store.AgreementRecipient, error) {
	var r store.AgreementRecipient
	return &r, s.DB.Transaction(func(tx *gorm.DB) error {
		if _, err := s.draft(tx, org, id); err != nil {
			return err
		}
		if err := tx.Where("org_id = ? and agreement_id = ? and id = ?", org, id, recipient).First(&r).Error; err != nil {
			return store.Missing(err, "no recipient %d on agreement %d", recipient, id)
		}
		if role != nil {
			if *role != RecipientSigner && *role != RecipientCopy {
				return say.New(`a recipient is a "signer" or gets a "copy"`)
			}
			if *role == RecipientCopy {
				if err := tx.Where("org_id = ? and recipient_id = ?", org, r.ID).Delete(&store.AgreementField{}).Error; err != nil {
					return err
				}
			}
			r.Role = *role
		}
		if position != nil {
			var all []store.AgreementRecipient
			if err := tx.Where("org_id = ? and agreement_id = ?", org, id).Order("position, id").Find(&all).Error; err != nil {
				return err
			}
			order := make([]store.AgreementRecipient, 0, len(all))
			for _, x := range all {
				if x.ID != r.ID {
					order = append(order, x)
				}
			}
			at := min(max(*position, 1), len(all)) - 1
			order = append(order[:at], append([]store.AgreementRecipient{r}, order[at:]...)...)
			for i, x := range order {
				if err := tx.Model(&store.AgreementRecipient{}).Where("id = ?", x.ID).Update("position", i+1).Error; err != nil {
					return err
				}
				if x.ID == r.ID {
					r.Position = i + 1
				}
			}
		}
		return tx.Model(&store.AgreementRecipient{}).Where("id = ?", r.ID).Update("role", r.Role).Error
	})
}

// RemoveRecipient takes someone off a draft, with the fields they had.
func (s *AgreementStore) RemoveRecipient(org, id, recipient int64) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		if _, err := s.draft(tx, org, id); err != nil {
			return err
		}
		res := tx.Where("org_id = ? and agreement_id = ? and id = ?", org, id, recipient).Delete(&store.AgreementRecipient{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return store.NoSuch("no recipient %d on agreement %d", recipient, id)
		}
		return tx.Where("org_id = ? and recipient_id = ?", org, recipient).Delete(&store.AgreementField{}).Error
	})
}

// AgreementFields are the places on a draft's pages, for the round it is on.
func (s *AgreementStore) AgreementFields(org, id int64) ([]store.AgreementField, error) {
	a, err := s.Agreement(org, id)
	if err != nil {
		return nil, err
	}
	out := []store.AgreementField{}
	return out, s.InOrg(org).Where("agreement_id = ? and round = ?", id, a.Round).Order("page, y, x, id").Find(&out).Error
}

// PutAgreementFields replaces every field on a draft: the editor sends them all at once.
// Each belongs to one of its signers and lies wholly on one of its pages.
func (s *AgreementStore) PutAgreementFields(org, id int64, fields []store.AgreementField) ([]store.AgreementField, error) {
	if len(fields) > agreementFieldLimit {
		return nil, say.Errorf("an agreement has %d fields at most", agreementFieldLimit)
	}
	a, err := s.Agreement(org, id)
	if err != nil {
		return nil, err
	}
	if a.Status != identity.AgreementDraft {
		return nil, errNotDraft
	}
	if a.FileHash == "" {
		return nil, say.New("upload the PDF before placing fields on it")
	}
	pages, err := s.agreementPages(org, a)
	if err != nil {
		return nil, err
	}
	out := []store.AgreementField{}
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		a, err := s.draft(tx, org, id)
		if err != nil {
			return err
		}
		var signers []store.AgreementRecipient
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
			out = append(out, store.AgreementField{OrgID: org, AgreementID: id, Round: a.Round, RecipientID: f.RecipientID, Page: f.Page, Kind: f.Kind, X: f.X, Y: f.Y, W: f.W, H: f.H})
		}
		if err := tx.Where("org_id = ? and agreement_id = ? and round = ?", org, id, a.Round).Delete(&store.AgreementField{}).Error; err != nil {
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

// Readiness is what a draft still lacks before it could be sent.
func (s *AgreementStore) Readiness(org int64, a store.Agreement) ([]string, error) {
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

// AgreementEvents are an agreement's events in the order they happened, checked against
// their hashes.
func (s *AgreementStore) AgreementEvents(org, id int64) ([]store.AgreementEvent, error) {
	out := []store.AgreementEvent{}
	if err := s.InOrg(org).Where("agreement_id = ?", id).Order("id").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, CheckChain(out)
}

// Preparing is everything a draft's Prepare page shows. People are only those at the
// company who could be sent it; whoever is on it already is left in, for the page to tell.
func (s *AgreementStore) Preparing(o *store.Org, id int64) (*PreparePage, error) {
	ag, err := s.Agreement(o.ID, id)
	if err != nil {
		return nil, err
	}
	c, err := s.CRM.CompanyByID(o.ID, ag.CompanyID)
	if err != nil {
		return nil, store.Missing(err, "no company %d", ag.CompanyID)
	}
	p := PreparePage{Agreement: *ag, Company: *c}
	if _, err := s.agreementPages(o.ID, &p.Agreement); err != nil {
		p.Problem = err.Error()
	}
	var failed [4]error
	p.Parties, failed[0] = s.AgreementRecipients(o.ID, ag.ID)
	var people []store.Contact
	people, failed[1] = s.CRM.People(o.ID, c.ID)
	p.Members, failed[2] = s.Identity.Members(o.ID)
	p.Missing, failed[3] = s.Readiness(o.ID, p.Agreement)
	if err := errors.Join(failed[:]...); err != nil {
		return nil, fmt.Errorf("agreement %d: %w", ag.ID, err)
	}
	for _, person := range people {
		if signable(o, c.ID, person) == nil {
			p.People = append(p.People, person)
		}
	}
	return &p, nil
}

// SealKey is the organisation's sealing key and certificate, made on first use.
func (s *AgreementStore) SealKey(o *store.Org) (*ecdsa.PrivateKey, *x509.Certificate, error) {
	var k store.SealKey
	err := s.DB.Where("org_id = ?", o.ID).First(&k).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if k, err = s.makeSealKey(o); err != nil {
			return nil, nil, err
		}
	} else if err != nil {
		return nil, nil, err
	}
	plain, err := s.Keyring.Open(k.Key)
	if err != nil {
		return nil, nil, fmt.Errorf("the organisation's sealing key: %w", err)
	}
	key, err := x509.ParseECPrivateKey([]byte(plain))
	if err != nil {
		return nil, nil, fmt.Errorf("the organisation's sealing key: %w", err)
	}
	cert, err := x509.ParseCertificate(k.Cert)
	if err != nil {
		return nil, nil, fmt.Errorf("the organisation's sealing certificate: %w", err)
	}
	return key, cert, nil
}

// makeSealKey makes an organisation's key and its self-signed certificate. Two made at
// once keep whichever was stored first.
func (s *AgreementStore) makeSealKey(o *store.Org) (store.SealKey, error) {
	if s.Keyring == nil {
		return store.SealKey{}, fmt.Errorf("sealing agreements needs a secret key to keep its own under: %w", store.ErrNoKeys)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return store.SealKey{}, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return store.SealKey{}, err
	}
	now := time.Now().UTC()
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: o.Name + " (Prospector)", Organization: []string{o.Name}},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.AddDate(20, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageContentCommitment,
		UnknownExtKeyUsage:    []asn1.ObjectIdentifier{DocumentSigning},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return store.SealKey{}, err
	}
	raw, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return store.SealKey{}, err
	}
	sealedKey, err := s.Keyring.Seal(string(raw))
	if err != nil {
		return store.SealKey{}, err
	}
	k := store.SealKey{OrgID: o.ID, Cert: der, Key: sealedKey, CreatedAt: now}
	if err := s.DB.Clauses(clause.OnConflict{DoNothing: true}).Create(&k).Error; err != nil {
		return store.SealKey{}, err
	}
	return k, s.DB.Where("org_id = ?", o.ID).First(&k).Error
}

// SealDocument fills an agreement's document in, adds its certificate and seals the whole with
// the organisation's key, then checks the seal holds before handing it back.
func (s *AgreementStore) SealDocument(o *store.Org, doc []byte, stamps []Stamp, c Certificate) (*Sealed, error) {
	// Text the fonts can't draw would seal as empty space, so it stops the seal instead.
	for _, text := range c.texts(stamps) {
		if err := drawable(text); err != nil {
			return nil, err
		}
	}
	key, cert, err := s.SealKey(o)
	if err != nil {
		return nil, err
	}
	rc, err := ReadForSealing(doc)
	if err != nil {
		return nil, err
	}
	if err := FillIn(rc, stamps); err != nil {
		return nil, err
	}
	page, err := drawCertificate(c)
	if err != nil {
		return nil, err
	}
	certificate, err := api.ReadAndValidate(context.Background(), bytes.NewReader(page), model.NewDefaultConfiguration())
	if err != nil {
		return nil, fmt.Errorf("reading the certificate back: %w", err)
	}
	rc.EnsureVersionForWriting()
	if err := pdfcpu.MergeXRefTables(context.Background(), "certificate", certificate, rc, false, false); err != nil {
		return nil, fmt.Errorf("adding the certificate: %w", err)
	}
	var whole bytes.Buffer
	if err := api.WriteContext(context.Background(), rc, &whole); err != nil {
		return nil, err
	}
	out, err := sealPDF(whole.Bytes(), key, cert, o.Name)
	if err != nil {
		return nil, err
	}
	if err := CheckSeal(out, cert); err != nil {
		return nil, err
	}
	sum := sha256.Sum256(out)
	return &Sealed{PDF: out, Hash: hex.EncodeToString(sum[:])}, nil
}

// TrySeal goes through sealing a draft as it is set out, with stand-in values in every
// field and a stand-in certificate, and keeps nothing. Sending runs it first, so a
// document that can't be filled in and sealed is found before anyone signs it.
func (s *AgreementStore) TrySeal(o *store.Org, id int64) error {
	a, doc, err := s.AgreementDocument(o.ID, id)
	if err != nil {
		return err
	}
	fields, err := s.AgreementFields(o.ID, id)
	if err != nil {
		return err
	}
	parties, err := s.AgreementRecipients(o.ID, id)
	if err != nil {
		return err
	}
	pages, err := s.agreementPages(o.ID, a)
	if err != nil {
		return err
	}
	names := map[int64]string{}
	c := Certificate{Org: o.Name, Agreement: a.Name, Ref: fmt.Sprintf("AGR-%04d", a.ID), Pages: pages, SentBy: a.CreatedBy, SentAt: time.Now(), CompletedAt: time.Now(),
		Rounds: []CertificateRound{{N: a.Round, Hash: a.FileHash, Outcome: "signed by everyone"}}, Consent: SigningConsent}
	for _, r := range parties {
		if err := drawable(r.Name); err != nil {
			return err
		}
		names[r.ID] = r.Name
		if r.Role == RecipientSigner {
			c.Signers = append(c.Signers, CertificateSigner{Name: r.Name, Email: r.Email, Via: "typed name · email code", Typed: r.Name, IP: "203.0.113.1", Browser: "a browser", SignedAt: time.Now()})
		}
	}
	stamps := make([]Stamp, 0, len(fields))
	for _, f := range fields {
		stamps = append(stamps, Stamp{Page: f.Page, X: f.X, Y: f.Y, W: f.W, H: f.H, Kind: f.Kind, Value: standIn(f.Kind, names[f.RecipientID])})
	}
	_, err = s.SealDocument(o, doc, stamps, c)
	return err
}
