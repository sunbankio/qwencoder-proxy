package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"
)

// formatIntWithCommas formats an integer with commas as thousands separators
func formatIntWithCommas(n int64) string {
    if n == 0 {
        return "0"
    }
    
    var result []string
    s := fmt.Sprintf("%d", n)
    sign := ""
    
    if s[0] == '-' {
        sign = "-"
        s = s[1:]
    }
    
    for i, c := range s {
        if (len(s)-i)%3 == 0 && i != 0 {
            result = append(result, ",")
        }
        result = append(result, string(c))
    }
    
    return sign + strings.Join(result, "")
}

// Define structs to match the non-streaming response structure for logging usage
type NonStreamResponse struct {
	Usage *UsageDetails `json:"usage,omitempty"`
}

type UsageDetails struct {
	PromptTokens     int                `json:"prompt_tokens,omitempty"`
	CompletionTokens int                `json:"completion_tokens,omitempty"`
	TotalTokens      int                `json:"total_tokens,omitempty"`
	PromptTokensDetails *PromptTokensDetails `json:"prompt_tokens_details,omitempty"`
}

type PromptTokensDetails struct {
	CacheType    string `json:"cache_type,omitempty"`
	CachedTokens int    `json:"cached_tokens,omitempty"`
}

// getValidTokenAndEndpoint gets a valid token and determines the correct endpoint
func getValidTokenAndEndpoint() (string, string, error) {
	credentials, err := loadQwenCredentials()
	if err != nil {
		// If credentials file doesn't exist, return a special error that can be handled by the caller
		return "", "", fmt.Errorf("credentials not found: %v. Please authenticate with Qwen by visiting /auth endpoint", err)
	}

	// If token is expired or about to expire, try to refresh it
	if !isTokenValid(credentials) {
		log.Println("Token is expired or about to expire, attempting to refresh...")
		credentials, err = refreshAccessToken(credentials)
		if err != nil {
			// If token refresh fails, return a special error that can be handled by the caller
			return "", "", fmt.Errorf("failed to refresh token: %v. Please re-authenticate with Qwen by visiting /auth endpoint", err)
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
	if !strings.HasPrefix(baseEndpoint, "http") {
		baseEndpoint = "https://" + baseEndpoint
	}

	const suffix = "/v1"
	if !strings.HasSuffix(baseEndpoint, suffix) {
		baseEndpoint = baseEndpoint + suffix
	}
	return credentials.AccessToken, baseEndpoint, nil
}

// prepareRequest constructs the target URL and processes the request body
func prepareRequest(r *http.Request, targetEndpoint string) (string, map[string]interface{}, error) {
	// Construct the full target URL
	requestPath := r.URL.Path
	if strings.HasPrefix(requestPath, "/v1") && strings.HasSuffix(targetEndpoint, "/v1") {
		requestPath = strings.TrimPrefix(requestPath, "/v1")
	}
	targetURL := targetEndpoint + requestPath

	// Read the request body
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		return "", nil, fmt.Errorf("failed to read request body: %w", err)
	}
	// Restore the body for further reads downstream (e.g., in proxy handlers)
	r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	originalBody := make(map[string]interface{})
	if len(bodyBytes) > 0 {
		if err := json.Unmarshal(bodyBytes, &originalBody); err != nil {
			return "", nil, fmt.Errorf("failed to unmarshal request body: %w", err)
		}
	}
	
	return targetURL, originalBody, nil
}

// handleResponse checks if the request is streaming and calls the appropriate handler
func handleResponse(w http.ResponseWriter, r *http.Request, accessToken, targetURL string, originalBody map[string]interface{}) {
	// Check if client request is streaming. Default to false if "stream" field is not present.
	isClientStreaming := false
	if streamVal, ok := originalBody["stream"]; ok {
		if streamBool, isBool := streamVal.(bool); isBool {
			isClientStreaming = streamBool
		}
	}

	if isClientStreaming {
		// If client is streaming, call the streaming proxy handler
		StreamProxyHandler(w, r, accessToken, targetURL, originalBody, time.Now())
	} else {
		// If client is not streaming, call the non-streaming proxy handler
		NonStreamProxyHandler(w, r, accessToken, targetURL, originalBody)
	}
}

// NonStreamProxyHandler handles non-streaming requests
func NonStreamProxyHandler(w http.ResponseWriter, r *http.Request, accessToken, targetEndpoint string, originalBody map[string]interface{}) {
	startTime := time.Now()
	
	// Marshal the modified body for the upstream request
	modifiedBodyBytes, err := json.Marshal(originalBody)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to marshal request body: %v", err), http.StatusInternalServerError)
		return
	}
	
	// Log the non-streaming request details with comma-formatted bytes
	log.Printf("[REQUESTING_NON_STREAMING]: %s bytes %s to %s", formatIntWithCommas(int64(len(modifiedBodyBytes))), r.Method, r.URL.Path)

	// Create a new request to the target endpoint
	req, err := http.NewRequest(r.Method, targetEndpoint, bytes.NewBuffer(modifiedBodyBytes))
	if err != nil {
		http.Error(w, "Failed to create proxy request", http.StatusInternalServerError)
		return
	}

	// Copy headers from original request, but set necessary ones
	for name, values := range r.Header {
		if strings.EqualFold(name, "Authorization") || strings.EqualFold(name, "Content-Type") {
			continue // Handled below or not relevant
		}
		for _, value := range values {
			req.Header.Add(name, value)
		}
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json") // Always JSON for body
	req.Header.Set("User-Agent", fmt.Sprintf("QwenCode/0.0.9 (%s; %s)", runtime.GOOS, runtime.GOARCH)) // Add runtime import if not already present
	req.Header.Set("X-DashScope-CacheControl", "enable")
	req.Header.Set("X-DashScope-UserAgent", fmt.Sprintf("QwenCode/0.0.9 (%s; %s)", runtime.GOOS, runtime.GOARCH))
	req.Header.Set("X-DashScope-AuthType", "qwen-oauth")

	client := SharedHTTPClient

	// Send the request to the target endpoint
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to send request to target endpoint: %v", err), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	// Copy headers from the upstream response to the client
	for name, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(name, value)
		}
	}
	// Capture the response body to parse usage information
	var buf bytes.Buffer
	teeReader := io.TeeReader(resp.Body, &buf)
	
	// Copy the response body from upstream to the client
	if _, err := io.Copy(w, teeReader); err != nil {
		log.Printf("Failed to copy response body: %v", err)
	}
	
	// Calculate the duration
	duration := time.Since(startTime).Milliseconds()
	
	// Parse the response body to extract usage information
	var response NonStreamResponse
	if err := json.Unmarshal(buf.Bytes(), &response); err == nil && response.Usage != nil {
		// Log the DONE message with usage information and duration, with comma-formatted numbers
		usageBytes, _ := json.Marshal(response.Usage)
		log.Printf("[DONE_NON_STREAMING] - Raw Usage: %s, Duration: %s ms", string(usageBytes), formatIntWithCommas(duration))
	} else {
		// Log the DONE message with duration, with comma-formatted numbers
		log.Printf("[DONE_NON_STREAMING] - No usage information found in response, Duration: %s ms", formatIntWithCommas(duration))
	}
}

// proxyHandler handles incoming requests and proxies them to the target endpoint
func proxyHandler(w http.ResponseWriter, r *http.Request) {
	// Get valid token and endpoint
	accessToken, targetEndpoint, err := getValidTokenAndEndpoint()
	if err != nil {
		// Check if the error is related to authentication
		errorMsg := err.Error()
		// If authentication is required, signal to the user to restart the proxy for authentication.
		// The authentication flow will now be handled during server startup.
		if strings.Contains(errorMsg, "credentials not found") || strings.Contains(errorMsg, "failed to refresh token") {
			http.Error(w, fmt.Sprintf("Authentication required: %v. Please restart the proxy to re-authenticate.", err), http.StatusUnauthorized)
			return
		}
		// For other errors, return a generic error message
		http.Error(w, fmt.Sprintf("Failed to get valid token: %v", err), http.StatusInternalServerError)
		return
	}

	// Prepare the request
	targetURL, originalBody, err := prepareRequest(r, targetEndpoint)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to prepare request: %v", err), http.StatusInternalServerError)
		return
	}

	// Handle the response
	// Pass the original request for streaming
	handleResponse(w, r, accessToken, targetURL, originalBody)
}

// modelsHandler handles requests to /v1/models and serves the models.json file
func modelsHandler(w http.ResponseWriter, r *http.Request) {
	// Set the correct content type for JSON
	w.Header().Set("Content-Type", "application/json")

	// Read the models.json file
	modelsData, err := os.ReadFile("models.json")
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read models.json: %v", err), http.StatusInternalServerError)
		return
	}

	// Write the file content to the response
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(modelsData); err != nil {
		log.Printf("Failed to write response: %v", err)
	}
}

func main() {
	// Set up the HTTP handler for /v1/models
	http.HandleFunc("/v1/models", modelsHandler)

	// Set up the general proxy handler for all other routes
	http.HandleFunc("/", proxyHandler)

	// Check for credentials on startup
	log.Println("Checking Qwen credentials...")
	_, _, err := getValidTokenAndEndpoint()
	if err != nil {
		errorMsg := err.Error()
		if strings.Contains(errorMsg, "credentials not found") || strings.Contains(errorMsg, "failed to refresh token") {
			log.Println("Credentials not found or invalid. Initiating authentication flow...")
			// Ensure the credentials file is removed before attempting authentication
			credsPath := getQwenCredentialsPath()
			if _, fileErr := os.Stat(credsPath); fileErr == nil {
				if removeErr := os.Remove(credsPath); removeErr != nil {
					log.Printf("Failed to remove existing credentials file %s: %v", credsPath, removeErr)
				} else {
					log.Printf("Successfully removed existing credentials file: %s", credsPath)
				}
			}

			authErr := AuthenticateWithOAuth()
			if authErr != nil {
				log.Fatalf("Authentication failed during startup: %v", authErr)
			}
			log.Println("Authentication successful. Starting proxy server...")
		} else {
			log.Fatalf("Failed to check credentials on startup: %v", err)
		}
	} else {
		log.Println("Credentials found and valid. Starting proxy server...")
	}

	// Start the server
	fmt.Printf("Proxy server starting on port %s\n", Port)
	if err := http.ListenAndServe(":"+Port, nil); err != nil {
		log.Fatal("Server failed to start: ", err)
	}
}
