package iflow

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

// TestRealIFlowAuthFlow tests the complete iFlow authentication flow with real endpoints
func TestRealIFlowAuthFlow(t *testing.T) {
	t.Log("=== DEMONSTRATION: iFlow Authentication Flow ===")
	t.Log("This test demonstrates loading credentials, refreshing tokens, and retrieving the API Key.")

	// Create authenticator using default config (~/.iflow/oauth_creds.json)
	auth := NewAuthenticator(nil)

	// Step 1: Check initial status
	if !auth.IsAuthenticated() {
		t.Fatal("Initial authentication failed. Please ensure ~/.iflow/oauth_creds.json exists and is valid.")
	}

	auth.mu.RLock()
	initialCreds := *auth.credentials
	auth.mu.RUnlock()

	t.Logf("Step 1: Initial Credentials Loaded")
	t.Logf("  - Email: %s", initialCreds.Email)
	t.Logf("  - Has Access Token: %t", initialCreds.AccessToken != "")
	t.Logf("  - Current API Key: %s...", initialCreds.APIKey[:min(10, len(initialCreds.APIKey))])
	t.Logf("  - Expires At: %s", initialCreds.ExpiresAt)

	// Step 2: Force Token Refresh
	t.Log("\nStep 2: Forcing Token Refresh...")
	auth.mu.Lock()
	// Mock expiry to trigger refresh in GetToken
	auth.credentials.ExpiresAt = time.Now().Add(-time.Hour).Format(time.RFC3339)
	auth.mu.Unlock()

	ctx := context.Background()
	tokenForLLM, err := auth.GetToken(ctx)
	if err != nil {
		t.Fatalf("Token refresh/retrieval failed: %v", err)
	}

	auth.mu.RLock()
	refreshedCreds := *auth.credentials
	auth.mu.RUnlock()

	t.Logf("  ✓ Refresh completed successfully")
	t.Logf("  - New Access Token: %s...", refreshedCreds.AccessToken[:10])
	t.Logf("  - New Expires At: %s", refreshedCreds.ExpiresAt)

	// Step 3: Verify API Key Retrieval
	t.Log("\nStep 3: Verifying API Key Retrieval...")
	if refreshedCreds.APIKey == "" {
		t.Error("FAILED: API Key was not retrieved after refresh")
	} else {
		t.Logf("  ✓ API Key retrieved: %s...", refreshedCreds.APIKey[:min(15, len(refreshedCreds.APIKey))])
		if refreshedCreds.APIKey == initialCreds.APIKey {
			t.Log("  - API Key remained consistent")
		} else {
			t.Log("  - API Key was updated")
		}
	}

	// Step 4: Demonstrate GetToken Result
	t.Log("\nStep 4: Demonstrating GetToken() result for LLM calls")
	t.Logf("  GetToken() returned: %s...", tokenForLLM[:min(15, len(tokenForLLM))])
	if strings.HasPrefix(tokenForLLM, "sk-") {
		t.Log("  ✓ Correct: GetToken() returned the API Key (starts with 'sk-')")
	} else {
		t.Errorf("  FAILED: GetToken() should return the API Key, but returned: %s", tokenForLLM)
	}

	t.Log("\n=== Demonstration Finished Successfully ===")
}

// TestOAuthFlowAPIKeyExtraction tests the complete OAuth flow with API key extraction
// This test demonstrates the exact flow from iflow.rs for getting API keys from user data
func TestOAuthFlowAPIKeyExtraction(t *testing.T) {
	t.Log("=== Testing OAuth Flow API Key Extraction ===")

	// Load existing credentials to get a valid access token
	credsPath := filepath.Join(os.Getenv("HOME"), ".iflow", "oauth_creds.json")

	credsData, err := os.ReadFile(credsPath)
	if err != nil {
		t.Skipf("No credentials file found at %s, skipping test", credsPath)
		return
	}

	var creds Credentials
	if err := json.Unmarshal(credsData, &creds); err != nil {
		t.Fatalf("Failed to parse credentials: %v", err)
	}

	if creds.AccessToken == "" {
		t.Fatal("No access token available in credentials")
	}

	t.Logf("Using access token for user: %s", creds.Email)

	// Step 1: Test the exact user info endpoint as used in iflow.rs
	t.Log("\n--- Step 1: Fetch User Info with API Key (matching iflow.rs implementation) ---")

	userInfoURL := fmt.Sprintf("%s?accessToken=%s", UserInfoURL, url.QueryEscape(creds.AccessToken))

	t.Logf("User Info URL: %s", userInfoURL)

	// Create request exactly as in iflow.rs fetch_user_info
	req, err := http.NewRequest("GET", userInfoURL, nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	req.Header.Set("Accept", "application/json")

	// Log request details (matching iflow.rs logging style)
	t.Logf("User Info Request:")
	t.Logf("  Method: %s", req.Method)
	t.Logf("  URL: %s", req.URL.String())
	t.Logf("  Headers:")
	for key, values := range req.Header {
		for _, value := range values {
			t.Logf("    %s: %s", key, value)
		}
	}

	// Make request
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	// Log response details
	t.Logf("User Info Response:")
	t.Logf("  Status Code: %d", resp.StatusCode)
	t.Logf("  Headers:")
	for key, values := range resp.Header {
		for _, value := range values {
			t.Logf("    %s: %s", key, value)
		}
	}
	t.Logf("  Body: %s", string(body))

	// Parse response as in iflow.rs
	var userInfoResp map[string]interface{}
	if err := json.Unmarshal(body, &userInfoResp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Check success field (matching iflow.rs logic)
	if success, ok := userInfoResp["success"].(bool); !ok || !success {
		t.Errorf("User info request unsuccessful: %+v", userInfoResp)
		return
	}

	// Extract data field (matching iflow.rs logic)
	data, ok := userInfoResp["data"].(map[string]interface{})
	if !ok {
		t.Fatal("No data field in user info response")
	}

	t.Logf("✓ User info retrieved successfully")

	// Step 2: Extract API key exactly as in iflow.rs
	t.Log("\n--- Step 2: Extract API Key from User Data ---")

	var extractedAPIKey string
	var extractedEmail string

	// Extract API key (matching iflow.rs: user_info.get("apiKey").and_then(|v| v.as_str()))
	if apiKeyValue, exists := data["apiKey"]; exists {
		if apiKey, ok := apiKeyValue.(string); ok {
			extractedAPIKey = apiKey
			t.Logf("✓ API Key extracted: %s...", extractedAPIKey[:20])
		} else {
			t.Errorf("API key field is not a string: %v", apiKeyValue)
		}
	} else {
		t.Error("No apiKey field found in user data")
	}

	// Extract email (matching iflow.rs logic)
	if emailValue, exists := data["email"]; exists {
		if email, ok := emailValue.(string); ok && email != "" {
			extractedEmail = email
			t.Logf("✓ Email extracted: %s", extractedEmail)
		}
	}

	// If no email, try phone (matching iflow.rs logic)
	if extractedEmail == "" {
		if phoneValue, exists := data["phone"]; exists {
			if phone, ok := phoneValue.(string); ok && phone != "" {
				extractedEmail = phone
				t.Logf("✓ Phone extracted as email: %s", extractedEmail)
			}
		}
	}

	// Log all extracted fields for debugging
	t.Logf("All extracted user data fields:")
	for key, value := range data {
		if key == "apiKey" {
			if strValue, ok := value.(string); ok {
				t.Logf("  %s: %s...", key, strValue[:min(20, len(strValue))])
			} else {
				t.Logf("  %s: %v", key, value)
			}
		} else {
			t.Logf("  %s: %v", key, value)
		}
	}

	// Step 3: Verify and update credentials
	t.Log("\n--- Step 3: Verify and Update Credentials ---")

	// Compare with existing credentials
	if creds.APIKey != "" {
		if creds.APIKey == extractedAPIKey {
			t.Logf("✓ Extracted API key matches existing credential")
		} else {
			t.Logf("⚠ API key differs from existing credential")
			t.Logf("  Existing: %s...", creds.APIKey[:20])
			t.Logf("  Extracted: %s...", extractedAPIKey[:20])
		}
	}

	if extractedEmail != "" && creds.Email != "" {
		if creds.Email == extractedEmail {
			t.Logf("✓ Extracted email matches existing credential")
		} else {
			t.Logf("⚠ Email differs from existing credential")
			t.Logf("  Existing: %s", creds.Email)
			t.Logf("  Extracted: %s", extractedEmail)
		}
	}

	// Update credentials with extracted data (matching iflow.rs logic)
	if extractedAPIKey != "" {
		creds.APIKey = extractedAPIKey
		t.Logf("✓ Updated credentials with new API key")
	}

	if extractedEmail != "" {
		creds.Email = extractedEmail
		t.Logf("✓ Updated credentials with new email")
	}

	creds.LastRefresh = time.Now().Format(time.RFC3339)

	// Save updated credentials
	updatedData, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal updated credentials: %v", err)
	}

	if err := os.WriteFile(credsPath, updatedData, 0600); err != nil {
		t.Fatalf("Failed to save updated credentials: %v", err)
	}

	t.Logf("✓ Credentials updated and saved")

	// Step 4: Test the complete flow with token refresh
	t.Log("\n--- Step 4: Test Complete Flow with Token Refresh ---")

	// Force token expiry to trigger refresh
	auth := NewAuthenticator(nil)
	auth.credentials = &creds

	// Mock expiry by setting past time
	auth.credentials.ExpiresAt = time.Now().Add(-time.Hour).Format(time.RFC3339)

	// Get token (should trigger refresh and API key extraction)
	ctx := context.Background()
	newToken, err := auth.GetToken(ctx)
	if err != nil {
		t.Fatalf("Failed to get/refresh token: %v", err)
	}

	t.Logf("✓ Token refresh completed: %s...", newToken[:20])

	// Verify API key was updated during refresh
	auth.mu.RLock()
	refreshedCreds := auth.credentials
	auth.mu.RUnlock()

	if refreshedCreds.APIKey != extractedAPIKey {
		t.Logf("⚠ API key changed during refresh")
		t.Logf("  Before: %s...", extractedAPIKey[:20])
		t.Logf("  After: %s...", refreshedCreds.APIKey[:20])
	} else {
		t.Logf("✓ API key remained consistent during refresh")
	}

	t.Log("\n=== OAuth Flow API Key Extraction Test Finished ===")
}

// TestAPIKeyExtractionFromTokenExchange tests API key extraction during OAuth token exchange
// This simulates the exchange_iflow_code_for_token flow from iflow.rs
func TestAPIKeyExtractionFromTokenExchange(t *testing.T) {
	t.Log("=== Testing API Key Extraction from Token Exchange ===")

	// This test would normally require a fresh OAuth flow with authorization code
	// For now, we'll simulate the token exchange part using existing credentials

	credsPath := filepath.Join(os.Getenv("HOME"), ".iflow", "oauth_creds.json")

	credsData, err := os.ReadFile(credsPath)
	if err != nil {
		t.Skipf("No credentials file found at %s, skipping test", credsPath)
		return
	}

	var creds Credentials
	if err := json.Unmarshal(credsData, &creds); err != nil {
		t.Fatalf("Failed to parse credentials: %v", err)
	}

	if creds.AccessToken == "" {
		t.Fatal("No access token available")
	}

	t.Logf("Simulating token exchange with access token: %s...", creds.AccessToken[:20])

	// Simulate the user info fetch that happens after token exchange
	// This matches the logic in exchange_iflow_code_for_token in iflow.rs

	userInfoURL := fmt.Sprintf("%s?accessToken=%s", UserInfoURL, url.QueryEscape(creds.AccessToken))

	t.Logf("Fetching user info for API key extraction...")

	req, err := http.NewRequest("GET", userInfoURL, nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	t.Logf("User info response: %s", string(body))

	var userResp map[string]interface{}
	if err := json.Unmarshal(body, &userResp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Extract API key and email exactly as in iflow.rs exchange_iflow_code_for_token
	var apiKey string
	var email string

	if success, ok := userResp["success"].(bool); ok && success {
		if data, ok := userResp["data"].(map[string]interface{}); ok {
			if key, exists := data["apiKey"]; exists {
				if keyStr, ok := key.(string); ok {
					apiKey = keyStr
					t.Logf("✓ API key extracted: %s...", apiKey[:20])
				}
			}

			if mail, exists := data["email"]; exists {
				if mailStr, ok := mail.(string); ok {
					email = mailStr
					t.Logf("✓ Email extracted: %s", email)
				}
			} else if phone, exists := data["phone"]; exists {
				if phoneStr, ok := phone.(string); ok {
					email = phoneStr
					t.Logf("✓ Phone extracted as email: %s", email)
				}
			}
		}
	}

	// Create credentials object as in iflow.rs
	extractedCreds := Credentials{
		AuthType:     "oauth",
		AccessToken:  creds.AccessToken,
		RefreshToken: creds.RefreshToken,
		APIKey:       apiKey,
		Email:        email,
		Type:         "iflow",
		LastRefresh:  time.Now().Format(time.RFC3339),
	}

	t.Logf("Extracted credentials:")
	t.Logf("  Auth Type: %s", extractedCreds.AuthType)
	t.Logf("  Has API Key: %t", extractedCreds.APIKey != "")
	t.Logf("  Email: %s", extractedCreds.Email)

	if extractedCreds.APIKey == "" {
		t.Error("Failed to extract API key from user data")
	}

	t.Log("\n=== API Key Extraction from Token Exchange Test Finished ===")
}

// TestRealTokenRefreshDirect tests direct token refresh with real endpoints
func TestRealTokenRefreshDirect(t *testing.T) {
	t.Log("=== Testing Direct Token Refresh with Real Endpoints ===")

	// Load existing credentials
	credsPath := filepath.Join(os.Getenv("HOME"), ".iflow", "oauth_creds.json")

	credsData, err := os.ReadFile(credsPath)
	if err != nil {
		t.Skipf("No credentials file found at %s, skipping test", credsPath)
		return
	}

	var creds Credentials
	if err := json.Unmarshal(credsData, &creds); err != nil {
		t.Fatalf("Failed to parse credentials: %v", err)
	}

	if creds.RefreshToken == "" {
		t.Fatal("No refresh token available in credentials")
	}

	t.Logf("Using credentials for user: %s", creds.Email)
	t.Logf("Refresh token: %s...", creds.RefreshToken[:20])

	// Prepare token refresh request
	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", creds.RefreshToken)
	data.Set("client_id", ClientID)
	data.Set("client_secret", ClientSecret)

	// Create Basic Auth header
	authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte(ClientID+":"+ClientSecret))

	// Create request
	req, err := http.NewRequest("POST", TokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", authHeader)

	// Log request details
	t.Logf("Token refresh request:")
	t.Logf("  URL: %s", TokenURL)
	t.Logf("  Method: %s", req.Method)
	t.Logf("  Headers:")
	for key, values := range req.Header {
		for _, value := range values {
			if strings.Contains(strings.ToLower(key), "auth") {
				t.Logf("    %s: %s...", key, value[:20])
			} else {
				t.Logf("    %s: %s", key, value)
			}
		}
	}
	t.Logf("  Body: grant_type=refresh_token&refresh_token=%s...&client_id=%s&client_secret=%s",
		creds.RefreshToken[:10], ClientID, "***")

	// Make request
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	// Log response details
	t.Logf("Token refresh response:")
	t.Logf("  Status Code: %d", resp.StatusCode)
	t.Logf("  Headers:")
	for key, values := range resp.Header {
		for _, value := range values {
			t.Logf("    %s: %s", key, value)
		}
	}
	t.Logf("  Body: %s", string(body))

	// Parse response
	var tokenResp map[string]interface{}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Check for error
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Token refresh failed with status %d", resp.StatusCode)
		return
	}

	// Extract new tokens
	if accessToken, ok := tokenResp["access_token"].(string); ok {
		t.Logf("✓ New access token received: %s...", accessToken[:20])

		// Update credentials
		creds.AccessToken = accessToken
		if refreshToken, ok := tokenResp["refresh_token"].(string); ok {
			creds.RefreshToken = refreshToken
			t.Logf("✓ New refresh token received: %s...", refreshToken[:20])
		}
		if tokenType, ok := tokenResp["token_type"].(string); ok {
			creds.TokenType = tokenType
		}
		if expiresIn, ok := tokenResp["expires_in"].(float64); ok {
			expiresAt := time.Now().Add(time.Duration(expiresIn) * time.Second)
			creds.ExpiresAt = expiresAt.Format(time.RFC3339)
			creds.Expire = creds.ExpiresAt
		}
		creds.LastRefresh = time.Now().Format(time.RFC3339)

		// Save updated credentials
		updatedData, _ := json.MarshalIndent(creds, "", "  ")
		os.WriteFile(credsPath, updatedData, 0600)

		t.Logf("✓ Credentials updated and saved")
	} else {
		t.Error("No access token in response")
	}
}

// TestRealAPIKeyRefresh tests API key refresh with cookies
func TestRealAPIKeyRefresh(t *testing.T) {
	t.Log("=== Testing API Key Refresh with Real Endpoints ===")

	// Load existing credentials
	credsPath := filepath.Join(os.Getenv("HOME"), ".iflow", "oauth_creds.json")

	credsData, err := os.ReadFile(credsPath)
	if err != nil {
		t.Skipf("No credentials file found at %s, skipping test", credsPath)
		return
	}

	var creds Credentials
	if err := json.Unmarshal(credsData, &creds); err != nil {
		t.Fatalf("Failed to parse credentials: %v", err)
	}

	if creds.AuthType != "cookie" && creds.Cookies == "" {
		t.Skip("No cookie credentials available, skipping API key refresh test")
		return
	}

	t.Logf("Using cookie-based authentication")
	t.Logf("User: %s", creds.Email)

	// Step 1: Get current API key info
	t.Log("\n--- Step 1: Get Current API Key Info ---")

	req, err := http.NewRequest("GET", APIKeyURL, nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	req.Header.Set("Cookie", creds.Cookies)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	t.Logf("GET API Key Info Response:")
	t.Logf("  Status Code: %d", resp.StatusCode)
	t.Logf("  Body: %s", string(body))

	var getResp map[string]interface{}
	if err := json.Unmarshal(body, &getResp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if success, ok := getResp["success"].(bool); !ok || !success {
		t.Errorf("Get API key info failed: %+v", getResp)
		return
	}

	data, ok := getResp["data"].(map[string]interface{})
	if !ok {
		t.Fatal("No data in response")
	}

	apiKeyName, _ := data["name"].(string)
	t.Logf("Current API Key Name: %s", apiKeyName)

	// Step 2: Refresh API key
	t.Log("\n--- Step 2: Refresh API Key ---")

	refreshBody := map[string]string{"name": apiKeyName}
	bodyData, _ := json.Marshal(refreshBody)

	req, err = http.NewRequest("POST", APIKeyURL, strings.NewReader(string(bodyData)))
	if err != nil {
		t.Fatalf("Failed to create POST request: %v", err)
	}

	req.Header.Set("Cookie", creds.Cookies)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("Origin", "https://platform.iflow.cn")
	req.Header.Set("Referer", "https://platform.iflow.cn/")

	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("POST request failed: %v", err)
	}
	defer resp.Body.Close()

	body, err = io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read POST response: %v", err)
	}

	t.Logf("POST API Key Refresh Response:")
	t.Logf("  Status Code: %d", resp.StatusCode)
	t.Logf("  Body: %s", string(body))

	var postResp map[string]interface{}
	if err := json.Unmarshal(body, &postResp); err != nil {
		t.Fatalf("Failed to parse POST response: %v", err)
	}

	if success, ok := postResp["success"].(bool); !ok || !success {
		t.Errorf("API key refresh failed: %+v", postResp)
		return
	}

	refreshedData, ok := postResp["data"].(map[string]interface{})
	if !ok {
		t.Fatal("No data in refresh response")
	}

	if newAPIKey, ok := refreshedData["apiKey"].(string); ok {
		t.Logf("✓ New API Key: %s...", newAPIKey[:20])

		// Update credentials
		creds.APIKey = newAPIKey
		if expireTime, ok := refreshedData["expireTime"].(string); ok {
			creds.Expire = expireTime
			creds.ExpiresAt = expireTime
		}
		creds.LastRefresh = time.Now().Format(time.RFC3339)

		// Save updated credentials
		updatedData, _ := json.MarshalIndent(creds, "", "  ")
		os.WriteFile(credsPath, updatedData, 0600)

		t.Logf("✓ API key updated and saved")
	}
}

// TestManualRefresh demonstrates manual token refresh using oauth2
func TestManualRefresh(t *testing.T) {
	auth := NewAuthenticator(nil)

	if !auth.IsAuthenticated() {
		t.Log("No valid credentials found. Please authenticate first.")
		return
	}

	ctx := context.Background()

	// Load credentials
	auth.mu.Lock()
	auth.loadCredentials()
	creds := auth.credentials
	auth.mu.Unlock()

	if creds == nil {
		t.Fatal("No credentials loaded")
	}

	// Create oauth2 token
	token := &oauth2.Token{
		AccessToken:  creds.AccessToken,
		RefreshToken: creds.RefreshToken,
		TokenType:    creds.TokenType,
	}

	if creds.ExpiresAt != "" {
		if expiry, err := time.Parse(time.RFC3339, creds.ExpiresAt); err == nil {
			token.Expiry = expiry
		}
	}

	// Force expiry to trigger refresh (for testing)
	token.Expiry = time.Now().Add(-time.Hour) // Set to 1 hour ago

	// Setup OAuth2 config
	conf := &oauth2.Config{
		ClientID:     auth.config.ClientID,
		ClientSecret: auth.config.ClientSecret,
		Endpoint: oauth2.Endpoint{
			AuthURL:  AuthURL,
			TokenURL: TokenURL,
		},
	}

	// Create token source
	oauth2Context := context.WithValue(ctx, oauth2.HTTPClient, auth.httpClient)
	ts := conf.TokenSource(oauth2Context, token)

	// Get token (refresh if needed)
	newToken, err := ts.Token()
	if err != nil {
		// Print the error as the response
		fmt.Printf("Token Refresh Response: Error - %v\n", err)
		fmt.Printf("This indicates the refresh attempt failed.\n")
		return
	}

	// Print the response details
	fmt.Printf("Token Response Details:\n")
	fmt.Printf("Access Token: %s\n", newToken.AccessToken)
	fmt.Printf("Token Type: %s\n", newToken.TokenType)
	fmt.Printf("Expiry: %v\n", newToken.Expiry)
	if newToken.RefreshToken != "" {
		fmt.Printf("Refresh Token: %s\n", newToken.RefreshToken)
	}

	// Check if refreshed
	if newToken.AccessToken != creds.AccessToken {
		fmt.Printf("Token was refreshed successfully!\n")

		// Update credentials
		auth.mu.Lock()
		auth.credentials.AccessToken = newToken.AccessToken
		auth.credentials.RefreshToken = newToken.RefreshToken
		auth.credentials.TokenType = newToken.TokenType
		if !newToken.Expiry.IsZero() {
			auth.credentials.ExpiresAt = newToken.Expiry.Format(time.RFC3339)
			auth.credentials.ExpiryDate = newToken.Expiry.UnixMilli()
		}
		auth.credentials.LastRefresh = time.Now().Format(time.RFC3339)

		// Fetch user info to ensure API key is updated
		if err := auth.fetchUserInfo(); err != nil {
			fmt.Printf("Warning: Failed to fetch user info after manual refresh: %v\n", err)
		}

		if err := auth.saveCredentials(); err != nil {
			t.Errorf("Failed to save credentials: %v", err)
		} else {
			fmt.Printf("Credentials saved successfully.\n")
		}
		auth.mu.Unlock()

	} else {
		fmt.Printf("Token was not refreshed.\n")
	}
}

// TestRawRefreshResponse makes a direct HTTP request to see the raw response
func TestRawRefreshResponse(t *testing.T) {
	auth := NewAuthenticator(nil)

	if !auth.IsAuthenticated() {
		t.Log("No valid credentials found. Please authenticate first.")
		return
	}

	// Load credentials
	auth.mu.Lock()
	auth.loadCredentials()
	creds := auth.credentials
	auth.mu.Unlock()

	if creds == nil {
		t.Fatal("No credentials loaded")
	}

	if creds.RefreshToken == "" {
		t.Fatal("No refresh token available")
	}

	// Prepare the request data (matching iflow.rs implementation)
	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", creds.RefreshToken)
	data.Set("client_id", auth.config.ClientID)
	data.Set("client_secret", auth.config.ClientSecret)

	// Create Basic Auth header
	authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte(auth.config.ClientID+":"+auth.config.ClientSecret))

	// Create the request
	req, err := http.NewRequest("POST", TokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", authHeader)

	// Make the request
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	// Read the raw response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}

	// Print raw response details
	fmt.Printf("Raw Token Refresh Response:\n")
	fmt.Printf("Status Code: %d\n", resp.StatusCode)
	fmt.Printf("Headers:\n")
	for key, values := range resp.Header {
		for _, value := range values {
			fmt.Printf("  %s: %s\n", key, value)
		}
	}
	fmt.Printf("Body:\n%s\n", string(body))

	// Try to parse as JSON for additional info
	var jsonResp map[string]interface{}
	if err := json.Unmarshal(body, &jsonResp); err == nil {
		fmt.Printf("Parsed JSON Response:\n")
		for key, value := range jsonResp {
			fmt.Printf("  %s: %v\n", key, value)
		}

		// If we got a new access token, save it
		if accessToken, ok := jsonResp["access_token"].(string); ok && accessToken != "" {
			auth.mu.Lock()
			auth.credentials.AccessToken = accessToken

			if refreshToken, ok := jsonResp["refresh_token"].(string); ok && refreshToken != "" {
				auth.credentials.RefreshToken = refreshToken
			}

			if tokenType, ok := jsonResp["token_type"].(string); ok {
				auth.credentials.TokenType = tokenType
			}

			if expiresIn, ok := jsonResp["expires_in"].(float64); ok {
				expiresAt := time.Now().Add(time.Duration(expiresIn) * time.Second)
				auth.credentials.ExpiresAt = expiresAt.Format(time.RFC3339)
				auth.credentials.Expire = auth.credentials.ExpiresAt
				auth.credentials.ExpiryDate = expiresAt.UnixMilli()
			}

			auth.credentials.LastRefresh = time.Now().Format(time.RFC3339)

			// Fetch user info to update API key
			if err := auth.fetchUserInfo(); err != nil {
				fmt.Printf("Warning: Failed to fetch user info after raw refresh: %v\n", err)
			}

			if err := auth.saveCredentials(); err != nil {
				t.Errorf("Failed to save credentials: %v", err)
			} else {
				fmt.Printf("Credentials updated and saved from raw refresh response.\n")
			}
			auth.mu.Unlock()
		}
	}
}

// TestUserInfoResponse tests the user info endpoint to see if it contains API key
func TestUserInfoResponse(t *testing.T) {
	auth := NewAuthenticator(nil)

	if !auth.IsAuthenticated() {
		t.Log("No valid credentials found. Please authenticate first.")
		return
	}

	// Load credentials
	auth.mu.Lock()
	auth.loadCredentials()
	creds := auth.credentials
	auth.mu.Unlock()

	if creds == nil || creds.AccessToken == "" {
		t.Fatal("No access token available")
	}

	// Create the user info URL (matching iflow.rs implementation)
	userInfoURL := fmt.Sprintf("%s?accessToken=%s", UserInfoURL, url.QueryEscape(creds.AccessToken))

	// Create the request
	req, err := http.NewRequest("GET", userInfoURL, nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	req.Header.Set("Accept", "application/json")

	// Make the request
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	// Read the raw response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}

	// Print raw response details
	fmt.Printf("Raw User Info Response:\n")
	fmt.Printf("Status Code: %d\n", resp.StatusCode)
	fmt.Printf("Headers:\n")
	for key, values := range resp.Header {
		for _, value := range values {
			fmt.Printf("  %s: %s\n", key, value)
		}
	}
	fmt.Printf("Body:\n%s\n", string(body))

	// Try to parse as JSON for additional info
	var jsonResp map[string]interface{}
	if err := json.Unmarshal(body, &jsonResp); err == nil {
		fmt.Printf("Parsed JSON Response:\n")
		for key, value := range jsonResp {
			fmt.Printf("  %s: %v\n", key, value)
		}

		// Check for API key in the data field
		if data, ok := jsonResp["data"].(map[string]interface{}); ok {
			fmt.Printf("Data field contents:\n")
			for key, value := range data {
				fmt.Printf("  %s: %v\n", key, value)
			}
		}

		// Assert success
		if success, ok := jsonResp["success"].(bool); !ok || !success {
			t.Errorf("User info request failed. Response: %v", jsonResp)
		} else {
			t.Log("✓ User info request successful")
		}
	}
}
