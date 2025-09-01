package config

import (
	"os"
	"strconv"
	"strings"
)

// LoadConfig loads configuration from environment variables
func LoadConfig() *Config {
	config := DefaultConfig()

	// Load server configuration
	if port := os.Getenv("PORT"); port != "" {
		config.Server.Port = port
	}

	// Load HTTP client configuration
	if maxIdleConns := os.Getenv("MAX_IDLE_CONNS"); maxIdleConns != "" {
		if val, err := strconv.Atoi(maxIdleConns); err == nil {
			config.HTTPClient.MaxIdleConns = val
		}
	}
	if maxIdleConnsPerHost := os.Getenv("MAX_IDLE_CONNS_PER_HOST"); maxIdleConnsPerHost != "" {
		if val, err := strconv.Atoi(maxIdleConnsPerHost); err == nil {
			config.HTTPClient.MaxIdleConnsPerHost = val
		}
	}
	if idleConnTimeout := os.Getenv("IDLE_CONN_TIMEOUT_SECONDS"); idleConnTimeout != "" {
		if val, err := strconv.Atoi(idleConnTimeout); err == nil {
			config.HTTPClient.IdleConnTimeoutSeconds = val
		}
	}
	if requestTimeout := os.Getenv("REQUEST_TIMEOUT_SECONDS"); requestTimeout != "" {
		if val, err := strconv.Atoi(requestTimeout); err == nil {
			config.HTTPClient.RequestTimeoutSeconds = val
		}
	}
	if streamingTimeout := os.Getenv("STREAMING_TIMEOUT_SECONDS"); streamingTimeout != "" {
		if val, err := strconv.Atoi(streamingTimeout); err == nil {
			config.HTTPClient.StreamingTimeoutSeconds = val
		}
	}
	if readTimeout := os.Getenv("READ_TIMEOUT_SECONDS"); readTimeout != "" {
		if val, err := strconv.Atoi(readTimeout); err == nil {
			config.HTTPClient.ReadTimeoutSeconds = val
		}
	}

	// Load logging configuration
	if debugMode := os.Getenv("DEBUG"); debugMode != "" {
		config.Logging.IsDebugMode = strings.ToLower(debugMode) == "true"
	}

	return config
}