package outreach

import (
	"gorm.io/gorm"

	"github.com/shutthegoatup/prospector/internal/store"
)

const MailQueued = "queued"

const MailSending = "sending"

const MailSent = "sent"

const MailFailed = "failed"

const MailUnknown = "unknown"

const MailSkipped = "skipped"

const MailCancelled = "cancelled"

// stored is whether a query went through, logging it when it didn't.
func stored(res *gorm.DB) bool { return store.Logged(res) == nil }
