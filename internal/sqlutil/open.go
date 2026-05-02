package sqlutil

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/hrygo/hotplex/internal/config"
)

// PoolOpts configures connection pool settings for OpenDB.
type PoolOpts struct {
	MaxOpen     int
	MaxIdle     int
	MaxLifetime time.Duration
	MaxIdleTime time.Duration
}

// OpenDB creates a ready-to-use SQLite connection: ensures parent directory,
// opens the database, applies standard PRAGMAs, and configures the pool.
func OpenDB(dbPath string, dbCfg *config.DBConfig, label string, pool PoolOpts) (*sql.DB, error) {
	dir := filepath.Dir(dbPath)
	if dir != "." && dir != "/" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("%s: create db dir: %w", label, err)
		}
	}

	db, err := sql.Open(DriverName, dbPath)
	if err != nil {
		return nil, fmt.Errorf("%s: open db: %w", label, err)
	}

	if err := InitSQLiteDB(db, dbCfg, label); err != nil {
		_ = db.Close()
		return nil, err
	}

	if pool.MaxOpen > 0 {
		db.SetMaxOpenConns(pool.MaxOpen)
	}
	if pool.MaxIdle > 0 {
		db.SetMaxIdleConns(pool.MaxIdle)
	}
	db.SetConnMaxLifetime(pool.MaxLifetime)
	db.SetConnMaxIdleTime(pool.MaxIdleTime)

	return db, nil
}
