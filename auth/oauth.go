package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
	"golang.org/x/oauth2"
)

// oauthHTTPClient is a dedicated HTTP client for OAuth operations
var oauthHTTPClient *http.Client

// initOAuthHTTPClient initializes the OAuth HTTP client with appropriate settings
func initOAuthHTTPClient() {
	if oauthHTTPClient == nil {
		// Use conservative timeouts for OAuth operations since they are infrequent
		oauthHTTPClient = &http.Client{
			Timeout: 30 * time.Second, // Shorter timeout for auth operations
		}
	}
}

// OAuthCreds represents the structure of the qwenproxy_creds.json file
type OAuthCreds struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	RefreshToken string `json:"refresh_token"`
	ResourceURL  string `json:"resource_url"`
	ExpiryDate   int64  `json:"expiry_date"`
}

// SaveOAuthCreds saves OAuth credentials to the qwenproxy_creds.json file
func SaveOAuthCreds(creds OAuthCreds) error {
	credsPath := GetQwenCredentialsPath()
	lockPath := credsPath + ".lock"

	// Create the directory if it doesn't exist
	dir := filepath.Dir(credsPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create credentials directory: %v", err)
	}

	// Use a file lock to prevent race conditions
	fileLock := flock.New(lockPath)
	locked, err := fileLock.TryLock()
	if err != nil {
		return fmt.Errorf("failed to acquire file lock: %v", err)
	}
	if !locked {
		return fmt.Errorf("failed to acquire file lock, another process is holding it")
	}
	defer fileLock.Unlock()

	// Create or overwrite the file
	file, err := os.Create(credsPath)
	if err != nil {
		return fmt.Errorf("failed to create credentials file: %v", err)
	}
	defer file.Close()

	// Encode and write the credentials
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(creds); err != nil {
		return fmt.Errorf("failed to encode credentials: %v", err)
	}

	return nil
}

// GetQwenCredentialsPath returns the path to the Qwen credentials file
func GetQwenCredentialsPath() string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".qwen", "qwenproxy_creds.json")
}

// LoadQwenCredentials loads the Qwen credentials from the qwenproxy_creds.json file
func LoadQwenCredentials() (OAuthCreds, error) {
	credsPath := GetQwenCredentialsPath()
	file, err := os.Open(credsPath)
	if err != nil {
		return OAuthCreds{}, fmt.Errorf("failed to open credentials file: %v", err)
	}
	defer file.Close()

	var creds OAuthCreds
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&creds); err != nil {
		return OAuthCreds{}, fmt.Errorf("failed to decode credentials file: %v", err)
	}

	return creds, nil
}

// IsTokenValid checks if the token is still valid
func IsTokenValid(credentials OAuthCreds) bool {
	if credentials.ExpiryDate == 0 {
		return false
	}
	// Add 30 second buffer. TokenRefreshBufferMs is defined in constants.go
	return time.Now().UnixMilli() < credentials.ExpiryDate-TokenRefreshBufferMs
}

// RefreshAccessToken refreshes the OAuth token using the refresh token
func RefreshAccessToken(credentials OAuthCreds) (OAuthCreds, error) {
	if credentials.RefreshToken == "" {
		return OAuthCreds{}, fmt.Errorf("no refresh token available")
	}

	conf := &oauth2.Config{
		ClientID: QwenOAuthClientID,
		Endpoint: oauth2.Endpoint{
			TokenURL: QwenOAuthTokenURL,
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	token := &oauth2.Token{
		RefreshToken: credentials.RefreshToken,
	}

	tokenSource := conf.TokenSource(ctx, token)
	newToken, err := tokenSource.Token()
	if err != nil {
		return OAuthCreds{}, fmt.Errorf("failed to refresh token: %w", err)
	}

	updatedCredentials := OAuthCreds{
		AccessToken:  newToken.AccessToken,
		TokenType:    newToken.TokenType,
		RefreshToken: newToken.RefreshToken,
		ExpiryDate:   newToken.Expiry.UnixMilli(),
	}

	if resourceURL, ok := newToken.Extra("resource_url").(string); ok {
		updatedCredentials.ResourceURL = resourceURL
	}

	if err := SaveOAuthCreds(updatedCredentials); err != nil {
		return OAuthCreds{}, fmt.Errorf("failed to save updated credentials: %v", err)
	}

	return updatedCredentials, nil
}

// AuthenticateWithOAuth performs the complete OAuth device authorization flow
func AuthenticateWithOAuth() error {
	return AuthenticateWithDeviceFlow()
}
