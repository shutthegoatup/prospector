package store

import "time"

// SealKey is the key an organisation's agreements are sealed with: made the first time
// one is needed, its private half sealed under the server's secret key, its certificate
// in every PDF it seals.
type SealKey struct {
	OrgID     int64     `gorm:"primaryKey;autoIncrement:false"`
	Cert      []byte    `gorm:"not null"`
	Key       string    `gorm:"not null;default:''"`
	CreatedAt time.Time `gorm:"not null"`
}
