// Package auth provides authentication implementations for various providers
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sunbankio/qwencoder-proxy/logging"
	"golang.org/x/oauth2"
)

const (
	// OAuth constants from reference/iflow.rs
	IFlowAuthURL     = "https://iflow.cn/oauth"
	IFlowTokenURL    = "https://iflow.cn/oauth/token"
	IFlowUserInfoURL = "https://iflow.cn/api/oauth/getUserInfo"
	IFlowAPIKeyURL   = "https://platform.iflow.cn/api/openapi/apikey"
	IFlowClientID    = "10009311001"
	IFlowClientSecret = "4Z3YjXycVsQvyGF1etiNlIBB4RsqSDtW"
	IFlowDefaultPort = 11451
)

// IFlowOAuthConfig holds the OAuth configuration for iFlow
type IFlowOAuthConfig struct {
	ClientID     string
	ClientSecret string
	RedirectPort int
	CredsDir     string
	CredsFile    string
}

// DefaultIFlowOAuthConfig returns the default iFlow OAuth configuration
func DefaultIFlowOAuthConfig() *IFlowOAuthConfig {
	return &IFlowOAuthConfig{
		ClientID:     IFlowClientID,
		ClientSecret: IFlowClientSecret,
		RedirectPort: IFlowDefaultPort,
		CredsDir:     ".iflow",
		CredsFile:    "oauth_creds.json",
	}
}

// IFlowCredentials represents the stored OAuth credentials for iFlow
type IFlowCredentials struct {
	AuthType        string `json:"auth_type"`        // "oauth" or "cookie"
	AccessToken     string `json:"access_token,omitempty"`
	RefreshToken    string `json:"refresh_token,omitempty"`
	Expire          string `json:"expire,omitempty"`
	ExpiresAt       string `json:"expires_at,omitempty"`
	ExpiryDate      int64  `json:"expiry_date,omitempty"` // Added to match user's file format
	Cookies         string `json:"cookies,omitempty"`
	CookieExpiresAt string `json:"cookie_expires_at,omitempty"`
	Email           string `json:"email,omitempty"`
	UserID          string `json:"user_id,omitempty"`
	LastRefresh     string `json:"last_refresh,omitempty"`
	APIKey          string `json:"apiKey,omitempty"` // Changed tag to "apiKey"
	TokenType       string `json:"token_type,omitempty"`
	Scope           string `json:"scope,omitempty"`
	Type            string `json:"type"` // "iflow"
}

// IFlowOAuthFileCredentials represents the exact file structure required by the user
type IFlowOAuthFileCredentials struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiryDate   int64  `json:"expiry_date"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
	APIKey       string `json:"apiKey"`
}

// IFlowPKCECodes represents PKCE codes for OAuth2 authorization
type IFlowPKCECodes struct {
	CodeVerifier  string `json:"code_verifier"`
	CodeChallenge string `json:"code_challenge"`
}

// IFlowOAuthCallbackResult represents the result of OAuth callback
type IFlowOAuthCallbackResult struct {
	Code  string `json:"code"`
	State string `json:"state"`
	Error string `json:"error,omitempty"`
}

// IsExpired checks if the credentials are expired
func (c *IFlowCredentials) IsExpired() bool {
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
func (c *IFlowCredentials) IsValid() bool {
	if c.AuthType == "oauth" {
		return c.AccessToken != "" && !c.IsExpired()
	}
	if c.AuthType == "cookie" {
		return c.Cookies != "" || c.APIKey != ""
	}
	return false
}

// GetExpire returns the expire time string
func (c *IFlowCredentials) GetExpire() string {
	if c.Expire != "" {
		return c.Expire
	}
	return c.ExpiresAt
}

// IFlowAuthenticator implements the auth.Authenticator interface for iFlow
type IFlowAuthenticator struct {
	config      *IFlowOAuthConfig
	credentials *IFlowCredentials
	mu          sync.RWMutex
	logger      *logging.Logger
	httpClient  *http.Client
	tokenSource oauth2.TokenSource
}

// NewIFlowAuthenticator creates a new iFlow authenticator
func NewIFlowAuthenticator(config *IFlowOAuthConfig) *IFlowAuthenticator {
	if config == nil {
		config = DefaultIFlowOAuthConfig()
	}
	return &IFlowAuthenticator{
		config:     config,
		logger:     logging.NewLogger(),
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// Authenticate performs the OAuth authentication flow
func (a *IFlowAuthenticator) Authenticate(ctx context.Context) error {
	// Generate PKCE codes
	pkceCodes, err := a.generatePKCECodes()
	if err != nil {
		return fmt.Errorf("failed to generate PKCE codes: %w", err)
	}

	// Start OAuth server to handle callback
	callbackResult, err := a.waitForCallback()
	if err != nil {
		return fmt.Errorf("OAuth callback failed: %w", err)
	}

	if callbackResult.Error != "" {
		return fmt.Errorf("OAuth error: %s", callbackResult.Error)
	}

	// Exchange authorization code for tokens
	if err := a.exchangeCodeForTokens(callbackResult.Code, pkceCodes); err != nil {
		return fmt.Errorf("failed to exchange code for tokens: %w", err)
	}

	// Fetch user info and API key
	if err := a.fetchUserInfo(); err != nil {
		a.logger.DebugLog("[iFlow] Failed to fetch user info: %v", err)
	}

	return nil
}

// GetToken returns a valid API key for LLM calls, refreshing if necessary
func (a *IFlowAuthenticator) GetToken(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Load credentials if not loaded
	if a.credentials == nil {
		a.loadCredentials()
	}

	// Check if we have credentials
	if a.credentials == nil || a.credentials.AccessToken == "" {
		return "", fmt.Errorf("no valid credentials available")
	}

	// Convert to oauth2.Token
	token := &oauth2.Token{
		AccessToken:  a.credentials.AccessToken,
		RefreshToken: a.credentials.RefreshToken,
		TokenType:    a.credentials.TokenType,
	}

	// Parse expiry if available
	if a.credentials.ExpiresAt != "" {
		if expiry, err := time.Parse(time.RFC3339, a.credentials.ExpiresAt); err == nil {
			token.Expiry = expiry
		}
	}

	// Setup OAuth2 config for iFlow
	conf := &oauth2.Config{
		ClientID:     a.config.ClientID,
		ClientSecret: a.config.ClientSecret,
		Endpoint: oauth2.Endpoint{
			AuthURL:  IFlowAuthURL,
			TokenURL: IFlowTokenURL,
		},
	}

	// Create a context with the custom HTTP client
	oauth2Context := context.WithValue(ctx, oauth2.HTTPClient, a.httpClient)

	// Create TokenSource with the current token
	ts := conf.TokenSource(oauth2Context, token)

	// Get token (this will refresh if needed)
	newToken, err := ts.Token()
	if err != nil {
		a.logger.ErrorLog("[iFlow Auth] Token refresh failed: %v", err)
		return "", fmt.Errorf("failed to refresh token: %w", err)
	}

	// Update credentials if token changed
	if newToken.AccessToken != a.credentials.AccessToken || newToken.RefreshToken != a.credentials.RefreshToken {
		a.logger.InfoLog("[iFlow Auth] Token refreshed successfully, saving credentials")

		a.credentials.AccessToken = newToken.AccessToken
		a.credentials.RefreshToken = newToken.RefreshToken
		a.credentials.TokenType = newToken.TokenType
		if !newToken.Expiry.IsZero() {
			a.credentials.ExpiresAt = newToken.Expiry.Format(time.RFC3339)
			a.credentials.ExpiryDate = newToken.Expiry.UnixMilli()
		}

		// Fetch user info and API key after refresh
		if err := a.fetchUserInfo(); err != nil {
			a.logger.ErrorLog("[iFlow Auth] Failed to fetch user info after refresh: %v", err)
		} else {
			a.logger.InfoLog("[iFlow Auth] User info and API key updated successfully")
		}

		if err := a.saveCredentials(); err != nil {
			a.logger.ErrorLog("Failed to save refreshed credentials: %v", err)
		}
	}

	// If we still don't have an API key, try to fetch it
	if a.credentials.APIKey == "" {
		if err := a.fetchUserInfo(); err != nil {
			a.logger.ErrorLog("[iFlow Auth] Failed to fetch API key: %v", err)
		} else {
			a.saveCredentials()
		}
	}

	// Return the API key for LLM calls, as requested by the user
	if a.credentials.APIKey != "" {
		return a.credentials.APIKey, nil
	}

	// Fallback to AccessToken if APIKey is still not available (should not happen)
	return a.credentials.AccessToken, nil
}

// GetAPIKey returns the stored API key
func (a *IFlowAuthenticator) GetAPIKey() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.credentials == nil {
		return ""
	}
	return a.credentials.APIKey
}

// IsAuthenticated checks if valid credentials exist
func (a *IFlowAuthenticator) IsAuthenticated() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.credentials == nil {
		// Try to load from file
		a.loadCredentials()
		if a.credentials == nil {
			return false
		}
	}

	return a.credentials.IsValid()
}

// GetCredentialsPath returns the path to stored credentials
func (a *IFlowAuthenticator) GetCredentialsPath() string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, a.config.CredsDir, a.config.CredsFile)
}

// ClearCredentials removes stored credentials
func (a *IFlowAuthenticator) ClearCredentials() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	credsPath := a.GetCredentialsPath()
	if err := os.Remove(credsPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove credentials file: %w", err)
	}

	a.credentials = nil
	return nil
}

// loadCredentials loads credentials from file
func (a *IFlowAuthenticator) loadCredentials() {
	credsPath := a.GetCredentialsPath()

	data, err := os.ReadFile(credsPath)
	if err != nil {
		return
	}

	// Try to load as OAuthFileCredentials first (strict user format)
	var fileCreds IFlowOAuthFileCredentials
	if err := json.Unmarshal(data, &fileCreds); err == nil && fileCreds.AccessToken != "" {
		// Map to internal struct
		creds := &IFlowCredentials{
			AuthType:     "oauth",
			Type:         "iflow",
			AccessToken:  fileCreds.AccessToken,
			RefreshToken: fileCreds.RefreshToken,
			TokenType:    fileCreds.TokenType,
			Scope:        fileCreds.Scope,
			APIKey:       fileCreds.APIKey,
			ExpiryDate:   fileCreds.ExpiryDate,
		}

		// Convert expiry date (millis) to RFC3339 string for internal use
		if fileCreds.ExpiryDate > 0 {
			t := time.UnixMilli(fileCreds.ExpiryDate)
			creds.ExpiresAt = t.Format(time.RFC3339)
			creds.Expire = creds.ExpiresAt
		}

		a.credentials = creds

		// If we have access token but no API key, try to fetch user info
		if a.credentials.APIKey == "" && a.credentials.AccessToken != "" {
			if err := a.fetchUserInfo(); err != nil {
				a.logger.DebugLog("[iFlow] Failed to fetch user info during load: %v", err)
			}
		}
		return
	}

	// Fallback to standard loading
	var creds IFlowCredentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return
	}

	a.credentials = &creds

	// Ensure auth_type is set if empty
	if a.credentials.AuthType == "" {
		a.credentials.AuthType = "oauth"
	}
	if a.credentials.Type == "" {
		a.credentials.Type = "iflow"
	}

	// If we have access token but no API key, try to fetch user info
	if a.credentials.APIKey == "" && a.credentials.AccessToken != "" {
		if err := a.fetchUserInfo(); err != nil {
			a.logger.DebugLog("[iFlow] Failed to fetch user info during load: %v", err)
		}
	}
}

// saveCredentials saves credentials to file
func (a *IFlowAuthenticator) saveCredentials() error {
	if a.credentials == nil {
		return fmt.Errorf("no credentials to save")
	}

	credsPath := a.GetCredentialsPath()

	// Create directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(credsPath), 0700); err != nil {
		return fmt.Errorf("failed to create credentials directory: %w", err)
	}

	var data []byte
	var err error

	// Use strict format for OAuth
	if a.credentials.AuthType == "oauth" {
		fileCreds := IFlowOAuthFileCredentials{
			AccessToken:  a.credentials.AccessToken,
			RefreshToken: a.credentials.RefreshToken,
			TokenType:    a.credentials.TokenType,
			Scope:        a.credentials.Scope,
			APIKey:       a.credentials.APIKey,
		}

		// Handle ExpiryDate
		if a.credentials.ExpiryDate > 0 {
			fileCreds.ExpiryDate = a.credentials.ExpiryDate
		} else if a.credentials.ExpiresAt != "" {
			if t, err := time.Parse(time.RFC3339, a.credentials.ExpiresAt); err == nil {
				fileCreds.ExpiryDate = t.UnixMilli()
			}
		}

		data, err = json.MarshalIndent(fileCreds, "", "  ")
	} else {
		// Standard format for others
		data, err = json.MarshalIndent(a.credentials, "", "  ")
	}

	if err != nil {
		return fmt.Errorf("failed to marshal credentials: %w", err)
	}

	if err := os.WriteFile(credsPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write credentials file: %w", err)
	}

	return nil
}

// generatePKCECodes generates PKCE codes for OAuth2
func (a *IFlowAuthenticator) generatePKCECodes() (*IFlowPKCECodes, error) {
	// Generate 96 random bytes for code verifier
	bytes := make([]byte, 96)
	if _, err := rand.Read(bytes); err != nil {
		return nil, fmt.Errorf("failed to generate random bytes: %w", err)
	}
	codeVerifier := base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(bytes)

	// Generate code challenge using S256 method
	hasher := sha256.New()
	hasher.Write([]byte(codeVerifier))
	hash := hasher.Sum(nil)
	codeChallenge := base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(hash)

	return &IFlowPKCECodes{
		CodeVerifier:  codeVerifier,
		CodeChallenge: codeChallenge,
	}, nil
}

// generateState generates a random state string for CSRF protection
func (a *IFlowAuthenticator) generateState() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}
	return base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(bytes), nil
}

// generateAuthURL generates the OAuth authorization URL
func (a *IFlowAuthenticator) generateAuthURL(state string, pkceCodes *IFlowPKCECodes) (string, error) {
	params := url.Values{
		"client_id":             {a.config.ClientID},
		"response_type":         {"code"},
		"redirect_uri":          {a.getRedirectURI()},
		"scope":                 {"openid email profile offline_access"},
		"state":                 {state},
		"code_challenge":        {pkceCodes.CodeChallenge},
		"code_challenge_method": {"S256"},
	}

	return fmt.Sprintf("%s?%s", IFlowAuthURL, params.Encode()), nil
}

// getRedirectURI returns the redirect URI for OAuth callback
func (a *IFlowAuthenticator) getRedirectURI() string {
	return fmt.Sprintf("http://localhost:%d/auth/callback", a.config.RedirectPort)
}

// waitForCallback waits for OAuth callback
func (a *IFlowAuthenticator) waitForCallback() (*IFlowOAuthCallbackResult, error) {
	// For now, we'll implement a simple manual approach
	// In a full implementation, this would start an HTTP server
	// For the scope of this task, we'll instruct the user to manually provide the authorization code

	pkceCodes, err := a.generatePKCECodes()
	if err != nil {
		return nil, fmt.Errorf("failed to generate PKCE codes: %w", err)
	}

	state, err := a.generateState()
	if err != nil {
		return nil, fmt.Errorf("failed to generate state: %w", err)
	}

	authURL, err := a.generateAuthURL(state, pkceCodes)
	if err != nil {
		return nil, fmt.Errorf("failed to generate auth URL: %w", err)
	}

	a.logger.InfoLog("[iFlow] Please open the following URL in your browser:")
	a.logger.InfoLog("[iFlow] %s", authURL)
	a.logger.InfoLog("[iFlow] After authorization, you will be redirected to a page showing the authorization code.")
	a.logger.InfoLog("[iFlow] Please copy the authorization code from the URL parameter 'code' and provide it to continue.")

	// For now, return an error indicating manual intervention is needed
	return nil, fmt.Errorf("manual OAuth flow requires user interaction - please implement full OAuth server for automated flow")
}

// exchangeCodeForTokens exchanges authorization code for tokens
func (a *IFlowAuthenticator) exchangeCodeForTokens(code string, pkceCodes *IFlowPKCECodes) error {
	// Create token request with PKCE
	tokenURL := fmt.Sprintf("%s?grant_type=authorization_code&client_id=%s&client_secret=%s&code=%s&redirect_uri=%s&code_verifier=%s",
		IFlowTokenURL,
		url.QueryEscape(a.config.ClientID),
		url.QueryEscape(a.config.ClientSecret),
		url.QueryEscape(code),
		url.QueryEscape(a.getRedirectURI()),
		url.QueryEscape(pkceCodes.CodeVerifier),
	)

	req, err := http.NewRequest("POST", tokenURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create token request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send token request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("token exchange failed (status %d): %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int64  `json:"expires_in"`
		Scope        string `json:"scope"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return fmt.Errorf("failed to decode token response: %w", err)
	}

	if tokenResp.AccessToken == "" {
		return fmt.Errorf("no access_token in response")
	}

	expiresAt := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)

	a.credentials = &IFlowCredentials{
		AuthType:     "oauth",
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		TokenType:    tokenResp.TokenType,
		Expire:       expiresAt.Format(time.RFC3339),
		ExpiresAt:    expiresAt.Format(time.RFC3339),
		ExpiryDate:   expiresAt.UnixMilli(),
		LastRefresh:  time.Now().Format(time.RFC3339),
		Type:         "iflow",
	}
	// Fetch user info and API key
	if err := a.fetchUserInfo(); err != nil {
		a.logger.DebugLog("[iFlow] Failed to fetch user info: %v", err)
		// Don't fail the exchange, just log the error
	}

	return a.saveCredentials()
}

// fetchUserInfo fetches user information and API key
func (a *IFlowAuthenticator) fetchUserInfo() error {
	if a.credentials == nil || a.credentials.AccessToken == "" {
		return fmt.Errorf("no access token available")
	}

	userInfoURL := fmt.Sprintf("%s?accessToken=%s", IFlowUserInfoURL, url.QueryEscape(a.credentials.AccessToken))

	req, err := http.NewRequest("GET", userInfoURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create user info request: %w", err)
	}

	req.Header.Set("Accept", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send user info request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("user info request failed (status %d): %s", resp.StatusCode, string(body))
	}

	var userInfoResp map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&userInfoResp); err != nil {
		return fmt.Errorf("failed to decode user info response: %w", err)
	}

	if success, ok := userInfoResp["success"].(bool); !ok || !success {
		return fmt.Errorf("user info request unsuccessful")
	}

	data, ok := userInfoResp["data"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("no data in user info response")
	}

	if apiKey, ok := data["apiKey"].(string); ok {
		a.credentials.APIKey = apiKey
	}

	if email, ok := data["email"].(string); ok {
		a.credentials.Email = email
	} else if phone, ok := data["phone"].(string); ok {
		a.credentials.Email = phone
	}

	return a.saveCredentials()
}
