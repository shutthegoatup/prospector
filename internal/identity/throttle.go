package identity

import (
	"strings"

	"github.com/shutthegoatup/prospector/internal/store"
)

func DeviceMAC(u *store.User, nonce string) string {
	return store.Mac([]byte(u.PasswordHash), "device\x00"+nonce)
}

// The guard counts under names that say what is being guessed at, so the sign-in
// form, whatever is typed into it, can't reach the count for anything else.
func SignInBucket(email string) string {
	return "sign-in\x00" + strings.ToLower(strings.TrimSpace(email))
}
