package session

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/sqlutil"
)

// ensureDBDir creates the parent directory of dbPath if it does not exist.
// This is a simple wrapper around os.MkdirAll which is idempotent and fast
// for existing directories (typically one stat syscall). We intentionally
// don't cache results to support multiple database paths in tests and future
// multi-tenancy scenarios.
func ensureDBDir(dbPath string) error {
	dir := filepath.Dir(dbPath)
	if dir != "." && dir != "/" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("session store: create db dir: %w", err)
		}
	}
	return nil
}

// dbOpenOpts configures differences between store DB connections.
type dbOpenOpts struct {
	Label       string // label for InitSQLiteDB ("session", "conversation")
	MaxOpen     int    // connection pool size (0 = driver default)
	MaxIdle     int    // idle connection pool size
	MaxLifetime time.Duration
	MaxIdleTime time.Duration
}

// openSQLiteDB handles the shared DB initialization: ensure dir, open, init, pool settings.
func openSQLiteDB(cfg *config.Config, opts dbOpenOpts) (*sql.DB, error) {
	if err := ensureDBDir(cfg.DB.Path); err != nil {
		return nil, err
	}

	db, err := sql.Open(sqlutil.DriverName, cfg.DB.Path)
	if err != nil {
		return nil, fmt.Errorf("%s store: open db: %w", opts.Label, err)
	}

	if err := sqlutil.InitSQLiteDB(db, &cfg.DB, opts.Label); err != nil {
		_ = db.Close()
		return nil, err
	}

	if opts.MaxOpen > 0 {
		db.SetMaxOpenConns(opts.MaxOpen)
	}
	if opts.MaxIdle > 0 {
		db.SetMaxIdleConns(opts.MaxIdle)
	}
	db.SetConnMaxLifetime(opts.MaxLifetime)
	db.SetConnMaxIdleTime(opts.MaxIdleTime)

	return db, nil
}
