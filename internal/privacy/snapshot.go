package privacy

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/shutthegoatup/prospector/internal/customers"
	"github.com/shutthegoatup/prospector/internal/identity"
	"github.com/shutthegoatup/prospector/internal/prospecting"
	"github.com/shutthegoatup/prospector/internal/say"
	"github.com/shutthegoatup/prospector/internal/store"
)

const SnapshotSchema = `create table if not exists contacts (
  id integer primary key,
  key text unique not null,
  file text not null, section text, fit text, maker text, person text, first text,
  linkedin text, email text, note text, subject text, body text, followup text, flags text,
  linkedin_added text, email_sent text, followup_sent text, replied text, declined text, hold text
);`

var SnapshotColumns = append(Columns[:len(Columns):len(Columns)], LaterColumns...)

var snapshotExtraColumns = SnapshotColumns[21:]

// LaterColumns are what Prospector added to a contact after the Python tracker. A
// snapshot carries them too: without status a restore would turn everyone an agent
// suggested into someone a person picked.
var LaterColumns = []string{"status", "title", "email_status", "stage", "next_step", "next_date", "lost_reason", "draft_by", "offering_id"}

func LaterValue(field any) *string {
	switch p := field.(type) {
	case **string:
		return *p
	case *int64:
		v := strconv.FormatInt(*p, 10)
		return &v
	}
	return field.(*string)
}

// setLater fills a field from a snapshot's text; a number that doesn't read is 0.
func setLater(field any, v *string) {
	switch p := field.(type) {
	case **string:
		*p = v
	case *int64:
		*p, _ = strconv.ParseInt(store.Deref(v), 10, 64)
	default:
		*field.(*string) = store.Deref(v)
	}
}

func SqlQuote(p *string) string {
	if p == nil {
		return "NULL"
	}
	return "'" + strings.ReplaceAll(*p, "'", "''") + "'"
}

// SnapshotText is the sorted text snapshot the Python tracker wrote, so the committed history carries on.
func (s *PrivacyStore) SnapshotText(org int64) (string, error) {
	var rows []store.Contact
	if err := s.InOrg(org).Find(&rows).Error; err != nil {
		return "", err
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Key < rows[j].Key })
	var b strings.Builder
	b.WriteString("-- Outreach tracker snapshot. Restore with: ./outreach restore\n" + SnapshotSchema + "\n")
	for _, c := range snapshotExtraColumns {
		fmt.Fprintf(&b, "alter table contacts add column %s text;\n", c)
	}
	cols := strings.Join(SnapshotColumns, ", ")
	for _, r := range rows {
		vals := r.Values()
		for _, f := range r.Later() {
			vals = append(vals, LaterValue(f))
		}
		out := make([]string, len(vals))
		out[0] = strconv.FormatInt(r.ID, 10)
		for i, v := range vals[1:] {
			out[i+1] = SqlQuote(v)
		}
		fmt.Fprintf(&b, "insert into contacts (%s) values (%s);\n", cols, strings.Join(out, ", "))
	}
	return b.String(), nil
}

// ParseInserts reads the snapshot's insert statements, whose quoted values may span
// lines, and says which of laterColumns no statement named: a snapshot written before
// they were carried.
func ParseInserts(text string) (out []store.Contact, absent []string, err error) {
	named := map[string]bool{}
	for text != "" {
		var c *store.Contact
		if c, text, err = parseInsert(text, named); err != nil {
			return nil, nil, err
		}
		if c != nil {
			out = append(out, *c)
		}
	}
	for _, name := range LaterColumns {
		if !named[name] {
			absent = append(absent, name)
		}
	}
	return out, absent, nil
}

// parseInsert reads the next insert statement, if there is one, and returns what follows it.
func parseInsert(text string, named map[string]bool) (*store.Contact, string, error) {
	const marker = " values ("
	start := strings.Index(text, "insert into contacts (")
	if start < 0 {
		return nil, "", nil
	}
	text = text[start:]
	open := strings.Index(text, marker)
	if open < 0 {
		return nil, "", say.Errorf("insert without values")
	}
	colList := text[len("insert into contacts ("):strings.Index(text, ")")]
	cols := strings.Split(colList, ", ")
	text = text[open+len(marker):]
	var vals []*string
	for {
		text = strings.TrimLeft(text, " ")
		switch {
		case strings.HasPrefix(text, "NULL"):
			vals, text = append(vals, nil), text[4:]
		case strings.HasPrefix(text, "'"):
			var b strings.Builder
			i := 1
			for {
				j := strings.IndexByte(text[i:], '\'')
				if j < 0 {
					return nil, "", say.Errorf("unterminated string")
				}
				b.WriteString(text[i : i+j])
				i += j + 1
				if i < len(text) && text[i] == '\'' {
					b.WriteByte('\'')
					i++
					continue
				}
				break
			}
			v := b.String()
			vals, text = append(vals, &v), text[i:]
		default:
			end := strings.IndexAny(text, ",)")
			v := strings.TrimSpace(text[:end])
			vals, text = append(vals, &v), text[end:]
		}
		if text[0] == ')' {
			break
		}
		text = text[1:]
	}
	if len(vals) != len(cols) {
		return nil, "", say.Errorf("%d values for %d columns", len(vals), len(cols))
	}
	c := store.Contact{}
	f, later := c.Fields(), c.Later()
	for i, name := range cols {
		if at := slices.Index(LaterColumns, name); at >= 0 {
			named[name] = true
			setLater(later[at], vals[i])
			continue
		}
		idx := slices.Index(Columns, name)
		if idx < 0 {
			return nil, "", say.Errorf("unknown column %q", name)
		}
		switch name {
		case "id":
			id, err := strconv.ParseInt(*vals[i], 10, 64)
			if err != nil {
				return nil, "", err
			}
			c.ID = id
		case "key":
			c.Key = *vals[i]
		case "file":
			c.File = *vals[i]
		default:
			*f[idx] = vals[i]
		}
	}
	return &c, text, nil
}

// Restore replaces an organisation's contacts with the snapshot's. Ids are kept so the
// snapshot round-trips; it fails if another organisation already holds one of them.
func (s *PrivacyStore) Restore(org int64, text string) (int, error) {
	rows, absent, err := ParseInserts(text)
	if err != nil {
		return 0, err
	}
	return s.ReplaceContacts(org, rows, absent)
}

// ImportLegacy copies the Python tracker's live database, opened read-only, into an
// organisation, replacing its contacts. It carries marks made since the last snapshot.
func (s *PrivacyStore) ImportLegacy(org int64, path string) (int, error) {
	if _, err := os.Stat(path); err != nil {
		return 0, err
	}
	old, err := gorm.Open(sqlite.Open("file:"+path+"?mode=ro&_pragma=busy_timeout(5000)"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return 0, err
	}
	if sqlDB, err := old.DB(); err == nil {
		defer func() { _ = sqlDB.Close() }()
	}
	var records []map[string]any
	if err := old.Table("contacts").Order("id").Find(&records).Error; err != nil {
		return 0, fmt.Errorf("read %s: %w", path, err)
	}
	byName := map[string]int{}
	for i, name := range Columns {
		byName[name] = i
	}
	rows := make([]store.Contact, len(records))
	for i, rec := range records {
		c := &rows[i]
		f := c.Fields()
		for col, v := range rec {
			idx, known := byName[col]
			if !known || v == nil {
				continue
			}
			text := fmt.Sprint(v)
			if b, ok := v.([]byte); ok {
				text = string(b)
			}
			switch col {
			case "id":
				id, err := strconv.ParseInt(text, 10, 64)
				if err != nil {
					return 0, fmt.Errorf("contact id %q: %w", text, err)
				}
				c.ID = id
			case "key":
				c.Key = text
			case "file":
				c.File = text
			default:
				*f[idx] = &text
			}
		}
	}
	return s.ReplaceContacts(org, rows, LaterColumns)
}

// ReplaceContacts swaps an organisation's contacts for rows. absent names the
// laterColumns the rows' source doesn't hold: someone already here under the same key
// keeps what they have in those, since the source says nothing about them.
func (s *PrivacyStore) ReplaceContacts(org int64, rows []store.Contact, absent []string) (int, error) {
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		kept := rows[:0]
		for _, r := range rows {
			err := prospecting.ErasedIn(tx, org, store.DisplayName(r), store.Deref(r.Maker), store.Deref(r.Email), store.Deref(r.Linkedin))
			if errors.Is(err, store.ErrErased) {
				continue
			}
			if err != nil {
				return err
			}
			r.OrgID = org
			kept = append(kept, r)
		}
		rows = kept
		if len(rows) == 0 {
			return say.New("that holds no contacts to restore, so nothing was replaced: is it the right file?")
		}
		if err := forgetStrays(tx, org); err != nil {
			return err
		}
		var before []store.Contact
		if err := tx.Where("org_id = ?", org).Find(&before).Error; err != nil {
			return err
		}
		have := map[string]*store.Contact{}
		for i := range before {
			have[before[i].Key] = &before[i]
		}
		for i := range rows {
			if was := have[rows[i].Key]; was != nil {
				from, to := was.Later(), rows[i].Later()
				for _, name := range absent {
					at := slices.Index(LaterColumns, name)
					setLater(to[at], LaterValue(from[at]))
				}
			}
		}
		if err := tx.Where("org_id = ?", org).Delete(&store.Contact{}).Error; err != nil {
			return err
		}
		if err := tx.CreateInBatches(rows, 100).Error; err != nil {
			return fmt.Errorf("restore (another organisation may already use these ids): %w", err)
		}
		// Only ever forwards: winding back would hand a dropped person's id, and the
		// history still filed under it, to whoever is added next.
		if err := tx.Exec("select setval(pg_get_serial_sequence('contacts', 'id'), greatest((select coalesce(max(id), 1) from contacts), nextval(pg_get_serial_sequence('contacts', 'id'))))").Error; err != nil {
			return err
		}
		if err := follow(tx, org, before, rows); err != nil {
			return err
		}
		// A snapshot can name an offering removed since it was taken: those people are
		// then the organisation's own, as removing it would have made them.
		if err := tx.Model(&store.Contact{}).Where("org_id = ? and offering_id <> 0 and offering_id not in (?)", org,
			tx.Model(&store.Offering{}).Select("id").Where("org_id = ?", org)).Update("offering_id", 0).Error; err != nil {
			return err
		}
		return LinkCompanies(tx, org, nil)
	})
	return len(rows), err
}

var PointAtContacts = []any{&store.Event{}, &store.Fact{}, &store.Enquiry{}, &store.Suggestion{}, &store.Deal{}, &store.CustomerGrant{}, &store.CampaignRecipient{}, &store.AgreementRecipient{}}

// follow keeps history, facts and enquiries with their person across a restore. They
// point at a contact by id, and a restore can drop a contact or give its id to someone
// else, so they are moved by the contact's key, and what belonged to nobody who came
// back is forgotten.
func follow(tx *gorm.DB, org int64, before, after []store.Contact) error {
	now := map[string]int64{}
	for _, c := range after {
		now[c.Key] = c.ID
	}
	moved := map[int64]int64{}
	for _, c := range before {
		if now[c.Key] != c.ID {
			moved[c.ID] = now[c.Key]
		}
	}
	repoint := func(from, to int64) error {
		for _, table := range PointAtContacts {
			if err := tx.Model(table).Where("org_id = ? and contact_id = ?", org, from).Update("contact_id", to).Error; err != nil {
				return err
			}
		}
		return nil
	}
	// Parked on negative ids first, so two contacts trading ids don't pick up each other's.
	for old := range moved {
		if err := repoint(old, -old); err != nil {
			return err
		}
	}
	for old, id := range moved {
		if id != 0 {
			if err := repoint(-old, id); err != nil {
				return err
			}
		}
	}
	return forgetStrays(tx, org)
}

// forgetStrays deals with what points at a contact that isn't there: history is
// scrubbed as it is for an erased person, facts about them go, and an enquiry is
// kept as it arrived but matched to nobody.
func forgetStrays(tx *gorm.DB, org int64) error {
	here := tx.Model(&store.Contact{}).Select("id").Where("org_id = ?", org)
	if err := forgetEvents(tx.Where("org_id = ? and contact_id <> 0 and contact_id not in (?)", org, here)); err != nil {
		return err
	}
	if err := tx.Where("org_id = ? and contact_id is not null and contact_id not in (?)", org, here).Delete(&store.Fact{}).Error; err != nil {
		return err
	}
	if err := tx.Where("org_id = ? and contact_id not in (?)", org, here).Delete(&store.Deal{}).Error; err != nil {
		return err
	}
	// Nobody is left to open a page that was theirs.
	if err := customers.DropCustomerGrants(tx, org, tx.Where("org_id = ? and contact_id not in (?)", org, here), false); err != nil {
		return err
	}
	if err := identity.DropRecipients(tx, tx.Where("org_id = ? and contact_id is not null and contact_id not in (?)", org, here)); err != nil {
		return err
	}
	// A mail sent to someone a restore dropped stays counted, belonging to nobody.
	if err := tx.Model(&store.CampaignRecipient{}).Where("org_id = ? and contact_id is not null and contact_id not in (?)", org, here).
		Update("contact_id", nil).Error; err != nil {
		return err
	}
	// What was suggested for them has nobody left to wait for.
	if err := tx.Where("org_id = ? and contact_id not in (?)", org, here).Delete(&store.Suggestion{}).Error; err != nil {
		return err
	}
	return tx.Model(&store.Enquiry{}).Where("org_id = ? and contact_id is not null and contact_id not in (?)", org, here).
		Update("contact_id", nil).Error
}
