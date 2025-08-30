package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"qwenproxy/logging"
	"qwenproxy/qwenclient"
	"qwenproxy/streaming"
	"qwenproxy/utils"
)

// HandleResponse checks if the request is streaming and calls the appropriate handler
func HandleResponse(w http.ResponseWriter, r *http.Request, accessToken, targetURL string, originalBody map[string]interface{}) {
	// Check if client request is streaming. Default to false if "stream" field is not present.
	isClientStreaming := false
	if streamVal, ok := originalBody["stream"]; ok {
		if streamBool, isBool := streamVal.(bool); isBool {
			isClientStreaming = streamBool
		}
	}

	if isClientStreaming {
		// If client is streaming, call the streaming proxy handler
		streaming.StreamProxyHandler(w, r, accessToken, targetURL, originalBody, time.Now(), SharedHTTPClient)
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
	logging.NewLogger().NonStreamLog("%s bytes %s to %s", utils.FormatIntWithCommas(int64(len(modifiedBodyBytes))), r.Method, r.URL.Path)

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

	// Use the shared HTTP client from the proxy package
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
		logging.NewLogger().ErrorLog("Failed to copy response body: %v", err)
	}

	// Calculate the duration
	duration := time.Since(startTime).Milliseconds()

	// Parse the response body to extract usage information
	var response qwenclient.NonStreamResponse
	if err := json.Unmarshal(buf.Bytes(), &response); err == nil && response.Usage != nil {
		// Log the DONE message with usage information and duration, with comma-formatted numbers
		usageBytes, _ := json.Marshal(response.Usage)
		logging.NewLogger().DoneNonStreamLog("Raw Usage: %s, Duration: %s ms", string(usageBytes), utils.FormatIntWithCommas(duration))
		logging.NewLogger().SeparatorLog()
	} else {
		// Log the DONE message with duration, with comma-formatted numbers
		logging.NewLogger().DoneNonStreamLog("No usage information found in response, Duration: %s ms", utils.FormatIntWithCommas(duration))
		logging.NewLogger().SeparatorLog()
	}
}

// ProxyHandler handles incoming requests and proxies them to the target endpoint
func ProxyHandler(w http.ResponseWriter, r *http.Request) {
	// Get valid token and endpoint
	accessToken, targetEndpoint, err := qwenclient.GetValidTokenAndEndpoint()
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
	targetURL, originalBody, err := qwenclient.PrepareRequest(r, targetEndpoint)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to prepare request: %v", err), http.StatusInternalServerError)
		return
	}

	// Handle the response
	// Pass the original request for streaming
	HandleResponse(w, r, accessToken, targetURL, originalBody)
}

// ModelsHandler handles requests to /v1/models and serves the models.json file
func ModelsHandler(w http.ResponseWriter, r *http.Request) {
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
		logging.NewLogger().ErrorLog("Failed to write response: %v", err)
	}
}