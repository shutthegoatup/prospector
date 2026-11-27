package crm

const pgCompanyText = `lower(companies.name || chr(10) || coalesce(companies.domain, '') || chr(10) || coalesce(companies.why, ''))`

const pgPersonText = `lower(coalesce(contacts.person, '') || chr(10) || coalesce(contacts.email, ''))`

const pgContactText = `lower(coalesce(contacts.person, '') || chr(10) || coalesce(contacts.email, '') || chr(10) || coalesce(contacts.maker, ''))`

var postgresSearch = []string{
	`create extension if not exists pg_trgm`,
	`create index if not exists idx_companies_search on companies using gin ((` + pgCompanyText + `) gin_trgm_ops)`,
	`create index if not exists idx_contacts_person_search on contacts using gin ((` + pgPersonText + `) gin_trgm_ops)`,
	`create index if not exists idx_contacts_search on contacts using gin ((` + pgContactText + `) gin_trgm_ops)`,
}
