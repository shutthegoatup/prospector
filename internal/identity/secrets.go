package identity

import "github.com/shutthegoatup/prospector/internal/store"

const // Sessions and apiKeys key what a session token and an API key are stored and found by.
Sessions store.Purpose = "prospector: sessions"

const ApiKeys store.Purpose = "prospector: API keys"
