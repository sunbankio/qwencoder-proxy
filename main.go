package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Constants
const (
	DefaultQwenBaseURL   = "https://dashscope.aliyuncs.com/compatible-mode/v1"
	Port                 = "8143"
	TokenRefreshBufferMs = 30 * 1000 // 30 seconds
)

// OAuthCreds represents the structure of the oauth_creds.json file
type OAuthCreds struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	RefreshToken string `json:"refresh_token"`
	ResourceURL  string `json:"resource_url"`
	ExpiryDate   int64  `json:"expiry_date"`
}

// getQwenCredentialsPath returns the path to the Qwen credentials file
func getQwenCredentialsPath() string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".qwen", "oauth_creds.json")
}

// loadQwenCredentials loads the Qwen credentials from the oauth_creds.json file
func loadQwenCredentials() (OAuthCreds, error) {
	credsPath := getQwenCredentialsPath()
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

// isTokenValid checks if the token is still valid
func isTokenValid(credentials OAuthCreds) bool {
	if credentials.ExpiryDate == 0 {
		return false
	}
	// Add 30 second buffer
	return time.Now().UnixMilli() < credentials.ExpiryDate-TokenRefreshBufferMs
}

// refreshAccessToken refreshes the OAuth token using the refresh token
func refreshAccessToken(credentials OAuthCreds) (OAuthCreds, error) {
	if credentials.RefreshToken == "" {
		return OAuthCreds{}, fmt.Errorf("no refresh token available")
	}

	const QwenOAuthTokenEndpoint = "https://chat.qwen.ai/api/v1/oauth2/token"
	const QwenOAuthClientID = "f0304373b74a44d2b584a3fb70ca9e56"

	bodyData := map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": credentials.RefreshToken,
		"client_id":     QwenOAuthClientID,
	}

	resp, err := http.Post(QwenOAuthTokenEndpoint, "application/x-www-form-urlencoded",
		bytes.NewBufferString(fmt.Sprintf("grant_type=%s&refresh_token=%s&client_id=%s",
			bodyData["grant_type"], bodyData["refresh_token"], bodyData["client_id"])))
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

	// Save updated credentials
	credsPath := getQwenCredentialsPath()
	file, err := os.Create(credsPath)
	if err != nil {
		return OAuthCreds{}, fmt.Errorf("failed to save updated credentials: %v", err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(updatedCredentials); err != nil {
		return OAuthCreds{}, fmt.Errorf("failed to encode updated credentials: %v", err)
	}

	return updatedCredentials, nil
}

// getValidTokenAndEndpoint gets a valid token and determines the correct endpoint
func getValidTokenAndEndpoint() (string, string, error) {
	credentials, err := loadQwenCredentials()
	if err != nil {
		return "", "", fmt.Errorf("failed to load Qwen credentials: %v", err)
	}

	// If token is expired or about to expire, try to refresh it
	if !isTokenValid(credentials) {
		log.Println("Token is expired or about to expire, attempting to refresh...")
		credentials, err = refreshAccessToken(credentials)
		if err != nil {
			return "", "", fmt.Errorf("failed to refresh token: %v", err)
		}
		log.Println("Token successfully refreshed")
	}

	if credentials.AccessToken == "" {
		return "", "", fmt.Errorf("no access token found in credentials")
	}

	// Use resource_url from credentials if available, otherwise fallback to default
	baseEndpoint := credentials.ResourceURL
	if baseEndpoint == "" {
		baseEndpoint = DefaultQwenBaseURL
	}

	// Normalize the URL: add protocol if missing, ensure /v1 suffix
	if !bytes.HasPrefix([]byte(baseEndpoint), []byte("http")) {
		baseEndpoint = "https://" + baseEndpoint
	}

	const suffix = "/v1"
	if !bytes.HasSuffix([]byte(baseEndpoint), []byte(suffix)) {
		baseEndpoint = baseEndpoint + suffix
	}

	log.Printf("Using endpoint: %s", baseEndpoint)

	return credentials.AccessToken, baseEndpoint, nil
}

// isBrokenPipe checks if the error indicates a broken pipe or connection reset
func isBrokenPipe(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "broken pipe") ||
		strings.Contains(errStr, "connection reset by peer") ||
		strings.Contains(errStr, "connection refused") // Although less common for client disconnect, can happen if target closes
}

// proxyHandler handles incoming requests and proxies them to the target endpoint
func proxyHandler(w http.ResponseWriter, r *http.Request) {
	// Get valid token and endpoint
	accessToken, targetEndpoint, err := getValidTokenAndEndpoint()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get valid token: %v", err), http.StatusInternalServerError)
		return
	}

	// Construct the full target URL
	requestPath := r.URL.Path
	if bytes.HasPrefix([]byte(requestPath), []byte("/v1")) && bytes.HasSuffix([]byte(targetEndpoint), []byte("/v1")) {
		requestPath = strings.TrimPrefix(requestPath, "/v1")
	}
	targetURL := targetEndpoint + requestPath

	// Create a context with a timeout for the upstream request
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second) // 90 seconds timeout
	defer cancel()                                                  // Ensure the context is cancelled to release resources

	// Read the original request body to modify it for non-streaming
	var originalBody map[string]interface{}
	var requestBodyBytes []byte
	if r.Body != nil {
		requestBodyBytes, err = io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to read original request body: %v", err), http.StatusInternalServerError)
			return
		}
		r.Body.Close() // Close the original body
		if len(requestBodyBytes) > 0 {
			if err := json.Unmarshal(requestBodyBytes, &originalBody); err != nil {
				log.Printf("Warning: Could not unmarshal original request body for modification: %v", err)
				originalBody = make(map[string]interface{}) // Proceed with empty map if unmarshal fails
			}
		} else {
			originalBody = make(map[string]interface{})
		}
	} else {
		originalBody = make(map[string]interface{})
	}

	// Ensure "stream" is set to false for the request to Qwen
	originalBody["stream"] = false
	// If stream_options is present, remove it for non-streaming requests
	if _, ok := originalBody["stream_options"]; ok {
		delete(originalBody, "stream_options")
	}
	modifiedBodyBytes, err := json.Marshal(originalBody)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to marshal modified request body: %v", err), http.StatusInternalServerError)
		return
	}

	// Create a new request to the target endpoint with modified body
	req, err := http.NewRequestWithContext(ctx, r.Method, targetURL, io.NopCloser(bytes.NewReader(modifiedBodyBytes)))
	if err != nil {
		http.Error(w, "Failed to create proxy request with modified body", http.StatusInternalServerError)
		return
	}
	req.ContentLength = int64(len(modifiedBodyBytes))

	// Start timer for duration logging
	startTime := time.Now()

	log.Printf("Starting non-streaming request to Qwen: %s %s", r.Method, r.URL.Path)

	// Copy headers from original request, but set necessary ones
	// mimicking exactly what Qwen CLI uses for non-streaming
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json") // Always JSON for body
	req.Header.Set("User-Agent", fmt.Sprintf("QwenCode/0.0.9 (%s; %s)", runtime.GOOS, runtime.GOARCH))
	req.Header.Set("X-DashScope-CacheControl", "enable")
	req.Header.Set("X-DashScope-UserAgent", fmt.Sprintf("QwenCode/0.0.9 (%s; %s)", runtime.GOOS, runtime.GOARCH))
	req.Header.Set("X-DashScope-AuthType", "qwen-oauth")

	// Configure a custom HTTP client with connection pool optimization and timeout
	transport := &http.Transport{
		MaxIdleConns:        100,              // Maximum idle connections across all hosts
		MaxIdleConnsPerHost: 10,               // Maximum idle connections per host
		IdleConnTimeout:     90 * time.Second, // How long an idle connection is kept alive
	}
	client := &http.Client{
		Timeout:   90 * time.Second, // Timeout for the entire request, including connection, send, and receive
		Transport: transport,
	}

	// Send the request to the target endpoint
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			http.Error(w, "Upstream request timed out", http.StatusGatewayTimeout)
			return
		}
		http.Error(w, fmt.Sprintf("Failed to send request to target endpoint: %v", err), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close() // Close the upstream response body

	// Read the entire response body from Qwen
	fullResponseBody, readBodyErr := io.ReadAll(resp.Body)
	if readBodyErr != nil {
		http.Error(w, fmt.Sprintf("Failed to read upstream response body: %v", readBodyErr), http.StatusInternalServerError)
		return
	}
	log.Printf("DEBUG_UPSTREAM_RAW_RESPONSE: %s", string(fullResponseBody)) // Added debug log

	var qwenResponse map[string]interface{}
	if err := json.Unmarshal(fullResponseBody, &qwenResponse); err != nil {
		http.Error(w, fmt.Sprintf("Failed to unmarshal Qwen response: %v", err), http.StatusInternalServerError)
		return
	}

	// Set necessary headers for SSE to the client
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Cache-Control")
	w.WriteHeader(resp.StatusCode) // Use upstream status code

	// Extract content and mimic streaming
	totalContent := ""
	if choices, ok := qwenResponse["choices"].([]interface{}); ok && len(choices) > 0 {
		if choice, ok := choices[0].(map[string]interface{}); ok {
			if message, ok := choice["message"].(map[string]interface{}); ok {
				if content, ok := message["content"].(string); ok {
					totalContent = content
				}
			}
		}
	}

	// Send entire content in one chunk
	chunk := map[string]interface{}{
		"choices": []map[string]interface{}{
			{
				"delta": map[string]interface{}{
					"content": totalContent,
				},
				"index": 0,
			},
		},
		"object": "chat.completion.chunk",
		"created": time.Now().Unix(),
		"model": qwenResponse["model"],
		"id": qwenResponse["id"],
	}

	jsonChunk, marshalErr := json.Marshal(chunk)
	if marshalErr != nil {
		log.Printf("Error marshaling mimicked chunk: %v", marshalErr)
		return // Can't recover from this, so return
	}

	fmt.Fprintf(w, "data: %s\n\n", jsonChunk)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}

	// Send [DONE] message
	fmt.Fprintf(w, "data: [DONE]\n\n")
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}

	modelName := "unknown" // Can get from qwenResponse["model"] if available
	if m, ok := qwenResponse["model"].(string); ok {
		modelName = m
	}
	requestID := "unknown"
	if id, ok := qwenResponse["id"].(string); ok {
		requestID = id
	}

	log.Printf("Fake streaming request completed. Path: %s, Model: %s, Duration: %s, RequestID: %s",
		r.URL.Path, modelName, time.Since(startTime).String(), requestID)
}

func main() {
	// Set up the HTTP handler
	http.HandleFunc("/", proxyHandler)

	// Start the server
	fmt.Printf("Proxy server starting on port %s\n", Port)
	if err := http.ListenAndServe(":"+Port, nil); err != nil {
		log.Fatal("Server failed to start: ", err)
	}
}