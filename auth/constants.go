package auth

// Constants
const (
	DefaultQwenBaseURL      = "https://portal.qwen.ai/v1"
	TokenRefreshBufferMs    = 1800 * 1000 // 30 minutes
	QwenOAuthTokenURL       = "https://chat.qwen.ai/api/v1/oauth2/token"
	QwenOAuthClientID       = "f0304373b74a44d2b584a3fb70ca9e56"
	QwenOAuthScope          = "openid profile email model.completion"
)
