package crm

// Erased says so if an address or a profile belongs to someone erased on request.
func (s *CRMStore) Erased(org int64, email, link string) error {
	return ErasedIn(s.DB, org, "", "", email, link)
}
