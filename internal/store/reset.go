package store

import "time"

// PasswordToken lets whoever holds its link choose the account's password, once.
type PasswordToken struct {
	ID        int64  `gorm:"primaryKey"`
	TokenHash string `gorm:"uniqueIndex;not null"`
	UserID    int64  `gorm:"index;not null"`
	Invite    bool   `gorm:"not null;default:false"`
	ExpiresAt time.Time
	CreatedAt time.Time
}

// OrgMail is an organisation's own mail server. Password is sealed, never plain.
type OrgMail struct {
	OrgID     int64 `gorm:"primaryKey;autoIncrement:false"`
	Host      string
	Port      int
	Username  string
	Password  string
	Sender    string
	UpdatedAt time.Time
}
