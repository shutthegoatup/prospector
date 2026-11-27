package main

import (
	"errors"
	"path"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
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
	InOrder   bool      `gorm:"not null;default:true" json:"in_order"`
	CreatedBy string    `gorm:"not null;default:''" json:"created_by"`
	UpdatedBy string    `gorm:"not null;default:''" json:"updated_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// AgreementFile is the document's bytes, one row to an agreement.
type AgreementFile struct {
	AgreementID int64  `gorm:"primaryKey;autoIncrement:false"`
	OrgID       int64  `gorm:"index;not null"`
	Data        []byte `gorm:"not null;default:''"`
}

const (
	AgreementDraft = "draft"
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
