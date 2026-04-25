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

	// Go migration 002: add columns that may already exist on older databases.
	addColumns := goose.NewGoMigration(
		2,
		&goose.GoFunc{
			RunTx: func(ctx context.Context, tx *sql.Tx) error {
				stmts := []string{
					"ALTER TABLE sessions ADD COLUMN owner_id TEXT",
					"ALTER TABLE sessions ADD COLUMN bot_id TEXT",
					"ALTER TABLE sessions ADD COLUMN platform TEXT NOT NULL DEFAULT ''",
					"ALTER TABLE sessions ADD COLUMN platform_key_json TEXT NOT NULL DEFAULT ''",
					"ALTER TABLE sessions ADD COLUMN work_dir TEXT",
				}
				for _, s := range stmts {
					_, _ = tx.ExecContext(ctx, s)
				}
				return nil
			},
		},
		&goose.GoFunc{},
	)

	provider, err := goose.NewProvider(
		goose.DialectSQLite3,
		db,
		migrations,
		goose.WithGoMigrations(addColumns),
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
		slog.Debug("session store: migration applied", "source", r.Source.Path, "duration", r.Duration)
	}
	return nil
}
