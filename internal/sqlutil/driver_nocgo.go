//go:build !cgo

package sqlutil

import _ "modernc.org/sqlite"

// DriverName is the database/sql driver name for the current build.
// Non-CGo builds (cross-compilation, Windows) use pure-Go modernc.org/sqlite.
const DriverName = "sqlite"
