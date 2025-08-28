package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// OAuth constants
const (
	QwenOAuthAuthURL     = "https://chat.qwen.ai/oauth/authorize"
	QwenOAuthTokenURL    = "https://chat.qwen.ai/api/v1/oauth2/token"
	QwenOAuthClientID    = "f0304373b74a44d2b584a3fb70ca9e56"
	QwenOAuthCallbackURL = "http://localhost:8144/oauth/callback"
	QwenOAuthScope       = "openid profile email model.completion"
)

// PKCEParams holds the parameters for PKCE
type PKCEParams struct {
	CodeVerifier  string
	CodeChallenge string
	State         string
}

// OAuthTokenResponse represents the token response from OAuth server
type OAuthTokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	ResourceURL  string `json:"resource_url"`
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

// generateState generates a random state parameter for CSRF protection
func generateState() (string, error) {
	// Generate a random 16-byte string
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	// Encode using base64 URL encoding without padding
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

// generatePKCEParams generates all PKCE parameters needed for OAuth flow
func generatePKCEParams() (*PKCEParams, error) {
	// Generate code verifier
	codeVerifier, err := generateCodeVerifier()
	if err != nil {
		return nil, fmt.Errorf("failed to generate code verifier: %v", err)
	}

	// Generate code challenge from code verifier
	codeChallenge := generateCodeChallenge(codeVerifier)

	// Generate state for CSRF protection
	state, err := generateState()
	if err != nil {
		return nil, fmt.Errorf("failed to generate state: %v", err)
	}

	return &PKCEParams{
		CodeVerifier:  codeVerifier,
		CodeChallenge: codeChallenge,
		State:         state,
	}, nil
}

// buildAuthorizationURL constructs the OAuth authorization URL
func buildAuthorizationURL(pkceParams *PKCEParams) string {
	// Parse the base authorization URL
	authURL, err := url.Parse(QwenOAuthAuthURL)
	if err != nil {
		log.Printf("Error parsing authorization URL: %v", err)
		return ""
	}

	// Add query parameters
	params := url.Values{}
	params.Add("client_id", QwenOAuthClientID)
	params.Add("response_type", "code")
	params.Add("redirect_uri", QwenOAuthCallbackURL)
	params.Add("scope", QwenOAuthScope)
	params.Add("state", pkceParams.State)
	params.Add("code_challenge", pkceParams.CodeChallenge)
	params.Add("code_challenge_method", "S256")

	// Set the query parameters
	authURL.RawQuery = params.Encode()

	return authURL.String()
}

// exchangeCodeForToken exchanges the authorization code for access/refresh tokens
func exchangeCodeForToken(code, codeVerifier string) (*OAuthTokenResponse, error) {
	// Prepare the request body
	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("client_id", QwenOAuthClientID)
	data.Set("code", code)
	data.Set("redirect_uri", QwenOAuthCallbackURL)
	data.Set("code_verifier", codeVerifier)

	// Create the HTTP request
	req, err := http.NewRequest("POST", QwenOAuthTokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create token exchange request: %v", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	// Send the request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send token exchange request: %v", err)
	}
	defer resp.Body.Close()

	// Read the response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read token exchange response: %v", err)
	}

	// Check if the response is successful
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token exchange failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Parse the JSON response
	var tokenResponse OAuthTokenResponse
	if err := json.Unmarshal(body, &tokenResponse); err != nil {
		return nil, fmt.Errorf("failed to parse token response: %v", err)
	}

	return &tokenResponse, nil
}

// startCallbackServer starts a local HTTP server to handle the OAuth callback
func startCallbackServer(pkceParams *PKCEParams) (string, error) {
	// Create a channel to receive the authorization code
	codeChan := make(chan string, 1)
	errorChan := make(chan error, 1)

	// Create the HTTP handler
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if this is the callback URL
		if r.URL.Path != "/oauth/callback" {
			http.NotFound(w, r)
			return
		}

		// Parse query parameters
		query := r.URL.Query()
		code := query.Get("code")
		state := query.Get("state")
		errorParam := query.Get("error")

		// Check if there was an error
		if errorParam != "" {
			errorDesc := query.Get("error_description")
			http.Error(w, fmt.Sprintf("OAuth error: %s - %s", errorParam, errorDesc), http.StatusBadRequest)
			errorChan <- fmt.Errorf("OAuth error: %s - %s", errorParam, errorDesc)
			return
		}

		// Validate state parameter to prevent CSRF
		if state != pkceParams.State {
			http.Error(w, "Invalid state parameter", http.StatusBadRequest)
			errorChan <- fmt.Errorf("invalid state parameter")
			return
		}

		// Validate that we received a code
		if code == "" {
			http.Error(w, "Missing authorization code", http.StatusBadRequest)
			errorChan <- fmt.Errorf("missing authorization code")
			return
		}

		// Send success response to browser
		successHTML := `
<!DOCTYPE html>
<html>
<head>
    <title>Authentication Successful</title>
    <style>
        body { font-family: Arial, sans-serif; text-align: center; padding: 50px; }
        .container { max-width: 500px; margin: 0 auto; }
        .success { color: #28a745; }
    </style>
</head>
<body>
    <div class="container">
        <h1 class="success">Authentication Successful!</h1>
        <p>You have successfully authenticated with Qwen.</p>
        <p>You can now close this window and return to the application.</p>
        <script>
            // Try to close the window after a short delay
            setTimeout(function() {
                window.close();
            }, 2000);
        </script>
    </div>
</body>
</html>
`
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(successHTML))

		// Send the code to the channel
		codeChan <- code
	})

	// Start the HTTP server
	server := &http.Server{
		Addr:    ":8144",
		Handler: handler,
	}

	// Start server in a goroutine
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errorChan <- fmt.Errorf("callback server error: %v", err)
		}
	}()

	// Wait for either a code or an error
	select {
	case code := <-codeChan:
		// Shutdown the server
		server.Close()
		return code, nil
	case err := <-errorChan:
		// Shutdown the server
		server.Close()
		return "", err
	case <-time.After(5 * time.Minute): // Timeout after 5 minutes
		server.Close()
		return "", fmt.Errorf("OAuth callback timeout")
	}
}

// saveCredentials saves the OAuth credentials to the oauth_creds.json file
func saveCredentials(tokenResponse *OAuthTokenResponse) error {
	// Get the path to the credentials file
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get user home directory: %v", err)
	}
	credsPath := filepath.Join(homeDir, ".qwen", "oauth_creds.json")

	// Create the directory if it doesn't exist
	dir := filepath.Dir(credsPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create credentials directory: %v", err)
	}

	// Create the credentials structure
	creds := OAuthCreds{
		AccessToken:  tokenResponse.AccessToken,
		TokenType:    tokenResponse.TokenType,
		RefreshToken: tokenResponse.RefreshToken,
		ResourceURL:  tokenResponse.ResourceURL,
		ExpiryDate:   time.Now().UnixMilli() + tokenResponse.ExpiresIn*1000,
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

// authenticateWithOAuth performs the complete OAuth flow
func authenticateWithOAuth() error {
	// Generate PKCE parameters
	pkceParams, err := generatePKCEParams()
	if err != nil {
		return fmt.Errorf("failed to generate PKCE parameters: %v", err)
	}

	// Build the authorization URL
	authURL := buildAuthorizationURL(pkceParams)
	if authURL == "" {
		return fmt.Errorf("failed to build authorization URL")
	}

	// Print instructions for the user
	fmt.Println("\n=== Qwen OAuth Authentication ===")
	fmt.Println("Opening browser for OAuth authentication...")
	fmt.Println("If the browser does not open, please visit the following URL:")
	fmt.Println()

	// Create a separator for the URL
	separator := strings.Repeat("━", 80)

	fmt.Println(separator)
	fmt.Println("COPY THE ENTIRE URL BELOW (select all text between the lines):")
	fmt.Println(separator)
	fmt.Println(authURL)
	fmt.Println(separator)
	fmt.Println()
	fmt.Println("💡 TIP: Triple-click to select the entire URL, then copy and paste it into your browser.")
	fmt.Println("⚠️  Make sure to copy the COMPLETE URL - it may wrap across multiple lines.")
	fmt.Println()

	// Try to open the browser
	if err := openBrowser(authURL); err != nil {
		log.Printf("Failed to open browser automatically: %v", err)
	}

	// Start the callback server and wait for the authorization code
	code, err := startCallbackServer(pkceParams)
	if err != nil {
		return fmt.Errorf("failed to receive authorization code: %v", err)
	}

	// Exchange the authorization code for tokens
	fmt.Println("\nAuthorization code received, exchanging for tokens...")
	tokenResponse, err := exchangeCodeForToken(code, pkceParams.CodeVerifier)
	if err != nil {
		return fmt.Errorf("failed to exchange code for tokens: %v", err)
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

// authHandler handles the /auth endpoint to initiate the OAuth flow
func authHandler(w http.ResponseWriter, r *http.Request) {
	// Perform the OAuth authentication flow
	if err := authenticateWithOAuth(); err != nil {
		http.Error(w, fmt.Sprintf("OAuth authentication failed: %v", err), http.StatusInternalServerError)
		return
	}

	// Redirect user to the main page or show success message
	fmt.Fprintf(w, `
<!DOCTYPE html>
<html>
<head>
	   <title>Authentication Successful</title>
	   <style>
	       body { font-family: Arial, sans-serif; text-align: center; padding: 50px; }
	       .container { max-width: 500px; margin: 0 auto; }
	       .success { color: #28a745; }
	   </style>
</head>
<body>
	   <div class="container">
	       <h1 class="success">Authentication Successful!</h1>
	       <p>You have successfully authenticated with Qwen.</p>
	       <p>You can now close this window and return to the application.</p>
	       <p><a href="/">Return to the main page</a></p>
	   </div>
</body>
</html>
`)
}

// oauthCallbackHandler handles the OAuth callback
// This is handled by the startCallbackServer function, but we'll add a handler
// for direct access to the callback endpoint
func oauthCallbackHandler(w http.ResponseWriter, r *http.Request) {
	// This endpoint is primarily handled by startCallbackServer
	// If someone accesses it directly, we'll just show a message
	fmt.Fprintf(w, `
<!DOCTYPE html>
<html>
<head>
	   <title>OAuth Callback</title>
	   <style>
	       body { font-family: Arial, sans-serif; text-align: center; padding: 50px; }
	       .container { max-width: 500px; margin: 0 auto; }
	   </style>
</head>
<body>
	   <div class="container">
	       <h1>OAuth Callback</h1>
	       <p>This endpoint is used for OAuth callback processing.</p>
	       <p>If you're seeing this page, the OAuth flow is in progress.</p>
	   </div>
</body>
</html>
`)
}
