// Package auth provides Kiro/AWS SSO authentication
package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sunbankio/qwencoder-proxy/logging"
	"golang.org/x/oauth2"
)

// KiroOAuthConfig holds the OAuth configuration for Kiro
type KiroOAuthConfig struct {
	Region        string
	RefreshURL    string
	RefreshIDCURL string
	BaseURL       string
	CredsPath     string
}

// DefaultKiroOAuthConfig returns the default Kiro OAuth configuration
func DefaultKiroOAuthConfig() *KiroOAuthConfig {
	homeDir, _ := os.UserHomeDir()
	return &KiroOAuthConfig{
		Region:        "us-east-1",
		RefreshURL:    "https://prod.{{region}}.auth.desktop.kiro.dev/refreshToken",
		RefreshIDCURL: "https://oidc.{{region}}.amazonaws.com/token",
		BaseURL:       "https://codewhisperer.{{region}}.amazonaws.com",
		CredsPath:     filepath.Join(homeDir, ".aws", "sso", "cache", "kiro-auth-token.json"),
	}
}

// KiroCredentials represents the stored Kiro credentials
type KiroCredentials struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken,omitempty"`
	ExpiresAt    string `json:"expiresAt,omitempty"`
	ClientID     string `json:"clientId,omitempty"`
	ClientSecret string `json:"clientSecret,omitempty"`
	AuthMethod   string `json:"authMethod,omitempty"`
	Region       string `json:"region,omitempty"`
	ProfileArn   string `json:"profileArn,omitempty"`
}

// KiroAuthenticator implements the Authenticator interface for Kiro
type KiroAuthenticator struct {
	config      *KiroOAuthConfig
	credentials *KiroCredentials
	mu          sync.RWMutex
	logger      *logging.Logger
	httpClient  *http.Client
}

// NewKiroAuthenticator creates a new Kiro authenticator
func NewKiroAuthenticator(config *KiroOAuthConfig) *KiroAuthenticator {
	if config == nil {
		config = DefaultKiroOAuthConfig()
	}
	return &KiroAuthenticator{
		config:     config,
		logger:     logging.NewLogger(),
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// GetCredentialsPath returns the path to the credentials file
func (a *KiroAuthenticator) GetCredentialsPath() string {
	return a.config.CredsPath
}

// IsAuthenticated checks if valid credentials exist
func (a *KiroAuthenticator) IsAuthenticated() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.credentials == nil {
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

	// Check if token is still valid
	if a.credentials.ExpiresAt != "" {
		expiresAt, err := time.Parse(time.RFC3339, a.credentials.ExpiresAt)
		// Check against 30 minute buffer
		buffer := time.Duration(TokenRefreshBufferMs) * time.Millisecond
		if err == nil && expiresAt.Before(time.Now().Add(buffer)) {
			// Considered "not authenticated" (needs refresh) if we strictly check validity here.
			// But IsAuthenticated usually just checks if we have *some* credentials.
			// Let's stick to simple existence + expiry check.
			return false
		}
	}

	return a.credentials != nil && a.credentials.AccessToken != ""
}

// loadCredentials loads credentials from file
func (a *KiroAuthenticator) loadCredentials() (*KiroCredentials, error) {
	credsPath := a.GetCredentialsPath()

	// First try to load the main credentials file
	data, err := os.ReadFile(credsPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read credentials file: %w", err)
	}

	var creds KiroCredentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("failed to parse credentials: %w", err)
	}

	// Also try to load additional credentials from the same directory
	dir := filepath.Dir(credsPath)
	files, err := os.ReadDir(dir)
	if err == nil {
		for _, file := range files {
			if file.IsDir() || !strings.HasSuffix(file.Name(), ".json") {
				continue
			}
			if file.Name() == filepath.Base(credsPath) {
				continue
			}

			filePath := filepath.Join(dir, file.Name())
			fileData, err := os.ReadFile(filePath)
			if err != nil {
				continue
			}

			var additionalCreds KiroCredentials
			if err := json.Unmarshal(fileData, &additionalCreds); err != nil {
				continue
			}

			// Merge additional credentials (client info)
			if additionalCreds.ClientID != "" && creds.ClientID == "" {
				creds.ClientID = additionalCreds.ClientID
			}
			if additionalCreds.ClientSecret != "" && creds.ClientSecret == "" {
				creds.ClientSecret = additionalCreds.ClientSecret
			}
		}
	}

	// Set region from credentials or use default
	if creds.Region == "" {
		creds.Region = a.config.Region
	}

	return &creds, nil
}

// saveCredentials saves credentials to file
func (a *KiroAuthenticator) saveCredentials(creds *KiroCredentials) error {
	credsPath := a.GetCredentialsPath()

	// Create directory if it doesn't exist
	dir := filepath.Dir(credsPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create credentials directory: %w", err)
	}

	// Load existing credentials to merge
	existingData, _ := os.ReadFile(credsPath)
	var existing KiroCredentials
	if len(existingData) > 0 {
		json.Unmarshal(existingData, &existing)
	}

	// Merge new credentials with existing
	if creds.AccessToken != "" {
		existing.AccessToken = creds.AccessToken
	}
	if creds.RefreshToken != "" {
		existing.RefreshToken = creds.RefreshToken
	}
	if creds.ExpiresAt != "" {
		existing.ExpiresAt = creds.ExpiresAt
	}

	data, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal credentials: %w", err)
	}

	// Write file (protected by caller lock usually)
	if err := os.WriteFile(credsPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write credentials file: %w", err)
	}

	return nil
}

// ClearCredentials removes stored credentials
func (a *KiroAuthenticator) ClearCredentials() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.credentials = nil
	// Don't delete the file as it may contain other AWS SSO credentials
	return nil
}

// kiroTokenRefresher implements oauth2.TokenSource for Kiro's custom protocol
type kiroTokenRefresher struct {
	auth *KiroAuthenticator
	ctx  context.Context
}

func (k *kiroTokenRefresher) Token() (*oauth2.Token, error) {
	// Call internal refresh logic
	return k.auth.performRefresh(k.ctx)
}

// GetToken returns a valid access token, refreshing if necessary
func (a *KiroAuthenticator) GetToken(ctx context.Context) (string, error) {
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

	if a.credentials.AccessToken == "" {
		return "", fmt.Errorf("no access token available")
	}

	// Construct oauth2.Token
	var expiry time.Time
	if a.credentials.ExpiresAt != "" {
		if t, err := time.Parse(time.RFC3339, a.credentials.ExpiresAt); err == nil {
			expiry = t
		}
	}

	token := &oauth2.Token{
		AccessToken:  a.credentials.AccessToken,
		RefreshToken: a.credentials.RefreshToken,
		Expiry:       expiry,
		TokenType:    "Bearer",
	}

	// Check buffer (30 mins)
	buffer := time.Duration(TokenRefreshBufferMs) * time.Millisecond
	if time.Until(token.Expiry) < buffer {
		a.logger.InfoLog("[Kiro Auth] Token expiring in less than 30m or expired, forcing refresh")
		token.Expiry = time.Now().Add(-1 * time.Second)
	}

	// Create TokenSource
	ts := oauth2.ReuseTokenSource(token, &kiroTokenRefresher{auth: a, ctx: ctx})

	// Get token (this triggers refresh if expired)
	newToken, err := ts.Token()
	if err != nil {
		a.logger.ErrorLog("[Kiro Auth] Failed to refresh token: %v", err)
		// Continue with existing token if refresh fails??
		// Original code: "Continue with existing token if refresh fails"
		// But if it's expired, we probably shouldn't.
		// Let's return error if we really needed it.
		// But existing logic was lenient.
		// However, oauth2.ReuseTokenSource returns error if refresh fails.
		// We'll return the error.
		return "", err
	}

	// Update credentials if changed
	if newToken.AccessToken != a.credentials.AccessToken || newToken.RefreshToken != a.credentials.RefreshToken {
		a.logger.InfoLog("[Kiro Auth] Token refreshed successfully, saving credentials")
		a.credentials.AccessToken = newToken.AccessToken
		// ReuseTokenSource preserves refresh token if not returned, so it should be safe.
		// But kiroTokenRefresher logic below ensures it's set in the returned token.
		a.credentials.RefreshToken = newToken.RefreshToken
		a.credentials.ExpiresAt = newToken.Expiry.Format(time.RFC3339)

		if err := a.saveCredentials(a.credentials); err != nil {
			a.logger.ErrorLog("Failed to save refreshed credentials: %v", err)
		}
	}

	return a.credentials.AccessToken, nil
}

// performRefresh executes the custom Kiro refresh logic and returns an oauth2.Token
func (a *KiroAuthenticator) performRefresh(ctx context.Context) (*oauth2.Token, error) {
	if a.credentials == nil || a.credentials.RefreshToken == "" {
		return nil, fmt.Errorf("no refresh token available")
	}

	region := a.credentials.Region
	if region == "" {
		region = a.config.Region
	}

	// Determine refresh URL based on auth method
	var refreshURL string
	if a.credentials.AuthMethod == "social" {
		refreshURL = strings.ReplaceAll(a.config.RefreshURL, "{{region}}", region)
	} else {
		refreshURL = strings.ReplaceAll(a.config.RefreshIDCURL, "{{region}}", region)
	}

	// Build request body
	requestBody := map[string]string{
		"refreshToken": a.credentials.RefreshToken,
	}
	if a.credentials.AuthMethod != "social" {
		requestBody["clientId"] = a.credentials.ClientID
		requestBody["clientSecret"] = a.credentials.ClientSecret
		requestBody["grantType"] = "refresh_token"
	}

	bodyBytes, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal refresh request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", refreshURL, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return nil, fmt.Errorf("failed to create refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send refresh request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token refresh failed with status: %d", resp.StatusCode)
	}

	var tokenResp struct {
		AccessToken  string `json:"accessToken"`
		ExpiresIn    int64  `json:"expiresIn"`
		RefreshToken string `json:"refreshToken,omitempty"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("failed to decode refresh response: %w", err)
	}

	// Return as oauth2.Token
	// Ensure we preserve the old refresh token if new one is empty
	refreshToken := tokenResp.RefreshToken
	if refreshToken == "" {
		refreshToken = a.credentials.RefreshToken
	}

	return &oauth2.Token{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: refreshToken,
		Expiry:       time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
		TokenType:    "Bearer",
	}, nil
}

// refreshToken kept for compatibility/internal use but now wrapper calls performRefresh
func (a *KiroAuthenticator) refreshToken(ctx context.Context) error {
	t, err := a.performRefresh(ctx)
	if err != nil {
		return err
	}
	// Update credentials
	a.credentials.AccessToken = t.AccessToken
	a.credentials.RefreshToken = t.RefreshToken
	a.credentials.ExpiresAt = t.Expiry.Format(time.RFC3339)
	return a.saveCredentials(a.credentials)
}

// Authenticate performs the authentication flow
// For Kiro, we expect pre-existing credentials in the AWS SSO cache
func (a *KiroAuthenticator) Authenticate(ctx context.Context) error {
	// Try to load existing credentials
	creds, err := a.loadCredentials()
	if err != nil {
		return fmt.Errorf("Kiro authentication requires pre-existing credentials in %s. Please authenticate with Kiro IDE first", a.GetCredentialsPath())
	}

	a.mu.Lock()
	a.credentials = creds
	a.mu.Unlock()

	// Try to refresh if needed (using new GetToken logic essentially, or just GetToken)
	_, err = a.GetToken(ctx)
	if err != nil {
		a.logger.ErrorLog("[Kiro Auth] Initial token check/refresh failed: %v", err)
	}

	if a.credentials.AccessToken == "" {
		return fmt.Errorf("no valid access token found in credentials")
	}

	a.logger.DebugLog("[Kiro Auth] Authentication successful using existing credentials")
	return nil
}

// GetRegion returns the configured region
func (a *KiroAuthenticator) GetRegion() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.credentials != nil && a.credentials.Region != "" {
		return a.credentials.Region
	}
	return a.config.Region
}

// GetAuthMethod returns the authentication method used
func (a *KiroAuthenticator) GetAuthMethod() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.credentials != nil {
		return a.credentials.AuthMethod
	}
	return ""
}

// GetProfileArn returns the profile ARN if available
func (a *KiroAuthenticator) GetProfileArn() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.credentials != nil {
		return a.credentials.ProfileArn
	}
	return ""
}
