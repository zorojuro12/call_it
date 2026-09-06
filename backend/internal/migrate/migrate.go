// Package migrate applies and reverts the PostgreSQL ledger schema from
// the embedded migrations in the migrations package, with no CLI
// dependency — cmd/migrate is a thin wrapper over this package.
package migrate

import (
	"context"
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/zorojuro12/call_it/backend/migrations"
)

func newMigrator(dsn string) (*migrate.Migrate, error) {
	source, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return nil, fmt.Errorf("migrate: building embedded source: %w", err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", source, dsn)
	if err != nil {
		return nil, fmt.Errorf("migrate: building migrator: %w", err)
	}
	return m, nil
}

// Up applies every pending migration. Running it against an
// already-migrated database is a no-op, not an error.
func Up(ctx context.Context, dsn string) error {
	m, err := newMigrator(dsn)
	if err != nil {
		return err
	}
	defer m.Close()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate: up: %w", err)
	}
	return nil
}

// Down reverts every applied migration. Running it against a database
// with nothing applied is a no-op, not an error.
func Down(ctx context.Context, dsn string) error {
	m, err := newMigrator(dsn)
	if err != nil {
		return err
	}
	defer m.Close()

	if err := m.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate: down: %w", err)
	}
	return nil
}
