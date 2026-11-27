package privacy

import (
	"gorm.io/gorm"

	"github.com/shutthegoatup/prospector/internal/marketing"
	"github.com/shutthegoatup/prospector/internal/store"
)

// forgetMailDomains drops what is kept about the domains of an erased person's
// addresses: a domain can be their own name. It is asked about again if anyone else uses it.
func forgetMailDomains(tx *gorm.DB, emails []string) error {
	var domains []string
	for _, e := range emails {
		if d := marketing.MailDomainOf(e); d != "" {
			domains = append(domains, d)
		}
	}
	if len(domains) == 0 {
		return nil
	}
	return tx.Where("domain in ?", domains).Delete(&store.MailDomain{}).Error
}
