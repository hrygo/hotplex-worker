package session

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"

	"github.com/pressly/goose/v3"
)

//go:embed sql/migrations/*.sql
var migrationFS embed.FS

// runMigrations applies all pending goose migrations to the database.
func runMigrations(ctx context.Context, db *sql.DB) error {
	migrations, err := fs.Sub(migrationFS, "sql/migrations")
	if err != nil {
		return fmt.Errorf("session store: migration fs: %w", err)
	}

	provider, err := goose.NewProvider(
		goose.DialectSQLite3,
		db,
		migrations,
		goose.WithDisableGlobalRegistry(true),
	)
	if err != nil {
		return fmt.Errorf("session store: goose provider: %w", err)
	}

	results, err := provider.Up(ctx)
	if err != nil {
		return fmt.Errorf("session store: goose up: %w", err)
	}
	for _, r := range results {
		slog.Default().Debug("session store: migration applied", "component", "session_store", "source", r.Source.Path, "duration", r.Duration)
	}
	return nil
}
