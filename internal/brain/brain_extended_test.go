package brain

import (
	"context"
	"log/slog"
	"testing"
)

// ========================================
// Brain Global Tests
// ========================================

func TestGlobal(t *testing.T) {
	// Test Global getter/setter
	SetGlobal(nil)
	g := Global()
	if g != nil {
		t.Error("Global should return nil after SetGlobal(nil)")
	}
}

func TestGetRouter(t *testing.T) {
	// GetRouter should not panic
	r := GetRouter()
	if r != nil {
		t.Log("GetRouter returned non-nil")
	}
}

func TestGetRateLimiter(t *testing.T) {
	// GetRateLimiter should not panic
	rl := GetRateLimiter()
	if rl != nil {
		t.Log("GetRateLimiter returned non-nil")
	}
}

// ========================================
// Guard Tests
// ========================================

func TestGuardConfig(t *testing.T) {
	cfg := GuardConfig{
		Enabled:            true,
		InputGuardEnabled:  true,
		OutputGuardEnabled: false,
		AdminUsers:         []string{"admin-user"},
		BanPatterns:        []string{"bad pattern"},
		MaxInputLength:     10000,
		Sensitivity:        "medium",
	}

	if !cfg.Enabled {
		t.Error("Enabled should be true")
	}
	if !cfg.InputGuardEnabled {
		t.Error("InputGuardEnabled should be true")
	}
	if cfg.OutputGuardEnabled {
		t.Error("OutputGuardEnabled should be false")
	}
	if len(cfg.AdminUsers) != 1 {
		t.Errorf("AdminUsers length = %d, want 1", len(cfg.AdminUsers))
	}
	if len(cfg.BanPatterns) != 1 {
		t.Error("BanPatterns length should be 1")
	}
	if cfg.MaxInputLength != 10000 {
		t.Error("MaxInputLength should be 10000")
	}
	if cfg.Sensitivity != "medium" {
		t.Error("Sensitivity should be medium")
	}
}

func TestDefaultGuardConfig(t *testing.T) {
	cfg := DefaultGuardConfig()

	if !cfg.Enabled {
		t.Error("Enabled should default to true")
	}
}

func TestNewSafetyGuard_NilBrain(t *testing.T) {
	logger := slog.Default()

	// Should work with nil brain
	guard, err := NewSafetyGuard(nil, GuardConfig{}, logger)
	if err != nil {
		t.Logf("NewSafetyGuard with nil brain: %v", err)
	}
	if guard != nil {
		t.Log("Guard created successfully")
	}
}

func TestSafetyGuard_CheckInput(t *testing.T) {
	logger := slog.Default()
	guard, _ := NewSafetyGuard(nil, GuardConfig{
		InputGuardEnabled: true,
	}, logger)

	ctx := context.Background()
	result := guard.CheckInput(ctx, "test input")

	if result == nil {
		t.Error("CheckInput should return non-nil result")
	}
}

func TestSafetyGuard_CheckOutput(t *testing.T) {
	logger := slog.Default()
	guard, _ := NewSafetyGuard(nil, GuardConfig{
		OutputGuardEnabled: true,
	}, logger)

	result := guard.CheckOutput("test output")

	if result == nil {
		t.Error("CheckOutput should return non-nil result")
	}
}

func TestSafetyGuard_SanitizeOutput(t *testing.T) {
	logger := slog.Default()
	guard, _ := NewSafetyGuard(nil, GuardConfig{}, logger)

	result := guard.SanitizeOutput("test <script>alert('xss')</script>")
	t.Logf("Sanitized: %s", result)
}

func TestSafetyGuard_IsAdmin(t *testing.T) {
	logger := slog.Default()
	guard, _ := NewSafetyGuard(nil, GuardConfig{
		AdminUsers:    []string{"admin-user"},
		AdminChannels: []string{"admin-channel"},
	}, logger)

	// Test admin user detection
	if !guard.IsAdmin("admin-user", "channel123") {
		t.Error("Should recognize admin user")
	}

	// Test admin channel detection
	if !guard.IsAdmin("regular-user", "admin-channel") {
		t.Error("Should recognize admin channel")
	}

	// Test non-admin
	if guard.IsAdmin("invalid-user", "invalid-channel") {
		t.Error("Should not recognize invalid user/channel")
	}
}

func TestSafetyGuard_Stats(t *testing.T) {
	logger := slog.Default()
	guard, _ := NewSafetyGuard(nil, GuardConfig{}, logger)

	stats := guard.Stats()
	if stats == nil {
		t.Error("Stats should not return nil")
	}
}

func TestSafetyGuard_SetEnabled(t *testing.T) {
	logger := slog.Default()
	guard, _ := NewSafetyGuard(nil, GuardConfig{}, logger)

	// Set enabled/disabled - should not panic
	guard.SetEnabled(false)
	guard.SetEnabled(true)
}

// ========================================
// Config Tests
// ========================================

func TestGetEnv(t *testing.T) {
	t.Setenv("HOTPLEX_TEST_VAR", "test_value")

	val := getEnv("HOTPLEX_TEST_VAR", "default")
	if val != "test_value" {
		t.Errorf("getEnv = %s, want test_value", val)
	}

	// Test default
	val = getEnv("HOTPLEX_NONEXISTENT", "default")
	if val != "default" {
		t.Errorf("getEnv = %s, want default", val)
	}
}
