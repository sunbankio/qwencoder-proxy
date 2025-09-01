package config

import (
	"os"
	"testing"
)

func TestDefaultValueWhenEnvNotExists(t *testing.T) {
	// Ensure the environment variable doesn't exist
	os.Unsetenv("PORT")
	
	// Load config - should use default value
	cfg := LoadConfig()
	
	// Verify default value is used
	if cfg.Server.Port != "8143" {
		t.Errorf("Expected default port 8143 when PORT env var doesn't exist, got %s", cfg.Server.Port)
	}
}

func TestDefaultValueWhenEnvEmpty(t *testing.T) {
	// Set environment variable to empty string
	os.Setenv("PORT", "")
	
	// Clean up after test
	defer os.Unsetenv("PORT")
	
	// Load config - should use default value
	cfg := LoadConfig()
	
	// Verify default value is used
	if cfg.Server.Port != "8143" {
		t.Errorf("Expected default port 8143 when PORT env var is empty, got %s", cfg.Server.Port)
	}
}

func TestMixedEnvAndDefault(t *testing.T) {
	// Set only some environment variables
	os.Setenv("PORT", "9000")
	os.Unsetenv("MAX_IDLE_CONNS") // This should use default
	
	// Clean up after test
	defer func() {
		os.Unsetenv("PORT")
	}()
	
	// Load config
	cfg := LoadConfig()
	
	// Verify environment variable is used
	if cfg.Server.Port != "9000" {
		t.Errorf("Expected port from environment variable, got %s", cfg.Server.Port)
	}
	
	// Verify default value is used for unset variable
	if cfg.HTTPClient.MaxIdleConns != 50 {
		t.Errorf("Expected default max idle connections when not set in env, got %d", cfg.HTTPClient.MaxIdleConns)
	}
}