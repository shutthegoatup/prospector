package store

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
