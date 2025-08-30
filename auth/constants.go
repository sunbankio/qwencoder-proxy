package auth

import (
	"net/http"
	"time"
)

// Constants
const (
	DefaultQwenBaseURL      = "https://portal.qwen.ai/v1"
	Port                    = "8143"
	TokenRefreshBufferMs    = 1800 * 1000 // 30 minutes
	MaxIdleConns            = 50          // Maximum number of idle connections across all hosts
	MaxIdleConnsPerHost     = 50          // Maximum number of idle connections per host
	IdleConnTimeoutSeconds  = 180         // Idle connection timeout
	RequestTimeoutSeconds   = 300         // Request timeout
	StreamingTimeoutSeconds = 300         // Streaming timeout
	ReadTimeoutSeconds      = 45          // Read timeout
	QwenOAuthTokenURL       = "https://chat.qwen.ai/api/v1/oauth2/token"
	QwenOAuthClientID       = "f0304373b74a44d2b584a3fb70ca9e56"
	QwenOAuthScope          = "openid profile email model.completion"
)

// Shared HTTP client with connection pooling and timeouts
var SharedHTTPClient *http.Client

func init() {
	transport := &http.Transport{
		MaxIdleConns:        MaxIdleConns,
		MaxIdleConnsPerHost: MaxIdleConnsPerHost,
		IdleConnTimeout:     IdleConnTimeoutSeconds * time.Second,
	}

	SharedHTTPClient = &http.Client{
		Timeout:   RequestTimeoutSeconds * time.Second,
		Transport: transport,
	}
}