package config

import (
	"net/http"
	"testing"
	"time"
)

func TestSharedHTTPClient(t *testing.T) {
	cfg := DefaultConfig()
	
	client := cfg.SharedHTTPClient()
	
	// Test that client is created
	if client == nil {
		t.Fatal("Expected shared HTTP client to be created, got nil")
	}
	
	// Test timeout configuration
	expectedTimeout := time.Duration(cfg.HTTPClient.RequestTimeoutSeconds) * time.Second
	if client.Timeout != expectedTimeout {
		t.Errorf("Expected timeout to be %v, got %v", expectedTimeout, client.Timeout)
	}
	
	// Test transport configuration
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatal("Expected client transport to be *http.Transport")
	}
	
	if transport.MaxIdleConns != cfg.HTTPClient.MaxIdleConns {
		t.Errorf("Expected MaxIdleConns to be %d, got %d", cfg.HTTPClient.MaxIdleConns, transport.MaxIdleConns)
	}
	
	if transport.MaxIdleConnsPerHost != cfg.HTTPClient.MaxIdleConnsPerHost {
		t.Errorf("Expected MaxIdleConnsPerHost to be %d, got %d", cfg.HTTPClient.MaxIdleConnsPerHost, transport.MaxIdleConnsPerHost)
	}
	
	expectedIdleConnTimeout := time.Duration(cfg.HTTPClient.IdleConnTimeoutSeconds) * time.Second
	if transport.IdleConnTimeout != expectedIdleConnTimeout {
		t.Errorf("Expected IdleConnTimeout to be %v, got %v", expectedIdleConnTimeout, transport.IdleConnTimeout)
	}
}

func TestStreamingHTTPClient(t *testing.T) {
	cfg := DefaultConfig()
	
	client := cfg.StreamingHTTPClient()
	
	// Test that client is created
	if client == nil {
		t.Fatal("Expected streaming HTTP client to be created, got nil")
	}
	
	// Test timeout configuration
	expectedTimeout := time.Duration(cfg.HTTPClient.StreamingTimeoutSeconds) * time.Second
	if client.Timeout != expectedTimeout {
		t.Errorf("Expected timeout to be %v, got %v", expectedTimeout, client.Timeout)
	}
}