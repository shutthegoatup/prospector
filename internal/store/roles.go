package store

import "time"

// Role is one opening at a company, such as a contract, found for an offering that has
// roles. Each is pursued or junked on its own, and the company's verdict for the
// offering follows from them.
type Role struct {
	ID         int64     `gorm:"primaryKey" json:"id"`
	OrgID      int64     `gorm:"index:idx_role,priority:1;not null" json:"-"`
	OfferingID int64     `gorm:"index:idx_role,priority:2;not null" json:"offering_id"`
	CompanyID  int64     `gorm:"index:idx_role,priority:3;not null" json:"company_id"`
	Title      string    `gorm:"not null" json:"title"`
	URL        string    `gorm:"not null;default:''" json:"url"`
	Details    string    `gorm:"not null;default:''" json:"details"`
	Stage      string    `gorm:"not null;default:'review'" json:"stage"`
	Reason     string    `gorm:"not null;default:''" json:"reason"`
	By         string    `gorm:"column:decided_by;not null;default:''" json:"by"`
	FoundBy    string    `gorm:"not null;default:''" json:"found_by"`
	TaskID     int64     `gorm:"not null;default:0" json:"task_id"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}
