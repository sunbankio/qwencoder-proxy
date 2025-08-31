package proxy

import (
	"net/http"
	"time"

	"qwenproxy/logging"
	"qwenproxy/streaming"
)

// SharedHTTPClient is the shared HTTP client with connection pooling and timeouts
var SharedHTTPClient *http.Client

func init() {
	// Initialize the shared HTTP client with default settings
	SharedHTTPClient = &http.Client{
		Timeout: streaming.StreamingTimeoutSeconds * time.Second,
	}
	logging.NewLogger().DebugLog("Shared HTTP Client initialized with timeout: %v", SharedHTTPClient.Timeout)
}