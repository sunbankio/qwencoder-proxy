package auth

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/sunbankio/qwencoder-proxy/logging"
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

// PKCEParams holds the parameters for PKCE
type PKCEParams struct {
	CodeVerifier  string
	CodeChallenge string
}

// OAuthTokenResponse represents the token response from OAuth server
type OAuthTokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	ResourceURL  string `json:"resource_url"`
}

// DeviceAuthResponse represents the response from the device authorization endpoint
type DeviceAuthResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int64  `json:"expires_in"`
}

// SaveOAuthCreds saves OAuth credentials to the qwenproxy_creds.json file
func SaveOAuthCreds(creds OAuthCreds) error {
	credsPath := GetQwenCredentialsPath()

	// Create the directory if it doesn't exist
	dir := filepath.Dir(credsPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create credentials directory: %v", err)
	}

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
	// Initialize OAuth HTTP client if not already done
	initOAuthHTTPClient()
	
	if credentials.RefreshToken == "" {
		return OAuthCreds{}, fmt.Errorf("no refresh token available")
	}

	// QwenOAuthTokenEndpoint and QwenOAuthClientID are defined in constants.go
	bodyData := map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": credentials.RefreshToken,
		"client_id":     QwenOAuthClientID,
	}

	req, err := http.NewRequest("POST", QwenOAuthTokenURL, bytes.NewBufferString(fmt.Sprintf("grant_type=%s&refresh_token=%s&client_id=%s",
		bodyData["grant_type"], bodyData["refresh_token"], bodyData["client_id"])))
	if err != nil {
		return OAuthCreds{}, fmt.Errorf("failed to create token refresh request: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := oauthHTTPClient.Do(req)
	if err != nil {
		return OAuthCreds{}, fmt.Errorf("token refresh request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode == 400 {
			return OAuthCreds{}, fmt.Errorf("refresh token expired or invalid. Please re-authenticate with Qwen CLI using '/auth'. Response: %s", string(body))
		}
		return OAuthCreds{}, fmt.Errorf("token refresh failed: %d %s. Response: %s", resp.StatusCode, resp.Status, string(body))
	}

	var tokenData map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&tokenData); err != nil {
		return OAuthCreds{}, fmt.Errorf("failed to decode token response: %v", err)
	}

	if errorMsg, ok := tokenData["error"]; ok {
		return OAuthCreds{}, fmt.Errorf("token refresh failed: %v - %v", errorMsg, tokenData["error_description"])
	}

	// Update credentials with new token
	expiresIn, _ := tokenData["expires_in"].(float64)
	updatedCredentials := OAuthCreds{
		AccessToken:  tokenData["access_token"].(string),
		TokenType:    tokenData["token_type"].(string),
		RefreshToken: tokenData["refresh_token"].(string),
		ResourceURL:  tokenData["resource_url"].(string),
		ExpiryDate:   time.Now().UnixMilli() + int64(expiresIn*1000),
	}

	// Save updated credentials using the shared function
	if err := SaveOAuthCreds(updatedCredentials); err != nil {
		return OAuthCreds{}, fmt.Errorf("failed to save updated credentials: %v", err)
	}

	return updatedCredentials, nil
}

// generateCodeVerifier generates a random code verifier for PKCE
func generateCodeVerifier() (string, error) {
	// Generate a random 32-byte string
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	// Encode using base64 URL encoding without padding
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

// generateCodeChallenge generates a code challenge from a code verifier using SHA-256
func generateCodeChallenge(codeVerifier string) string {
	// Hash the code verifier with SHA-256
	hasher := sha256.New()
	hasher.Write([]byte(codeVerifier))
	// Encode using base64 URL encoding without padding
	return base64.RawURLEncoding.EncodeToString(hasher.Sum(nil))
}

// generatePKCEParams generates PKCE parameters (code verifier and code challenge)
func generatePKCEParams() (*PKCEParams, error) {
	codeVerifier, err := generateCodeVerifier()
	if err != nil {
		return nil, fmt.Errorf("failed to generate code verifier: %v", err)
	}
	codeChallenge := generateCodeChallenge(codeVerifier)
	return &PKCEParams{
		CodeVerifier:  codeVerifier,
		CodeChallenge: codeChallenge,
	}, nil
}

// initiateDeviceAuth initiates the OAuth 2.0 Device Authorization Flow
func initiateDeviceAuth(pkceParams *PKCEParams) (*DeviceAuthResponse, error) {
	// Initialize OAuth HTTP client if not already done
	initOAuthHTTPClient()

	// Device authorization endpoint
	deviceAuthURL := "https://chat.qwen.ai/api/v1/oauth2/device/code"

	// Prepare the request body
	data := url.Values{}
	data.Set("client_id", QwenOAuthClientID)
	data.Set("scope", QwenOAuthScope)
	data.Set("code_challenge", pkceParams.CodeChallenge)
	data.Set("code_challenge_method", "S256")

	// Create the HTTP request
	req, err := http.NewRequest("POST", deviceAuthURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create device auth request: %v", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	// Send the request
	resp, err := oauthHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send device auth request: %v", err)
	}
	defer resp.Body.Close()

	// Read the response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read device auth response: %v", err)
	}

	// Check if the response is successful
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("device auth request failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Parse the JSON response
	var deviceAuthResponse DeviceAuthResponse
	if err := json.Unmarshal(body, &deviceAuthResponse); err != nil {
		return nil, fmt.Errorf("failed to parse device auth response: %v", err)
	}

	return &deviceAuthResponse, nil
}

// exchangeDeviceCodeForToken exchanges the device code for access/refresh tokens
func exchangeDeviceCodeForToken(deviceCode, codeVerifier string) (*OAuthTokenResponse, error) {
	// Initialize OAuth HTTP client if not already done
	initOAuthHTTPClient()
	
	// Prepare the request body
	data := url.Values{}
	data.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
	data.Set("client_id", QwenOAuthClientID)
	data.Set("device_code", deviceCode)
	data.Set("code_verifier", codeVerifier)

	// Create the HTTP request
	req, err := http.NewRequest("POST", QwenOAuthTokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create device token exchange request: %v", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	// Send the request
	resp, err := oauthHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send device token exchange request: %v", err)
	}
	defer resp.Body.Close()

	// Read the response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read device token exchange response: %v", err)
	}

	// Check if the response is successful
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("device token exchange failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Parse the JSON response
	var tokenResponse OAuthTokenResponse
	if err := json.Unmarshal(body, &tokenResponse); err != nil {
		return nil, fmt.Errorf("failed to parse device token response: %v", err)
	}

	return &tokenResponse, nil
}

// saveCredentials saves the OAuth credentials to the qwenproxy_creds.json file
func saveCredentials(tokenResponse *OAuthTokenResponse) error {
	// Create the credentials structure
	creds := OAuthCreds{
		AccessToken:  tokenResponse.AccessToken,
		TokenType:    tokenResponse.TokenType,
		RefreshToken: tokenResponse.RefreshToken,
		ResourceURL:  tokenResponse.ResourceURL,
		ExpiryDate:   time.Now().UnixMilli() + tokenResponse.ExpiresIn*1000,
	}

	// Save credentials using the shared function
	return SaveOAuthCreds(creds)
}

// PollForToken polls the token endpoint with the device code until successful or timeout
func PollForToken(deviceCode, codeVerifier string) (*OAuthTokenResponse, error) {
	// Polling interval in seconds
	pollInterval := 5 * time.Second

	// Maximum number of attempts (30 minutes with 5 second intervals)
	maxAttempts := 360

	// Poll for the token
	for attempt := 0; attempt < maxAttempts; attempt++ {
		// Try to exchange the device code for tokens
		tokenResponse, err := exchangeDeviceCodeForToken(deviceCode, codeVerifier)
		if err != nil {
			// Check if the error is a pending authorization error
			// This is expected while the user hasn't authorized yet
			if strings.Contains(err.Error(), "authorization_pending") {
				// Wait before polling again
				time.Sleep(pollInterval)
				continue
			}

			// Check if the error is a slow down error
			// In this case, we should increase our polling interval
			if strings.Contains(err.Error(), "slow_down") {
				// Double the polling interval
				pollInterval *= 2
				time.Sleep(pollInterval)
				continue
			}

			// For any other error, return it
			return nil, fmt.Errorf("failed to exchange device code for token: %v", err)
		}

		// If we got a successful response, return it
		return tokenResponse, nil
	}

	// If we've exhausted our attempts, return a timeout error
	return nil, fmt.Errorf("timeout waiting for device authorization")
}

// AuthenticateWithOAuth performs the complete OAuth device authorization flow
func AuthenticateWithOAuth() error {
	// Generate PKCE parameters once at the beginning
	pkceParams, err := generatePKCEParams()
	if err != nil {
		return fmt.Errorf("failed to generate PKCE parameters: %v", err)
	}

	// Request device authorization, passing the generated PKCE parameters
	deviceAuthResponse, err := initiateDeviceAuth(pkceParams)
	if err != nil {
		return fmt.Errorf("failed to initiate device authorization: %v", err)
	}

	// Construct verification URL with user code and client parameter
	// Use "qwen-code" as the client parameter value
	var verificationURL string
	if deviceAuthResponse.VerificationURIComplete != "" {
		verificationURL = deviceAuthResponse.VerificationURIComplete
	} else {
		verificationURL = fmt.Sprintf("%s?user_code=%s&client=qwen-code", deviceAuthResponse.VerificationURI, deviceAuthResponse.UserCode)
	}

	// Display the user code and verification URI to the user
	fmt.Println("\n=== Qwen OAuth Authentication ===")
	fmt.Printf("User Code: %s\n", deviceAuthResponse.UserCode)
	fmt.Printf("Verification URI: %s\n", verificationURL)
	fmt.Println()
	fmt.Println("Please visit the Verification URI in your browser and enter the User Code.")
	fmt.Println("Waiting for authorization...")

	// Try to open the verification URI in the browser
	if err := openBrowser(verificationURL); err != nil {
		logging.NewLogger().WarningLog("Failed to open browser automatically: %v", err)
		fmt.Println("Please manually open the Verification URI in your browser.")
	}

	// Poll for the token
	tokenResponse, err := PollForToken(deviceAuthResponse.DeviceCode, pkceParams.CodeVerifier)
	if err != nil {
		return fmt.Errorf("failed to poll for token: %v", err)
	}

	// Save the credentials
	if err := saveCredentials(tokenResponse); err != nil {
		return fmt.Errorf("failed to save credentials: %v", err)
	}

	fmt.Println("Authentication successful! Credentials saved.")
	return nil
}

// openBrowser opens the default browser with the given URL
func openBrowser(url string) error {
	var err error

	switch runtime.GOOS {
	case "linux":
		err = exec.Command("xdg-open", url).Start()
	case "windows":
		err = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		err = exec.Command("open", url).Start()
	default:
		err = fmt.Errorf("unsupported platform")
	}

	return err
}
