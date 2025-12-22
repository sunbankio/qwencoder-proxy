// Package auth provides authentication implementations for various providers
package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sunbankio/qwencoder-proxy/logging"
	"golang.org/x/oauth2"
)

// GeminiOAuthConfig holds the OAuth configuration for Gemini
type GeminiOAuthConfig struct {
	ClientID     string
	ClientSecret string
	Scope        string
	RedirectPort int
	CredsDir     string
	CredsFile    string
}

// DefaultGeminiOAuthConfig returns the default Gemini OAuth configuration
func DefaultGeminiOAuthConfig() *GeminiOAuthConfig {
	return &GeminiOAuthConfig{
		ClientID:     "681255809395-oo8ft2oprdrnp9e3aqf6av3hmdib135j.apps.googleusercontent.com",
		ClientSecret: "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",
		Scope:        "https://www.googleapis.com/auth/cloud-platform",
		RedirectPort: 8085,
		CredsDir:     ".gemini",
		CredsFile:    "oauth_creds.json",
	}
}

// GeminiCredentials represents the stored OAuth credentials
type GeminiCredentials struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiryDate   int64  `json:"expiry_date"`
	Scope        string `json:"scope,omitempty"`
}

// GeminiAuthenticator implements the Authenticator interface for Gemini
type GeminiAuthenticator struct {
	config      *GeminiOAuthConfig
	credentials *GeminiCredentials
	mu          sync.RWMutex
	logger      *logging.Logger
	httpClient  *http.Client
}

// NewGeminiAuthenticator creates a new Gemini authenticator
func NewGeminiAuthenticator(config *GeminiOAuthConfig) *GeminiAuthenticator {
	if config == nil {
		config = DefaultGeminiOAuthConfig()
	}
	return &GeminiAuthenticator{
		config:     config,
		logger:     logging.NewLogger(),
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// GetCredentialsPath returns the path to the credentials file
func (a *GeminiAuthenticator) GetCredentialsPath() string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, a.config.CredsDir, a.config.CredsFile)
}

// IsAuthenticated checks if valid credentials exist
func (a *GeminiAuthenticator) IsAuthenticated() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.credentials == nil {
		// Try to load from file
		creds, err := a.loadCredentials()
		if err != nil {
			return false
		}
		a.mu.RUnlock()
		a.mu.Lock()
		a.credentials = creds
		a.mu.Unlock()
		a.mu.RLock()
	}

	// Check if token is still valid (with 30 minute buffer)
	buffer := time.Duration(TokenRefreshBufferMs) * time.Millisecond
	return a.credentials != nil && time.Unix(a.credentials.ExpiryDate, 0).After(time.Now().Add(buffer))
}

// loadCredentials loads credentials from file
func (a *GeminiAuthenticator) loadCredentials() (*GeminiCredentials, error) {
	credsPath := a.GetCredentialsPath()
	data, err := os.ReadFile(credsPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read credentials file: %w", err)
	}

	var creds GeminiCredentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("failed to parse credentials: %w", err)
	}

	return &creds, nil
}

// saveCredentials saves credentials to file
func (a *GeminiAuthenticator) saveCredentials(creds *GeminiCredentials) error {
	credsPath := a.GetCredentialsPath()

	// Create directory if it doesn't exist
	dir := filepath.Dir(credsPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create credentials directory: %w", err)
	}

	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal credentials: %w", err)
	}

	// Lock file write operation is handled by OS filesystem locking usually,
	// but here we ensure process-level safety via the caller holding a.mu.
	// For added safety we write to temp file and rename.
	// But sticking to os.WriteFile with 0600 is standard.
	if err := os.WriteFile(credsPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write credentials file: %w", err)
	}

	return nil
}

// ClearCredentials removes stored credentials
func (a *GeminiAuthenticator) ClearCredentials() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.credentials = nil
	credsPath := a.GetCredentialsPath()
	if err := os.Remove(credsPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove credentials file: %w", err)
	}
	return nil
}

// GetToken returns a valid access token, refreshing if necessary
func (a *GeminiAuthenticator) GetToken(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Load credentials if not in memory
	if a.credentials == nil {
		creds, err := a.loadCredentials()
		if err != nil {
			return "", fmt.Errorf("credentials not found: %w", err)
		}
		a.credentials = creds
	}

	// Convert to oauth2.Token
	token := &oauth2.Token{
		AccessToken:  a.credentials.AccessToken,
		RefreshToken: a.credentials.RefreshToken,
		TokenType:    a.credentials.TokenType,
		Expiry:       time.Unix(a.credentials.ExpiryDate, 0),
	}

	// 30 minute buffer
	buffer := time.Duration(TokenRefreshBufferMs) * time.Millisecond

	// Setup OAuth2 config
	conf := &oauth2.Config{
		ClientID:     a.config.ClientID,
		ClientSecret: a.config.ClientSecret,
		Scopes:       []string{a.config.Scope},
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://accounts.google.com/o/oauth2/v2/auth",
			TokenURL: "https://oauth2.googleapis.com/token",
		},
	}

	// Create a context with the custom HTTP client
	ctx = context.WithValue(ctx, oauth2.HTTPClient, a.httpClient)

	// Check if we need to force refresh (if within buffer or expired)
	if time.Until(token.Expiry) < buffer {
		a.logger.InfoLog("[Gemini Auth] Token expiring in less than 30m or expired, forcing refresh")
		// Trick ReuseTokenSource by making the token look expired
		token.Expiry = time.Now().Add(-1 * time.Second)
	}

	// Create TokenSource
	ts := conf.TokenSource(ctx, token)

	// Get token (this will refresh if needed/forced)
	newToken, err := ts.Token()
	if err != nil {
		a.logger.ErrorLog("[Gemini Auth] Token refresh failed: %v", err)
		// Clear creds on failure so we retry/reload next time
		a.credentials = nil
		return "", fmt.Errorf("failed to refresh token: %w", err)
	}

	// Check if token changed or was refreshed
	if newToken.AccessToken != a.credentials.AccessToken || newToken.RefreshToken != a.credentials.RefreshToken {
		a.logger.InfoLog("[Gemini Auth] Token refreshed successfully, saving credentials")
		
		a.credentials.AccessToken = newToken.AccessToken
		// ReuseTokenSource ensures RefreshToken is preserved if not returned
		a.credentials.RefreshToken = newToken.RefreshToken 
		a.credentials.TokenType = newToken.TokenType
		a.credentials.ExpiryDate = newToken.Expiry.Unix()
		// Scope might update
		if extraScope, ok := newToken.Extra("scope").(string); ok && extraScope != "" {
			a.credentials.Scope = extraScope
		}

		if err := a.saveCredentials(a.credentials); err != nil {
			a.logger.ErrorLog("Failed to save refreshed credentials: %v", err)
		}
	}

	return a.credentials.AccessToken, nil
}

// ForceRefresh forces a token refresh regardless of expiry
func (a *GeminiAuthenticator) ForceRefresh(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.credentials == nil {
		creds, err := a.loadCredentials()
		if err != nil {
			return fmt.Errorf("credentials not found: %w", err)
		}
		a.credentials = creds
	}

	// Manually expire token to force refresh in GetToken logic, but here we can just call the refresh logic directly.
	// We'll reuse the logic we built in GetToken but tailored for just refreshing.
	// Actually, we can just call GetToken but we need to ensure it refreshes.
	// Since GetToken now has buffer logic, if we want to FORCE it even if valid > 30m?
	// Yes, ForceRefresh implies ignoring validity.
	
	// Temporarily expire the token in memory
	originalExpiry := a.credentials.ExpiryDate
	a.credentials.ExpiryDate = time.Now().Add(-1 * time.Hour).Unix()
	
	// Unlock to call GetToken (which locks) - wait, GetToken locks. 
	// We are holding the lock. We cannot call GetToken.
	// We have to duplicate the refresh logic or refactor.
	// Refactoring: extract refresh logic.
	
	token := &oauth2.Token{
		AccessToken:  a.credentials.AccessToken,
		RefreshToken: a.credentials.RefreshToken,
		TokenType:    a.credentials.TokenType,
		Expiry:       time.Now().Add(-1 * time.Second), // Force expired
	}

	conf := &oauth2.Config{
		ClientID:     a.config.ClientID,
		ClientSecret: a.config.ClientSecret,
		Scopes:       []string{a.config.Scope},
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://accounts.google.com/o/oauth2/v2/auth",
			TokenURL: "https://oauth2.googleapis.com/token",
		},
	}
	ctx = context.WithValue(ctx, oauth2.HTTPClient, a.httpClient)
	ts := conf.TokenSource(ctx, token)
	newToken, err := ts.Token()
	
	if err != nil {
		a.credentials.ExpiryDate = originalExpiry // Restore if failed
		return fmt.Errorf("failed to refresh token: %w", err)
	}

	a.credentials.AccessToken = newToken.AccessToken
	a.credentials.RefreshToken = newToken.RefreshToken
	a.credentials.TokenType = newToken.TokenType
	a.credentials.ExpiryDate = newToken.Expiry.Unix()
	
	if err := a.saveCredentials(a.credentials); err != nil {
		a.logger.ErrorLog("Failed to save refreshed credentials: %v", err)
	}
	
	a.logger.InfoLog("[Gemini Auth] Token forced refresh successful")
	return nil
}

// Authenticate performs the OAuth web flow authentication
func (a *GeminiAuthenticator) Authenticate(ctx context.Context) error {
	// Generate state for CSRF protection
	stateBytes := make([]byte, 16)
	if _, err := rand.Read(stateBytes); err != nil {
		return fmt.Errorf("failed to generate state: %w", err)
	}
	state := base64.URLEncoding.EncodeToString(stateBytes)

	redirectURI := fmt.Sprintf("http://localhost:%d", a.config.RedirectPort)

	// Build authorization URL
	authURL := fmt.Sprintf(
		"https://accounts.google.com/o/oauth2/v2/auth?client_id=%s&redirect_uri=%s&response_type=code&scope=%s&access_type=offline&prompt=consent&state=%s",
		url.QueryEscape(a.config.ClientID),
		url.QueryEscape(redirectURI),
		url.QueryEscape(a.config.Scope),
		url.QueryEscape(state),
	)

	// Channel to receive the authorization code
	codeChan := make(chan string, 1)
	errChan := make(chan error, 1)

	// Start local server to receive callback
	server := &http.Server{Addr: fmt.Sprintf(":%d", a.config.RedirectPort)}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Verify state
		if r.URL.Query().Get("state") != state {
			errChan <- fmt.Errorf("state mismatch")
			http.Error(w, "State mismatch", http.StatusBadRequest)
			return
		}

		code := r.URL.Query().Get("code")
		if code == "" {
			errChan <- fmt.Errorf("no code in callback")
			http.Error(w, "No code received", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html><body><h1>Authorization successful!</h1><p>You can close this window.</p></body></html>"))
		codeChan <- code
	})

	// Start server in goroutine
	go func() {
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			errChan <- err
		}
	}()

	// Print authorization URL for user
	fmt.Printf("\n[Gemini Auth] Please visit the following URL to authorize:\n\n%s\n\n", authURL)
	fmt.Println("[Gemini Auth] Waiting for authorization...")

	// Wait for code or error
	var code string
	select {
	case code = <-codeChan:
		// Got the code
	case err := <-errChan:
		server.Shutdown(ctx)
		return fmt.Errorf("authorization failed: %w", err)
	case <-ctx.Done():
		server.Shutdown(ctx)
		return ctx.Err()
	case <-time.After(5 * time.Minute):
		server.Shutdown(ctx)
		return fmt.Errorf("authorization timeout")
	}

	// Shutdown server
	server.Shutdown(ctx)

	// Exchange code for tokens
	return a.exchangeCodeForTokens(ctx, code, redirectURI)
}

// exchangeCodeForTokens exchanges the authorization code for tokens
func (a *GeminiAuthenticator) exchangeCodeForTokens(ctx context.Context, code, redirectURI string) error {
	data := url.Values{}
	data.Set("client_id", a.config.ClientID)
	data.Set("client_secret", a.config.ClientSecret)
	data.Set("code", code)
	data.Set("grant_type", "authorization_code")
	data.Set("redirect_uri", redirectURI)

	req, err := http.NewRequestWithContext(ctx, "POST", "https://oauth2.googleapis.com/token", strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("failed to create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send token request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("token exchange failed with status: %d", resp.StatusCode)
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
		TokenType    string `json:"token_type"`
		Scope        string `json:"scope"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return fmt.Errorf("failed to decode token response: %w", err)
	}

	a.mu.Lock()
	a.credentials = &GeminiCredentials{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		TokenType:    tokenResp.TokenType,
		ExpiryDate:   time.Now().Unix() + tokenResp.ExpiresIn,
		Scope:        tokenResp.Scope,
	}
	a.mu.Unlock()

	// Save credentials
	if err := a.saveCredentials(a.credentials); err != nil {
		return fmt.Errorf("failed to save credentials: %w", err)
	}

	a.logger.DebugLog("[Gemini Auth] Authentication successful, credentials saved")
	return nil
}
