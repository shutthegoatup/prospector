package main

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const snapshotSchema = `create table if not exists contacts (
  id integer primary key,
  key text unique not null,
  file text not null, section text, fit text, maker text, person text, first text,
  linkedin text, email text, note text, subject text, body text, followup text, flags text,
  linkedin_added text, email_sent text, followup_sent text, replied text, declined text, hold text
);`

var snapshotExtraColumns = Columns[21:]

func sqlQuote(p *string) string {
	if p == nil {
		return "NULL"
	}
	return "'" + strings.ReplaceAll(*p, "'", "''") + "'"
}

func (c *Contact) values() []*string {
	return []*string{nil, &c.Key, &c.File, c.Section, c.Fit, c.Maker, c.Person, c.First, c.Linkedin, c.Email,
		c.Note, c.Subject, c.Body, c.Followup, c.Flags, c.LinkedinAdded, c.EmailSent, c.FollowupSent,
		c.Replied, c.Declined, c.Hold, c.Sources, c.Phone, c.Called, c.CallNotes, c.Tz, c.SendTo, c.Accepted, c.Message}
}

func (c *Contact) fields() []**string {
	return []**string{nil, nil, nil, &c.Section, &c.Fit, &c.Maker, &c.Person, &c.First, &c.Linkedin, &c.Email,
		&c.Note, &c.Subject, &c.Body, &c.Followup, &c.Flags, &c.LinkedinAdded, &c.EmailSent, &c.FollowupSent,
		&c.Replied, &c.Declined, &c.Hold, &c.Sources, &c.Phone, &c.Called, &c.CallNotes, &c.Tz, &c.SendTo, &c.Accepted, &c.Message}
}

// SnapshotText is the sorted text snapshot the Python tracker wrote, so the committed history carries on.
func (s *Store) SnapshotText(org int64) (string, error) {
	var rows []Contact
	if err := s.in(org).Find(&rows).Error; err != nil {
		return "", err
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Key < rows[j].Key })
	var b strings.Builder
	b.WriteString("-- Outreach tracker snapshot. Restore with: ./outreach restore\n" + snapshotSchema + "\n")
	for _, c := range snapshotExtraColumns {
		fmt.Fprintf(&b, "alter table contacts add column %s text;\n", c)
	}
	cols := strings.Join(Columns, ", ")
	for _, r := range rows {
		vals := r.values()
		out := make([]string, len(vals))
		out[0] = strconv.FormatInt(r.ID, 10)
		for i, v := range vals[1:] {
			out[i+1] = sqlQuote(v)
		}
		fmt.Fprintf(&b, "insert into contacts (%s) values (%s);\n", cols, strings.Join(out, ", "))
	}
	return b.String(), nil
}

// parseInserts reads the snapshot's insert statements, whose quoted values may span lines.
func parseInserts(text string) ([]Contact, error) {
	var out []Contact
	const marker = " values ("
	for {
		start := strings.Index(text, "insert into contacts (")
		if start < 0 {
			return out, nil
		}
		text = text[start:]
		open := strings.Index(text, marker)
		if open < 0 {
			return nil, fmt.Errorf("insert without values")
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
						return nil, fmt.Errorf("unterminated string")
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
			return nil, fmt.Errorf("%d values for %d columns", len(vals), len(cols))
		}
		c := Contact{}
		byName := map[string]int{}
		for i, name := range Columns {
			byName[name] = i
		}
		f := c.fields()
		for i, name := range cols {
			idx, ok := byName[name]
			if !ok {
				return nil, fmt.Errorf("unknown column %q", name)
			}
			switch name {
			case "id":
				id, err := strconv.ParseInt(*vals[i], 10, 64)
				if err != nil {
					return nil, err
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
		out = append(out, c)
	}
}

// Restore replaces an organisation's contacts with the snapshot's. Ids are kept so the
// snapshot round-trips; it fails if another organisation already holds one of them.
func (s *Store) Restore(org int64, text string) (int, error) {
	rows, err := parseInserts(text)
	if err != nil {
		return 0, err
	}
	return s.replaceContacts(org, rows)
}

// ImportLegacy copies the Python tracker's live database, opened read-only, into an
// organisation, replacing its contacts. It carries marks made since the last snapshot.
func (s *Store) ImportLegacy(org int64, path string) (int, error) {
	if _, err := os.Stat(path); err != nil {
		return 0, err
	}
	old, err := gorm.Open(sqlite.Open("file:"+path+"?mode=ro&_pragma=busy_timeout(5000)"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return 0, err
	}
	if sqlDB, err := old.DB(); err == nil {
		defer sqlDB.Close()
	}
	var records []map[string]any
	if err := old.Table("contacts").Order("id").Find(&records).Error; err != nil {
		return 0, fmt.Errorf("read %s: %w", path, err)
	}
	byName := map[string]int{}
	for i, name := range Columns {
		byName[name] = i
	}
	rows := make([]Contact, len(records))
	for i, rec := range records {
		c := &rows[i]
		f := c.fields()
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
	return s.replaceContacts(org, rows)
}

func (s *Store) replaceContacts(org int64, rows []Contact) (int, error) {
	err := s.db.Transaction(func(tx *gorm.DB) error {
		kept := rows[:0]
		for _, r := range rows {
			err := erased(tx, org, displayName(r), deref(r.Maker), deref(r.Email), deref(r.Linkedin))
			if errors.Is(err, errErased) {
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
			return errors.New("that holds no contacts to restore, so nothing was replaced: is it the right file?")
		}
		if err := forgetStrays(tx, org); err != nil {
			return err
		}
		var before []Contact
		if err := tx.Select("id", "key").Where("org_id = ?", org).Find(&before).Error; err != nil {
			return err
		}
		if err := tx.Where("org_id = ?", org).Delete(&Contact{}).Error; err != nil {
			return err
		}
		if err := tx.CreateInBatches(rows, 100).Error; err != nil {
			return fmt.Errorf("restore (another organisation may already use these ids): %w", err)
		}
		if s.postgres() {
			// Only ever forwards: winding back would hand a dropped person's id, and the
			// history still filed under it, to whoever is added next.
			if err := tx.Exec("select setval(pg_get_serial_sequence('contacts', 'id'), greatest((select coalesce(max(id), 1) from contacts), nextval(pg_get_serial_sequence('contacts', 'id'))))").Error; err != nil {
				return err
			}
		}
		if err := follow(tx, org, before, rows); err != nil {
			return err
		}
		return linkCompanies(tx, org, nil)
	})
	return len(rows), err
}

var pointAtContacts = []any{&Event{}, &Fact{}, &Enquiry{}}

// follow keeps history, facts and enquiries with their person across a restore. They
// point at a contact by id, and a restore can drop a contact or give its id to someone
// else, so they are moved by the contact's key, and what belonged to nobody who came
// back is forgotten.
func follow(tx *gorm.DB, org int64, before, after []Contact) error {
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
		for _, table := range pointAtContacts {
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
	here := tx.Model(&Contact{}).Select("id").Where("org_id = ?", org)
	if err := forgetEvents(tx.Where("org_id = ? and contact_id <> 0 and contact_id not in (?)", org, here)); err != nil {
		return err
	}
	if err := tx.Where("org_id = ? and contact_id is not null and contact_id not in (?)", org, here).Delete(&Fact{}).Error; err != nil {
		return err
	}
	return tx.Model(&Enquiry{}).Where("org_id = ? and contact_id is not null and contact_id not in (?)", org, here).
		Update("contact_id", nil).Error
}
