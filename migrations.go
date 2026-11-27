package main

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

// migrationFiles are the schema's changes after the baseline, one numbered SQL file
// each (00002_add_x.sql, …), applied in order by goose and recorded in goose_db_version.
//
//go:embed migrations
var migrationFiles embed.FS

//go:embed migrations/baseline/schema.sql
var baselineSchema string

// baseline is the schema as AutoMigrate had built it when goose took over. A database
// that already has its tables, which AutoMigrate made, is only recorded at version 1.
var baseline = goose.NewGoMigration(1, &goose.GoFunc{RunTx: func(ctx context.Context, tx *sql.Tx) error {
	var orgs sql.NullString
	if err := tx.QueryRowContext(ctx, "select to_regclass('orgs')::text").Scan(&orgs); err != nil {
		return err
	}
	if orgs.Valid {
		return nil
	}
	_, err := tx.ExecContext(ctx, baselineSchema)
	return err
}}, nil)

func (s *Store) migrate() error {
	return s.migrateWith(migrationFiles, "migrations")
}

func (s *Store) migrateWith(files fs.FS, dir string) error {
	db, err := s.db.DB()
	if err != nil {
		return err
	}
	numbered, err := fs.Sub(files, dir)
	if err != nil {
		return err
	}
	// Several servers can start on one database at once; one migrates while the others wait.
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return err
	}
	p, err := goose.NewProvider(goose.DialectPostgres, db, numbered,
		goose.WithGoMigrations(baseline), goose.WithSessionLocker(locker), goose.WithDisableGlobalRegistry(true))
	if err != nil {
		return err
	}
	if _, err := p.Up(context.Background()); err != nil {
		return fmt.Errorf("migrating the database: %w", err)
	}
	s.oneNameWhateverItsCapitals()
	s.indexSearch()
	return nil
}
