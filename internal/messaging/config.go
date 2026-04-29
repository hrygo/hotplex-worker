package messaging

import "time"

// AdapterConfig groups all dependencies for platform adapter configuration.
// Adapters receive this in a single ConfigureWith call instead of individual setters.
type AdapterConfig struct {
	// Core dependencies (from PlatformAdapter setters)
	Hub     HubInterface
	SM      SessionManager
	Handler HandlerInterface
	Bridge  *Bridge

	// Access control
	Gate *Gate

	// Platform-specific credentials and settings.
	// Slack: bot_token, app_token, assistant_enabled, reconnect_base_delay, reconnect_max_delay, transcriber
	// Feishu: app_id, app_secret, reconnect_base_delay, reconnect_max_delay, transcriber
	Extras map[string]any
}

// ExtrasString extracts a string value from the Extras map.
func (c AdapterConfig) ExtrasString(key string) string {
	v, _ := c.Extras[key].(string)
	return v
}

// ExtrasDuration extracts a time.Duration value from the Extras map.
func (c AdapterConfig) ExtrasDuration(key string) time.Duration {
	v, _ := c.Extras[key].(time.Duration)
	return v
}

// ExtrasBoolPtr extracts a *bool value from the Extras map.
func (c AdapterConfig) ExtrasBoolPtr(key string) *bool {
	v, _ := c.Extras[key].(*bool)
	return v
}
