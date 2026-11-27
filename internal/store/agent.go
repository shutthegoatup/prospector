package store

func (who Actor) Did(e Event) bool {
	if e.KeyID != 0 && who.KeyID != 0 {
		return e.KeyID == who.KeyID
	}
	return e.User == who.Name
}
