package proxy

import (
	"net/http"

	"github.com/sunbankio/qwencoder-proxy/config"
	"github.com/sunbankio/qwencoder-proxy/logging"
)

// SharedHTTPClient is the shared HTTP client with connection pooling and timeouts
var SharedHTTPClient *http.Client

func init() {
	// Load configuration
	cfg := config.LoadConfig()

	// Initialize the shared HTTP client with configured settings
	SharedHTTPClient = cfg.StreamingHTTPClient()
	logging.NewLogger().DebugLog("Shared HTTP Client initialized with timeout: %v", SharedHTTPClient.Timeout)
}
