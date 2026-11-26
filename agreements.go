package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"gorm.io/gorm"
)

// Agreement is a contract a company is to sign: the document as it will be sent, frozen
// by its hash. Prospector doesn't read what it says, and isn't for deeds or anything
// that must be witnessed. While it is a draft its signers and their fields are set out;
// sending it comes later.
type Agreement struct {
	ID        int64  `gorm:"primaryKey" json:"id"`
	OrgID     int64  `gorm:"index:idx_agreements_company,priority:1;not null" json:"-"`
	CompanyID int64  `gorm:"index:idx_agreements_company,priority:2;not null" json:"company_id"`
	Name      string `gorm:"not null;default:''" json:"name"`
	Status    string `gorm:"not null;default:'draft'" json:"status"`
	// FileName, FileSize and FileHash describe the document; the bytes are kept apart,
	// so a list of agreements never carries them. FileHash is its SHA-256, in hex.
	FileName string `gorm:"not null;default:''" json:"file_name"`
	FileSize int64  `gorm:"not null;default:0" json:"file_size"`
	FileHash string `gorm:"not null;default:''" json:"file_hash"`
	// PageCount is the document's pages, 0 before one is uploaded.
	PageCount int `gorm:"not null;default:0" json:"page_count"`
	// Round counts the versions sent for signing; changes asked for start the next.
	Round int `gorm:"not null;default:1" json:"round"`
	// InOrder is whether signers sign one after another, in their order, or all at once.
	InOrder bool `gorm:"not null;default:true" json:"in_order"`
	// Set when it is sent: when, by whom, until when it can be signed, and what the
	// organisation was called then, which its certificate shows.
	SentAt     *time.Time `json:"sent_at"`
	SentBy     string     `gorm:"not null;default:''" json:"sent_by"`
	SentByUser int64      `gorm:"not null;default:0" json:"-"`
	SentOrg    string     `gorm:"not null;default:''" json:"-"`
	ExpiresAt  *time.Time `json:"expires_at"`
	Subject    string     `gorm:"not null;default:''" json:"subject"`
	Message    string     `gorm:"not null;default:''" json:"message"`
	// SealedHash is the SHA-256 of the sealed PDF once everyone has signed.
	SealedHash string `gorm:"not null;default:''" json:"sealed_hash"`
	// SealProblem is why the last try at sealing it failed, while it waits to be sealed.
	SealProblem string     `gorm:"not null;default:''" json:"seal_problem"`
	CompletedAt *time.Time `json:"completed_at"`
	CreatedBy   string     `gorm:"not null;default:''" json:"created_by"`
	UpdatedBy   string     `gorm:"not null;default:''" json:"updated_by"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// AgreementFile is the document's bytes, one row to an agreement.
type AgreementFile struct {
	AgreementID int64  `gorm:"primaryKey;autoIncrement:false"`
	OrgID       int64  `gorm:"index;not null"`
	Data        []byte `gorm:"not null;default:''"`
}

const (
	AgreementDraft = "draft"
	// AgreementSent is out for signature; AgreementSealing has every signature and is
	// being sealed; the rest are where it ended.
	AgreementSent      = "sent"
	AgreementSealing   = "sealing"
	AgreementCompleted = "completed"
	AgreementDeclined  = "declined"
	AgreementVoided    = "voided"
	AgreementExpired   = "expired"
	// agreementFileLimit is the largest document taken: a contract, not a scan of a box file.
	agreementFileLimit = 10 << 20
)

var errNotDraft = errors.New("only a draft agreement can be changed")

// errTampered is a document that no longer matches the hash it was kept with.
var errTampered = errors.New("the document doesn't match the SHA-256 it was kept with")

func agreementName(name string) (string, error) {
	name = strings.Join(strings.Fields(name), " ")
	switch {
	case name == "":
		return "", errors.New("an agreement needs a name")
	case utf8.RuneCountInString(name) > 120:
		return "", errors.New("an agreement's name is 120 characters at most")
	}
	return name, nil
}

// fileName is a document's name fit to keep and to hand back in a header: its last
// part only, no control characters, and a .pdf at the end.
func fileName(name string) string {
	name = path.Base(strings.ReplaceAll(strings.TrimSpace(name), `\`, "/"))
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == '"' {
			return -1
		}
		return r
	}, name)
	if strings.Trim(name, "./ ") == "" {
		name = "agreement"
	}
	if utf8.RuneCountInString(name) > 120 {
		name = string([]rune(name)[:120])
	}
	if !strings.HasSuffix(strings.ToLower(name), ".pdf") {
		name += ".pdf"
	}
	return name
}

func (s *Store) AddAgreement(org, company int64, name, actor string) (*Agreement, error) {
	name, err := agreementName(name)
	if err != nil {
		return nil, err
	}
	if err := s.in(org).Where("id = ?", company).First(&Company{}).Error; err != nil {
		return nil, missing(err, "no company %d", company)
	}
	a := Agreement{OrgID: org, CompanyID: company, Name: name, Status: AgreementDraft, Round: 1, InOrder: true, CreatedBy: actor, UpdatedBy: actor}
	return &a, s.db.Create(&a).Error
}

func (s *Store) Agreement(org, id int64) (*Agreement, error) {
	var a Agreement
	if err := s.in(org).Where("id = ?", id).First(&a).Error; err != nil {
		return nil, missing(err, "no agreement %d", id)
	}
	return &a, nil
}

// Agreements are an organisation's, or one company's when company isn't 0, newest first.
func (s *Store) Agreements(org, company int64) ([]Agreement, error) {
	q := s.in(org)
	if company != 0 {
		q = q.Where("company_id = ?", company)
	}
	out := []Agreement{}
	return out, q.Order("id desc").Find(&out).Error
}

// ChangeAgreement renames a draft, or says whether its signers sign in order.
func (s *Store) ChangeAgreement(org, id int64, name *string, inOrder *bool, actor string) (*Agreement, error) {
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
	res := s.in(org).Model(&Agreement{}).Where("id = ? and status = ?", id, AgreementDraft).Updates(change)
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

func (s *Store) RemoveAgreement(org, id int64) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
		res := tx.Where("org_id = ? and id = ? and status = ?", org, id, AgreementDraft).Delete(&Agreement{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			if err := tx.Where("org_id = ? and id = ?", org, id).First(&Agreement{}).Error; err != nil {
				return missing(err, "no agreement %d", id)
			}
			return errors.New("only a draft agreement can be removed; one that was sent is kept")
		}
		for _, part := range []any{&AgreementFile{}, &AgreementRecipient{}, &AgreementField{}, &AgreementEvent{}} {
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
func (s *Store) PutAgreementFile(org, id int64, name string, data []byte, actor string) (*Agreement, error) {
	if len(data) > agreementFileLimit {
		return nil, fmt.Errorf("a document is %d MB at most", agreementFileLimit>>20)
	}
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		return nil, errors.New("that isn't a PDF: save the agreement as a PDF first")
	}
	pages, err := pdfPages(data)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	var a Agreement
	err = s.db.Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&Agreement{}).Where("org_id = ? and id = ? and status = ?", org, id, AgreementDraft).
			Updates(map[string]any{"file_name": fileName(name), "file_size": int64(len(data)), "file_hash": hex.EncodeToString(sum[:]), "page_count": pages, "updated_by": actor})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			if err := tx.Where("org_id = ? and id = ?", org, id).First(&a).Error; err != nil {
				return missing(err, "no agreement %d", id)
			}
			return errNotDraft
		}
		if err := tx.Where("org_id = ? and agreement_id = ?", org, id).Delete(&AgreementFile{}).Error; err != nil {
			return err
		}
		if err := tx.Create(&AgreementFile{AgreementID: id, OrgID: org, Data: data}).Error; err != nil {
			return err
		}
		if err := tx.Where("org_id = ? and agreement_id = ? and page > ?", org, id, pages).Delete(&AgreementField{}).Error; err != nil {
			return err
		}
		return tx.Where("org_id = ? and id = ?", org, id).First(&a).Error
	})
	return &a, err
}

// AgreementFile is a document's bytes, checked against the hash it was kept with.
func (s *Store) AgreementDocument(org, id int64) (*Agreement, []byte, error) {
	a, err := s.Agreement(org, id)
	if err != nil {
		return nil, nil, err
	}
	// The record and its file in one statement, so a replacement committing between two
	// reads can't pass for tampering.
	var both struct {
		Agreement
		Data []byte
	}
	read := s.db.Model(&Agreement{}).Select("agreements.*, agreement_files.data as data").
		Joins("join agreement_files on agreement_files.agreement_id = agreements.id and agreement_files.org_id = agreements.org_id").
		Where("agreements.org_id = ? and agreements.id = ?", org, id).Limit(1).Scan(&both)
	if read.Error != nil {
		return nil, nil, read.Error
	}
	if read.RowsAffected == 0 {
		return nil, nil, noSuch("agreement %d has no document yet", id)
	}
	a, f := &both.Agreement, AgreementFile{Data: both.Data}
	if sum := sha256.Sum256(f.Data); hex.EncodeToString(sum[:]) != a.FileHash {
		return nil, nil, fmt.Errorf("agreement %d: %w", id, errTampered)
	}
	return a, f.Data, nil
}
