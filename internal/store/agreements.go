package store

import "time"

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

// SealKey is the key an organisation's agreements are sealed with: made the first time
// one is needed, its private half sealed under the server's secret key, its certificate
// in every PDF it seals.
type SealKey struct {
	OrgID     int64     `gorm:"primaryKey;autoIncrement:false"`
	Cert      []byte    `gorm:"not null"`
	Key       string    `gorm:"not null;default:''"`
	CreatedAt time.Time `gorm:"not null"`
}

// AgreementEvent is one thing that happened to an agreement once it went out: sent, a
// link opened, a code entered, signed, sealed. Rows are only ever added. Each carries
// the hash of the one before it, so a row changed or taken out afterwards shows. The
// certificate in a sealed PDF is drawn from these.
type AgreementEvent struct {
	ID          int64     `gorm:"primaryKey" json:"id"`
	OrgID       int64     `gorm:"index;not null" json:"-"`
	AgreementID int64     `gorm:"uniqueIndex:idx_agreement_event_chain,priority:1;not null" json:"agreement_id"`
	Round       int       `gorm:"not null;default:1" json:"round"`
	At          time.Time `gorm:"not null" json:"at"`
	Kind        string    `gorm:"not null;default:''" json:"kind"`
	Actor       string    `gorm:"not null;default:''" json:"actor"`
	RecipientID *int64    `json:"recipient_id"`
	Detail      string    `gorm:"not null;default:''" json:"detail"`
	IP          string    `gorm:"not null;default:''" json:"ip"`
	UserAgent   string    `gorm:"not null;default:''" json:"user_agent"`
	// Prev is the hash of the event before it on the same agreement, "" for the first.
	// One agreement can't have two events after the same one, so two writers racing to
	// add the next can't fork the chain: one of them is refused.
	Prev string `gorm:"uniqueIndex:idx_agreement_event_chain,priority:2;not null;default:''" json:"prev"`
	Hash string `gorm:"not null;default:''" json:"hash"`
}

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
