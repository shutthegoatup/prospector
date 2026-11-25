package main

import (
	"log"
	"strings"
	"unicode/utf8"
)

// Search matches a substring anywhere in a few text columns. A trigram index serves
// that on both databases: Postgres indexes the columns joined into one text with pg_trgm,
// and SQLite keeps its own FTS5 tables in step with triggers.

var sqliteSearch = []string{
	`create virtual table if not exists companies_fts using fts5(name, domain, why, tokenize='trigram')`,
	`create virtual table if not exists contacts_fts using fts5(person, email, maker, tokenize='trigram')`,
	`create trigger if not exists companies_fts_insert after insert on companies begin
		insert into companies_fts(rowid, name, domain, why) values (new.id, new.name, new.domain, new.why); end`,
	`create trigger if not exists companies_fts_delete after delete on companies begin
		delete from companies_fts where rowid = old.id; end`,
	`create trigger if not exists companies_fts_update after update of name, domain, why on companies begin
		delete from companies_fts where rowid = old.id;
		insert into companies_fts(rowid, name, domain, why) values (new.id, new.name, new.domain, new.why); end`,
	`create trigger if not exists contacts_fts_insert after insert on contacts begin
		insert into contacts_fts(rowid, person, email, maker) values (new.id, new.person, new.email, new.maker); end`,
	`create trigger if not exists contacts_fts_delete after delete on contacts begin
		delete from contacts_fts where rowid = old.id; end`,
	`create trigger if not exists contacts_fts_update after update of person, email, maker on contacts begin
		delete from contacts_fts where rowid = old.id;
		insert into contacts_fts(rowid, person, email, maker) values (new.id, new.person, new.email, new.maker); end`,
	// Built afresh at every start: a migration that rebuilds a table takes its triggers
	// with it, and an edit made meanwhile would otherwise stay searchable by its old text.
	`delete from companies_fts`,
	`insert into companies_fts(rowid, name, domain, why) select id, name, domain, why from companies`,
	`delete from contacts_fts`,
	`insert into contacts_fts(rowid, person, email, maker) select id, person, email, maker from contacts`,
}

// One text per row, because Postgres will scan the table rather than combine an index
// scan per column. A line break keeps a term from matching across two columns.
const (
	pgCompanyText = `lower(companies.name || chr(10) || coalesce(companies.domain, '') || chr(10) || coalesce(companies.why, ''))`
	pgPersonText  = `lower(coalesce(contacts.person, '') || chr(10) || coalesce(contacts.email, ''))`
	pgContactText = `lower(coalesce(contacts.person, '') || chr(10) || coalesce(contacts.email, '') || chr(10) || coalesce(contacts.maker, ''))`
)

var postgresSearch = []string{
	`create extension if not exists pg_trgm`,
	`create index if not exists idx_companies_search on companies using gin ((` + pgCompanyText + `) gin_trgm_ops)`,
	`create index if not exists idx_contacts_person_search on contacts using gin ((` + pgPersonText + `) gin_trgm_ops)`,
	`create index if not exists idx_contacts_search on contacts using gin ((` + pgContactText + `) gin_trgm_ops)`,
}

// indexSearch never stops the server starting: without the index, search scans as it always did.
func (s *Store) indexSearch() {
	stmts := sqliteSearch
	if s.postgres() {
		stmts = postgresSearch
	}
	for _, stmt := range stmts {
		if err := s.db.Exec(stmt).Error; err != nil {
			log.Printf("search index unavailable, searching by scan: %v", err)
			return
		}
	}
	s.fts = !s.postgres()
}

// A trigram index can't answer a term shorter than three characters.
func (s *Store) useFTS(term string) bool { return s.fts && utf8.RuneCountInString(term) >= 3 }

func ftsPhrase(term string) string { return `"` + strings.ReplaceAll(term, `"`, `""`) + `"` }

func likeAny(term string, cols ...string) (string, []any) {
	var parts []string
	var args []any
	for _, c := range cols {
		parts = append(parts, "lower("+c+") like ?")
		args = append(args, "%"+term+"%")
	}
	return "(" + strings.Join(parts, " or ") + ")", args
}

// term is already lower case.
func (s *Store) companyMatch(term string) (string, []any) {
	switch {
	case s.useFTS(term):
		return "companies.id in (select rowid from companies_fts where companies_fts match ?)", []any{ftsPhrase(term)}
	case s.postgres():
		return pgCompanyText + " like ?", []any{"%" + term + "%"}
	}
	return likeAny(term, "companies.name", "companies.domain", "companies.why")
}

func (s *Store) contactMatch(term string, maker bool) (string, []any) {
	cols, text := []string{"person", "email"}, pgPersonText
	if maker {
		cols, text = append(cols, "maker"), pgContactText
	}
	switch {
	case s.useFTS(term):
		return "contacts.id in (select rowid from contacts_fts where contacts_fts match ?)",
			[]any{"{" + strings.Join(cols, " ") + "} : " + ftsPhrase(term)}
	case s.postgres():
		return text + " like ?", []any{"%" + term + "%"}
	}
	for i, c := range cols {
		cols[i] = "contacts." + c
	}
	return likeAny(term, cols...)
}
