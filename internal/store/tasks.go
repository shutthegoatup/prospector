package store

import (
	"time"

	"gorm.io/gorm"
)

// A task is a brief a person wrote and the requests made on it since. Nothing an
// agent said while working is kept: what it found is the companies, people and
// drafts its events point at, so whoever picks the task up next starts from those.
type Task struct {
	ID        int64      `gorm:"primaryKey" json:"id"`
	OrgID     int64      `gorm:"index;not null" json:"-"`
	Title     string     `gorm:"not null" json:"title"`
	CreatedBy string     `json:"created_by"`
	CreatedAt time.Time  `json:"created_at"`
	ClosedAt  *time.Time `json:"closed_at,omitempty"`
	// OfferingID is what the task is finding companies for; 0 is the organisation's own.
	OfferingID int64 `gorm:"not null;default:0" json:"offering_id"`
	// Want is about how many companies the person asked for; 0 is no number.
	Want int `gorm:"not null;default:0" json:"want"`
}

// TaskRequest is something a person asked for on a task. The first is the brief.
type TaskRequest struct {
	ID      int64      `gorm:"primaryKey" json:"id"`
	OrgID   int64      `gorm:"index;not null" json:"-"`
	TaskID  int64      `gorm:"index;not null" json:"task_id"`
	Text    string     `gorm:"not null" json:"text"`
	AskedBy string     `json:"asked_by"`
	AskedAt time.Time  `json:"asked_at"`
	DoneAt  *time.Time `json:"done_at,omitempty"`
	DoneBy  string     `json:"done_by,omitempty"`
}

// TaskNote is the why that records don't carry: what an agent skipped, where it
// found nothing, what it would do next. It belongs to a task, a company or both.
type TaskNote struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	OrgID     int64     `gorm:"index;not null" json:"-"`
	TaskID    int64     `gorm:"index;not null;default:0" json:"task_id,omitempty"`
	CompanyID int64     `gorm:"index;not null;default:0" json:"company_id,omitempty"`
	Actor     string    `json:"actor"`
	KeyID     int64     `gorm:"not null;default:0" json:"-"`
	Kind      string    `json:"kind"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
}

func OpenTask(tx *gorm.DB, org, id int64) (*Task, error) {
	var t Task
	if err := tx.Where("org_id = ? and id = ? and closed_at is null", org, id).First(&t).Error; err != nil {
		return nil, Missing(err, "no open task %d in this organisation", id)
	}
	return &t, nil
}

// RequestClaim says an agent is on an open request, so another picking up the task
// takes a different one. It is advice, not a lock: anyone's writes still go through.
// It lapses by itself, because agents stop without saying so; times are Unix
// milliseconds, compared the same way on both databases.
type RequestClaim struct {
	RequestID int64  `gorm:"primaryKey;autoIncrement:false"`
	OrgID     int64  `gorm:"index;not null"`
	TaskID    int64  `gorm:"index;not null"`
	Holder    string `gorm:"not null"`
	Actor     string `gorm:"not null"`
	ClaimedAt int64  `gorm:"not null"`
	ExpiresAt int64  `gorm:"not null"`
}
