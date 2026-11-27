package prospecting

import (
	"errors"
	"time"
)

const openContacts = "hold is null and replied is null and declined is null and status <> 'suggested' and key not like 'org:%'"

// A rejected company's people leave the queues without being marked, so qualifying
// it again brings them back as they were. Rejected is for the offering each person is
// reached for: the company's own stage for the organisation's, a verdict for another.
const companyNotRejected = "(company_id is null" +
	" or (offering_id = 0 and not exists (select 1 from companies c where c.id = contacts.company_id" +
	" and c.org_id = contacts.org_id and c.stage = '" + CompanyRejected + "'))" +
	" or (offering_id <> 0 and not exists (select 1 from verdicts v where v.org_id = contacts.org_id" +
	" and v.offering_id = contacts.offering_id and v.company_id = contacts.company_id and v.stage = '" + CompanyRejected + "')))"

// MarkDate compares days as text, in the zone today() uses: parsing the day as UTC
// midnight would call today "the future" for the first hours of it east of UTC.
func MarkDate(date string, now time.Time) error {
	if _, err := time.Parse("2006-01-02", date); err != nil {
		return errors.New("date is YYYY-MM-DD")
	}
	if date > now.Format("2006-01-02") {
		return errors.New("date is in the future")
	}
	return nil
}
