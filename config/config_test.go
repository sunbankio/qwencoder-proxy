package config

import (
	"os"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	// Test default server configuration
	if cfg.Server.Port != "8143" {
		t.Errorf("Expected default port to be 8143, got %s", cfg.Server.Port)
	}

	// Test default HTTP client configuration
	if cfg.HTTPClient.MaxIdleConns != 50 {
		t.Errorf("Expected default max idle connections to be 50, got %d", cfg.HTTPClient.MaxIdleConns)
	}

	if cfg.HTTPClient.StreamingTimeoutSeconds != 900 {
		t.Errorf("Expected default streaming timeout to be 900, got %d", cfg.HTTPClient.StreamingTimeoutSeconds)
	}
}

func TestLoadConfigFromEnv(t *testing.T) {
	// Save original environment variables
	originalPort := os.Getenv("PORT")
	originalMaxIdleConns := os.Getenv("MAX_IDLE_CONNS")
	originalDebug := os.Getenv("DEBUG")
	
	// Clean up environment variables after test
	defer func() {
		os.Setenv("PORT", originalPort)
		os.Setenv("MAX_IDLE_CONNS", originalMaxIdleConns)
		os.Setenv("DEBUG", originalDebug)
	}()

	// Set some environment variables
	os.Setenv("PORT", "9000")
	os.Setenv("MAX_IDLE_CONNS", "100")
	os.Setenv("DEBUG", "true")

	cfg := LoadConfig()

	// Test that environment variables are loaded
	if cfg.Server.Port != "9000" {
		t.Errorf("Expected port from environment to be 9000, got %s", cfg.Server.Port)
	}

	if cfg.HTTPClient.MaxIdleConns != 100 {
		t.Errorf("Expected max idle connections from environment to be 100, got %d", cfg.HTTPClient.MaxIdleConns)
	}

	if cfg.Logging.IsDebugMode != true {
		t.Errorf("Expected debug mode from environment to be true, got %t", cfg.Logging.IsDebugMode)
	}
}

func TestHTTPClients(t *testing.T) {
	cfg := DefaultConfig()

	// Test that shared HTTP client is created correctly
	client := cfg.SharedHTTPClient()
	if client == nil {
		t.Error("Expected shared HTTP client to be created, got nil")
	}

	if client.Timeout.Seconds() != 300 {
		t.Errorf("Expected shared HTTP client timeout to be 300 seconds, got %f", client.Timeout.Seconds())
	}

	// Test that streaming HTTP client is created correctly
	streamingClient := cfg.StreamingHTTPClient()
	if streamingClient == nil {
		t.Error("Expected streaming HTTP client to be created, got nil")
	}

	if streamingClient.Timeout.Seconds() != 900 {
		t.Errorf("Expected streaming HTTP client timeout to be 900 seconds, got %f", streamingClient.Timeout.Seconds())
	}
}