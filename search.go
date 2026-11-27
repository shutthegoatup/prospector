package main

// Search matches a substring anywhere in a few text columns, served by a pg_trgm index
// on the columns joined into one text. One text per row, because Postgres will scan the
// table rather than combine an index scan per column. A line break keeps a term from
// matching across two columns.
