package store

import (
	"context"
	"net"
	"time"
)

// MailDomain is what DNS last said about whether a domain takes mail. Only a
// definite answer is kept: a lookup that timed out says nothing either way.
type MailDomain struct {
	Domain    string    `gorm:"primaryKey"`
	TakesMail bool      `gorm:"not null;default:false"`
	CheckedAt time.Time `gorm:"not null"`
}

// MailDNS is the part of a resolver the check needs; *net.Resolver is one.
type MailDNS interface {
	LookupMX(ctx context.Context, name string) ([]*net.MX, error)
	LookupHost(ctx context.Context, host string) ([]string, error)
}
