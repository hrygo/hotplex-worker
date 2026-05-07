package onboard

import _ "embed"

//go:embed assets/config.yaml
var embeddedConfigYAML []byte

// TODO: embed env.example when buildEnvContent is refactored to use it.
