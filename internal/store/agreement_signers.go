package store

import "time"

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
