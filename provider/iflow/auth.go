// Package iflow provides authentication for the iFlow provider
package iflow

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
	"strings"
	"sync"
	"time"

	"github.com/sunbankio/qwencoder-proxy/logging"
)

// OAuthConfig holds the OAuth configuration for iFlow
type OAuthConfig struct {
	ClientID     string
	ClientSecret string
	RedirectPort int
	CredsDir     string
	CredsFile    string
}

// DefaultOAuthConfig returns the default iFlow OAuth configuration
func DefaultOAuthConfig() *OAuthConfig {
	return &OAuthConfig{
		ClientID:     ClientID,
		ClientSecret: ClientSecret,
		RedirectPort: DefaultPort,
		CredsDir:     ".iflow",
		CredsFile:    "oauth_creds.json",
	}
}

// Authenticator implements the auth.Authenticator interface for iFlow
type Authenticator struct {
	config      *OAuthConfig
	credentials *Credentials
	mu          sync.RWMutex
	logger      *logging.Logger
	httpClient  *http.Client
}

// NewAuthenticator creates a new iFlow authenticator
func NewAuthenticator(config *OAuthConfig) *Authenticator {
	if config == nil {
		config = DefaultOAuthConfig()
	}
	return &Authenticator{
		config:     config,
		logger:     logging.NewLogger(),
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// Authenticate performs the OAuth authentication flow
func (a *Authenticator) Authenticate(ctx context.Context) error {
	// Generate PKCE codes
	pkceCodes, err := a.generatePKCECodes()
	if err != nil {
		return fmt.Errorf("failed to generate PKCE codes: %w", err)
	}

	// Start OAuth server to handle callback
	callbackResult, err := a.waitForCallback(ctx, 2*time.Minute)
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

// GetToken returns a valid access token, refreshing if necessary
func (a *Authenticator) GetToken(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Load credentials if not loaded
	if a.credentials == nil {
		a.loadCredentials()
	}

	// Check if credentials are valid
	if !a.credentials.IsValid() {
		if a.credentials.RefreshToken != "" {
			// Try to refresh token
			if err := a.refreshToken(); err != nil {
				return "", fmt.Errorf("failed to refresh token: %w", err)
			}
		} else {
			return "", fmt.Errorf("no valid credentials available")
		}
	}

	return a.credentials.AccessToken, nil
}

// IsAuthenticated checks if valid credentials exist
func (a *Authenticator) IsAuthenticated() bool {
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
func (a *Authenticator) GetCredentialsPath() string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, a.config.CredsDir, a.config.CredsFile)
}

// ClearCredentials removes stored credentials
func (a *Authenticator) ClearCredentials() error {
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
func (a *Authenticator) loadCredentials() {
	credsPath := a.GetCredentialsPath()
	
	data, err := os.ReadFile(credsPath)
	if err != nil {
		return
	}

	var creds Credentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return
	}

	a.credentials = &creds
}

// saveCredentials saves credentials to file
func (a *Authenticator) saveCredentials() error {
	if a.credentials == nil {
		return fmt.Errorf("no credentials to save")
	}

	credsPath := a.GetCredentialsPath()
	
	// Create directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(credsPath), 0700); err != nil {
		return fmt.Errorf("failed to create credentials directory: %w", err)
	}

	data, err := json.MarshalIndent(a.credentials, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal credentials: %w", err)
	}

	if err := os.WriteFile(credsPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write credentials file: %w", err)
	}

	return nil
}

// generatePKCECodes generates PKCE codes for OAuth2
func (a *Authenticator) generatePKCECodes() (*PKCECodes, error) {
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

	return &PKCECodes{
		CodeVerifier:  codeVerifier,
		CodeChallenge: codeChallenge,
	}, nil
}

// generateState generates a random state string for CSRF protection
func (a *Authenticator) generateState() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}
	return base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(bytes), nil
}

// generateAuthURL generates the OAuth authorization URL
func (a *Authenticator) generateAuthURL(state string, pkceCodes *PKCECodes) (string, error) {
	params := url.Values{
		"client_id":             {a.config.ClientID},
		"response_type":         {"code"},
		"redirect_uri":          {a.getRedirectURI()},
		"scope":                 {"openid email profile offline_access"},
		"state":                 {state},
		"code_challenge":        {pkceCodes.CodeChallenge},
		"code_challenge_method": {"S256"},
	}

	return fmt.Sprintf("%s?%s", AuthURL, params.Encode()), nil
}

// getRedirectURI returns the redirect URI for OAuth callback
func (a *Authenticator) getRedirectURI() string {
	return fmt.Sprintf("http://localhost:%d/auth/callback", a.config.RedirectPort)
}

// waitForCallback waits for OAuth callback
func (a *Authenticator) waitForCallback(ctx context.Context, timeout time.Duration) (*OAuthCallbackResult, error) {
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
func (a *Authenticator) exchangeCodeForTokens(code string, pkceCodes *PKCECodes) error {
	params := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {a.config.ClientID},
		"code":          {code},
		"redirect_uri":  {a.getRedirectURI()},
		"code_verifier": {pkceCodes.CodeVerifier},
	}

	req, err := http.NewRequest("POST", TokenURL, strings.NewReader(params.Encode()))
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

	var tokenResp map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return fmt.Errorf("failed to decode token response: %w", err)
	}

	accessToken, ok := tokenResp["access_token"].(string)
	if !ok {
		return fmt.Errorf("no access_token in response")
	}

	refreshToken, _ := tokenResp["refresh_token"].(string)
	expiresIn, _ := tokenResp["expires_in"].(float64)
	email, _ := tokenResp["email"].(string)
	userID, _ := tokenResp["user_id"].(string)

	expiresAt := time.Now().Add(time.Duration(expiresIn) * time.Second)

	a.credentials = &Credentials{
		AuthType:     "oauth",
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		Expire:       expiresAt.Format(time.RFC3339),
		ExpiresAt:    expiresAt.Format(time.RFC3339),
		Email:        email,
		UserID:       userID,
		LastRefresh:  time.Now().Format(time.RFC3339),
		Type:         "iflow",
	}

	return a.saveCredentials()
}

// refreshToken refreshes the access token using refresh token
func (a *Authenticator) refreshToken() error {
	if a.credentials == nil || a.credentials.RefreshToken == "" {
		return fmt.Errorf("no refresh token available")
	}

	// Build Basic Auth header
	basicAuth := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", a.config.ClientID, a.config.ClientSecret)))

	params := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {a.credentials.RefreshToken},
		"client_id":     {a.config.ClientID},
		"client_secret": {a.config.ClientSecret},
	}

	req, err := http.NewRequest("POST", TokenURL, strings.NewReader(params.Encode()))
	if err != nil {
		return fmt.Errorf("failed to create refresh request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Basic %s", basicAuth))

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send refresh request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("token refresh failed (status %d): %s", resp.StatusCode, string(body))
	}

	var tokenResp map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return fmt.Errorf("failed to decode refresh response: %w", err)
	}

	accessToken, ok := tokenResp["access_token"].(string)
	if !ok {
		return fmt.Errorf("no access_token in refresh response")
	}

	a.credentials.AccessToken = accessToken

	if rt, ok := tokenResp["refresh_token"].(string); ok {
		a.credentials.RefreshToken = rt
	}

	if tokenType, ok := tokenResp["token_type"].(string); ok {
		a.credentials.TokenType = tokenType
	}

	if scope, ok := tokenResp["scope"].(string); ok {
		a.credentials.Scope = scope
	}

	expiresIn, _ := tokenResp["expires_in"].(float64)
	expiresAt := time.Now().Add(time.Duration(expiresIn) * time.Second)
	a.credentials.Expire = expiresAt.Format(time.RFC3339)
	a.credentials.ExpiresAt = expiresAt.Format(time.RFC3339)
	a.credentials.LastRefresh = time.Now().Format(time.RFC3339)

	return a.saveCredentials()
}

// fetchUserInfo fetches user information and API key
func (a *Authenticator) fetchUserInfo() error {
	if a.credentials == nil || a.credentials.AccessToken == "" {
		return fmt.Errorf("no access token available")
	}

	userInfoURL := fmt.Sprintf("%s?accessToken=%s", UserInfoURL, url.QueryEscape(a.credentials.AccessToken))

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