package outreach

import "github.com/shutthegoatup/prospector/internal/say"

const SuppressBounced = "bounced"

const SuppressComplained = "complained"

var ErrBounced = say.New("mail to that address bounced: it doesn't exist or won't take mail")

var ErrComplained = say.New("they marked a mail as spam")

// bounceBy is who a bounced address is put on the list by.
const bounceBy = "Postmark"
