package outreach

import "errors"

const SuppressBounced = "bounced"

const SuppressComplained = "complained"

var ErrBounced = errors.New("mail to that address bounced: it doesn't exist or won't take mail")

var ErrComplained = errors.New("they marked a mail as spam")
