//go:build cgo

package sqlutil

import _ "github.com/mattn/go-sqlite3"

// DriverName is the database/sql driver name for the current build.
// CGo builds use mattn/go-sqlite3 which returns errors on OOM instead of panicking.
const DriverName = "sqlite3"
