package main

// Search matches a substring anywhere in a few text columns, served by a pg_trgm index
// on the columns joined into one text. One text per row, because Postgres will scan the
// table rather than combine an index scan per column. A line break keeps a term from
// matching across two columns.
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
