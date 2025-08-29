package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// Constants
const (
	DefaultQwenBaseURL   = "https://portal.qwen.ai/v1"
	Port                 = "8143"
	TokenRefreshBufferMs = 30 * 1000 // 30 seconds
)

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
	if !bytes.HasPrefix([]byte(baseEndpoint), []byte("http")) {
		baseEndpoint = "https://" + baseEndpoint
	}

	const suffix = "/v1"
	if !bytes.HasSuffix([]byte(baseEndpoint), []byte(suffix)) {
		baseEndpoint = baseEndpoint + suffix
	}
	return credentials.AccessToken, baseEndpoint, nil
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

	// Construct the full target URL
	requestPath := r.URL.Path
	if bytes.HasPrefix([]byte(requestPath), []byte("/v1")) && bytes.HasSuffix([]byte(targetEndpoint), []byte("/v1")) {
		requestPath = strings.TrimPrefix(requestPath, "/v1")
	}
	targetURL := targetEndpoint + requestPath

	// Read the original request body to determine if it's streaming
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

	// Check if client request is streaming
	isClientStreaming, _ := originalBody["stream"].(bool)

	// If not streaming, return a 400 Bad Request as per the user's instructions.
	if !isClientStreaming {
		http.Error(w, "Non-streaming requests are not supported yet. Please send a streaming request.", http.StatusBadRequest)
		return
	}

	// If client is streaming, call the streaming proxy handler
	StreamProxyHandler(w, r, accessToken, targetURL, originalBody, time.Now()) // Pass current time for startTime
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
