package privacy

import (
	"strings"

	"gorm.io/gorm"

	"github.com/shutthegoatup/prospector/internal/store"
)

// forgetAddresses drops what is held against addresses being erased. The erasure's own
// hash of each keeps them from being mailed again.
func forgetAddresses(tx *gorm.DB, org int64, addresses []string) error {
	if len(addresses) == 0 {
		return nil
	}
	// As the erasure's own hash does: a dot at either end is the sentence's, not the address's.
	for _, a := range addresses {
		addresses = append(addresses, strings.Trim(a, "."))
	}
	if err := tx.Where("org_id = ? and address in ?", org, addresses).Delete(&store.Suppression{}).Error; err != nil {
		return err
	}
	return tx.Where("org_id = ? and address in ?", org, addresses).Delete(&store.UnsubscribeLink{}).Error
}
