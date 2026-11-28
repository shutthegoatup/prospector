// Package migrations is the schema: the baseline and the numbered goose migrations after it.
package migrations

import "embed"

// Numbered are the schema's changes after the baseline, one SQL file each
// (00002_add_x.sql, …), applied in order by goose and recorded in goose_db_version.
//
//go:embed *.sql
var Numbered embed.FS

// Baseline is the schema as AutoMigrate had built it when goose took over.
//
//go:embed baseline/schema.sql
var Baseline string
