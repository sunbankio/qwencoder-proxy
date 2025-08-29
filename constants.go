package main

// Constants
const (
	DefaultQwenBaseURL      = "https://portal.qwen.ai/v1"
	Port                    = "8143"
	TokenRefreshBufferMs    = 1800 * 1000 // 30 minutes
	MaxIdleConns            = 100
	MaxIdleConnsPerHost     = 10
	IdleConnTimeoutSeconds  = 90
	RequestTimeoutSeconds   = 300
	StreamingTimeoutSeconds = 300
)
