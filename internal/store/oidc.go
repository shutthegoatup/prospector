package store

// IdpSpec is this app as one OpenID Connect provider has it registered.
type IdpSpec struct {
	Issuer, ClientID, Secret, Redirect string
	// Plain is LinkedIn: it documents neither PKCE nor a nonce, wants the secret in the
	// form, and says whether the email is verified only at its userinfo endpoint.
	Plain bool
	// ManyTenants allows an issuer that serves every tenant under one registration.
	// Only Microsoft's social sign-in is one: an organisation's own must pin its tenant.
	ManyTenants bool
}
