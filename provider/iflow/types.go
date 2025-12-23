// Package iflow provides types and structures for the iFlow provider
package iflow

import (
	"time"
)

// Credentials represents the stored OAuth credentials for iFlow
type Credentials struct {
        AuthType       string `json:"auth_type"`        // "oauth" or "cookie"
        AccessToken    string `json:"access_token,omitempty"`
        RefreshToken   string `json:"refresh_token,omitempty"`
        Expire         string `json:"expire,omitempty"`
        ExpiresAt      string `json:"expires_at,omitempty"`
        ExpiryDate     int64  `json:"expiry_date,omitempty"` // Added to match user's file format
        Cookies        string `json:"cookies,omitempty"`
        CookieExpiresAt string `json:"cookie_expires_at,omitempty"`
        Email          string `json:"email,omitempty"`
        UserID         string `json:"user_id,omitempty"`
        LastRefresh    string `json:"last_refresh,omitempty"`
        APIKey         string `json:"apiKey,omitempty"`      // Changed tag to "apiKey"
        TokenType      string `json:"token_type,omitempty"`
        Scope          string `json:"scope,omitempty"`
        Type           string `json:"type"`             // "iflow"
}

// OAuthFileCredentials represents the exact file structure required by the user
type OAuthFileCredentials struct {
        AccessToken  string `json:"access_token"`
        RefreshToken string `json:"refresh_token"`
        ExpiryDate   int64  `json:"expiry_date"`
        TokenType    string `json:"token_type"`
        Scope        string `json:"scope"`
        APIKey       string `json:"apiKey"`
}
// KeyData represents API key information from iFlow
type KeyData struct {
	APIKey     string `json:"api_key"`
	ExpireTime string `json:"expire_time"`
	Name       string `json:"name"`
	HasExpired bool   `json:"has_expired"`
}

// PKCECodes represents PKCE codes for OAuth2 authorization
type PKCECodes struct {
	CodeVerifier  string `json:"code_verifier"`
	CodeChallenge string `json:"code_challenge"`
}

// OAuthCallbackResult represents the result of OAuth callback
type OAuthCallbackResult struct {
	Code  string `json:"code"`
	State string `json:"state"`
	Error string `json:"error,omitempty"`
}

// IsExpired checks if the credentials are expired
func (c *Credentials) IsExpired() bool {
	if c.Expire != "" {
		if expire, err := time.Parse(time.RFC3339, c.Expire); err == nil {
			return expire.Before(time.Now().Add(5 * time.Minute))
		}
	}
	if c.ExpiresAt != "" {
		if expire, err := time.Parse(time.RFC3339, c.ExpiresAt); err == nil {
			return expire.Before(time.Now().Add(5 * time.Minute))
		}
	}
	return true
}

// IsValid checks if the credentials are valid
func (c *Credentials) IsValid() bool {
	if c.AuthType == "oauth" {
		return c.AccessToken != "" && !c.IsExpired()
	}
	if c.AuthType == "cookie" {
		return c.Cookies != "" || c.APIKey != ""
	}
	return false
}

// GetExpire returns the expire time string
func (c *Credentials) GetExpire() string {
	if c.Expire != "" {
		return c.Expire
	}
	return c.ExpiresAt
}