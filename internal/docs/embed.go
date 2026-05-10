package docs

import "embed"

//go:embed all:out
var StaticFS embed.FS
