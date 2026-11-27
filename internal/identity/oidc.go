package identity

// Outsider is who a provider says has signed in.
type Outsider struct {
	Issuer, Subject    string
	Email, Name        string
	Username, HostedBy string
	// Vouched is the provider saying the address is this person's own. Disowned is
	// it saying outright that it isn't known to be.
	Vouched, Disowned bool
}
