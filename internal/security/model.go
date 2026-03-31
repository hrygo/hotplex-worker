package security

import (
	"fmt"
	"strings"
)

// AllowedModels contains the set of permitted AI model identifiers.
// New models must be explicitly added here before use.
var AllowedModels = map[string]bool{
	// Claude models.
	"claude-sonnet-4-6":       true,
	"claude-opus-4-6":         true,
	"claude-3-5-sonnet-20241022": true,
	"claude-3-5-haiku-20241022":  true,
	"claude-3-opus-20240229":     true,
	"claude-3-sonnet-20240229":   true,
}

// ValidateModel checks that the model identifier is in the allowed list.
func ValidateModel(model string) error {
	if model == "" {
		return fmt.Errorf("security: empty model name")
	}
	lower := strings.ToLower(model)
	if !AllowedModels[lower] {
		return fmt.Errorf("security: model %q not in allowed list", model)
	}
	return nil
}

// IsModelAllowed returns true if the model is permitted.
func IsModelAllowed(model string) bool {
	return AllowedModels[strings.ToLower(model)]
}
