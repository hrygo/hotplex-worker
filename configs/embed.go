// Package configs embeds the default config.yaml as a byte slice so that other
// packages (e.g. the onboard wizard) can use it as a template without depending
// on the file being present on disk at runtime.
package configs

import _ "embed"

//go:embed config.yaml
var EmbeddedConfigYAML []byte
