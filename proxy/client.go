package proxy

import (
	"net/http"
	"qwenproxy/logging"
	"time"
)

// SharedHTTPClient is the shared HTTP client with connection pooling and timeouts
var SharedHTTPClient *http.Client

const (
	StreamingTimeoutSeconds = 900
)

func init() {
	// Initialize the shared HTTP client with default settings
	SharedHTTPClient = &http.Client{
		Timeout: StreamingTimeoutSeconds * time.Second,
	}
	logging.NewLogger().DebugLog("Shared HTTP Client initialized with timeout: %v", SharedHTTPClient.Timeout)
}
