// Package proxy provides HTTP handlers for the proxy server
package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/sunbankio/qwencoder-proxy/logging"
	"github.com/sunbankio/qwencoder-proxy/qwenclient"
)

// Model represents the structure of a model
type Model struct {
	ID                  string   `json:"id"`
	Name                string   `json:"name"`
	ContextLength       int      `json:"context_length"`
	Architecture        Arch     `json:"architecture"`
	PerRequestLimits    *string  `json:"per_request_limits"`
	SupportedParameters []string `json:"supported_parameters"`
}

// Arch represents the architecture section of a model
type Arch struct {
	Modality         string   `json:"modality"`
	InputModalities  []string `json:"input_modalities"`
	OutputModalities []string `json:"output_modalities"`
	Tokenizer        string   `json:"tokenizer"`
	InstructType     *string  `json:"instruct_type"`
}

// ModelsResponse represents the overall structure of the models response
type ModelsResponse struct {
	Object string  `json:"object"`
	Data   []Model `json:"data"`
}

// responseWriterWrapper wraps http.ResponseWriter to capture response size and status code
type responseWriterWrapper struct {
	http.ResponseWriter
	statusCode int
	size       int
}

// readRequestBody reads the request body and handles errors
func readRequestBody(w http.ResponseWriter, r *http.Request) []byte {
	var requestBodyBytes []byte
	if r.Body != nil {
		var err error
		requestBodyBytes, err = io.ReadAll(r.Body)
		if err != nil {
			logging.NewLogger().ErrorLog("Failed to read request body: %v", err)
			http.Error(w, "Failed to read request body", http.StatusInternalServerError)
			return nil
		}
		r.Body = io.NopCloser(bytes.NewBuffer(requestBodyBytes))
	}
	return requestBodyBytes
}

// handleAuthError handles authentication errors with appropriate HTTP responses
func handleAuthError(w http.ResponseWriter, err error) {
	errorMsg := err.Error()
	if strings.Contains(errorMsg, "credentials not found") || strings.Contains(errorMsg, "failed to refresh token") {
		http.Error(w, fmt.Sprintf("Authentication required: %v. Please restart the proxy to re-authenticate.", err), http.StatusUnauthorized)
		return
	}
	http.Error(w, fmt.Sprintf("Failed to get valid token: %v", err), http.StatusInternalServerError)
}

// constructTargetURL builds the target URL, handling potential duplicate /v1 paths
func constructTargetURL(requestPath, targetEndpoint string) string {
	if strings.HasPrefix(requestPath, "/v1") && strings.HasSuffix(targetEndpoint, "/v1") {
		requestPath = strings.TrimPrefix(requestPath, "/v1")
	}
	return targetEndpoint + requestPath
}

// WriteHeader captures the status code and calls the original WriteHeader
func (w *responseWriterWrapper) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

// Write captures the response size and calls the original Write
func (w *responseWriterWrapper) Write(b []byte) (int, error) {
	size, err := w.ResponseWriter.Write(b)
	w.size += size
	return size, err
}

// Flush implements the http.Flusher interface
func (w *responseWriterWrapper) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// checkIfStreaming determines if the request is a streaming request
func checkIfStreaming(requestBodyBytes []byte) bool {
	isClientStreaming := false
	if len(requestBodyBytes) > 0 {
		var requestJSON map[string]interface{}
		err := json.Unmarshal(requestBodyBytes, &requestJSON)
		if err == nil {
			if streamVal, ok := requestJSON["stream"].(bool); ok && streamVal {
				isClientStreaming = true
			}
		} else {
			logging.NewLogger().ErrorLog("Failed to unmarshal request body for stream check: %v", err)
		}
	}
	logging.NewLogger().DebugLog("isClientStreaming evaluated to: %t", isClientStreaming)
	return isClientStreaming
}

// ProxyHandler handles incoming requests and proxies them to the target endpoint
func ProxyHandler(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	// Get client IP
	clientIP := r.RemoteAddr

	// Get user agent
	userAgent := r.Header.Get("User-Agent")
	if userAgent == "" {
		userAgent = "unknown"
	}

	// Log incoming request details
	logging.NewLogger().DebugLog("Incoming Request: Method=%s URL=%s Content-Length=%d ClientIP=%s User-Agent=%s", r.Method, r.URL.Path, r.ContentLength, clientIP, userAgent)

	// Handle CORS preflight OPTIONS requests
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With, X-DashScope-CacheControl, X-DashScope-UserAgent, X-DashScope-AuthType")
		w.Header().Set("Access-Control-Max-Age", "3600")
		w.WriteHeader(http.StatusOK)
		return
	}

	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		clientIP = ip
	} else if ip := r.Header.Get("X-Real-IP"); ip != "" {
		clientIP = ip
	}

	requestBodyBytes := readRequestBody(w, r)
	if requestBodyBytes == nil {
		return // Error already handled
	}

	// Add debug log for raw request body
	logging.NewLogger().DebugLog("Raw request body: %s", string(requestBodyBytes))

	// Check if streaming is enabled
	isClientStreaming := checkIfStreaming(requestBodyBytes)

	// Extract model information for logging
	model := extractModel(requestBodyBytes)

	accessToken, targetEndpoint, err := qwenclient.GetValidTokenAndEndpoint()
	if err != nil {
		handleAuthError(w, err)
		return
	}
	requestBodyBytes = updateModel(requestBodyBytes)

	targetURL := constructTargetURL(r.URL.Path, targetEndpoint)

	req, err := http.NewRequest(r.Method, targetURL, bytes.NewBuffer(requestBodyBytes))
	if err != nil {
		http.Error(w, "Failed to create proxy request", http.StatusInternalServerError)
		return
	}

	SetProxyHeaders(req, accessToken)

	client := SharedHTTPClient

	// Create a context that can be cancelled when client disconnects
	ctx := r.Context()
	resp, err := client.Do(req.WithContext(ctx))
	if err != nil {
		// Check if the error is due to client disconnection
		if ctx.Err() == context.Canceled {
			// Log the request with error information
			duration := time.Since(startTime).Milliseconds()
			logging.NewLogger().ProxyRequestLog(
				clientIP,
				r.Method,
				r.URL.Path,
				userAgent,
				model,
				len(requestBodyBytes),
				isClientStreaming,
				0,   // upstream status (0 indicates error)
				499, // client status (499 = Client Closed Request)
				0,   // response size
				duration,
			)
			return
		}
		http.Error(w, fmt.Sprintf("Failed to send request to target endpoint: %v", err), http.StatusInternalServerError)

		// Log the request with error information
		duration := time.Since(startTime).Milliseconds()
		logging.NewLogger().ProxyRequestLog(
			clientIP,
			r.Method,
			r.URL.Path,
			userAgent,
			model,
			len(requestBodyBytes),
			isClientStreaming,
			0,   // upstream status (0 indicates error)
			500, // client status
			0,   // response size
			duration,
		)
		return
	}

	defer resp.Body.Close()

	// Create a response writer wrapper to capture response size
	responseWriter := &responseWriterWrapper{ResponseWriter: w, statusCode: resp.StatusCode}

	if isClientStreaming {
		// Use the new streaming handler
		HandleStreamingResponse(responseWriter, resp, r.Context())
	} else {
		handleNonStreamingResponse(responseWriter, resp)
	}

	// Log the request
	duration := time.Since(startTime).Milliseconds()
	logging.NewLogger().ProxyRequestLog(
		clientIP,
		r.Method,
		r.URL.Path,
		userAgent,
		model,
		len(requestBodyBytes),
		isClientStreaming,
		resp.StatusCode,           // upstream status
		responseWriter.statusCode, // client status
		responseWriter.size,       // response size
		duration,
	)
}

// handleNonStreamingResponse processes non-streaming responses
func handleNonStreamingResponse(w *responseWriterWrapper, resp *http.Response) {
	logging.NewLogger().DebugLog("Not a streaming request, copying body directly.")
	for name, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(name, value)
		}
	}
	if _, err := io.Copy(w, resp.Body); err != nil {
		logging.NewLogger().ErrorLog("Error copying response body: %v", err)
	}
}

// ModelsHandler handles requests to /v1/models and returns the model data directly
func ModelsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	logging.NewLogger().DebugLog("ModelsHandler received request")

	// Create the model response directly in code
	modelsResponse := ModelsResponse{
		Object: "list",
		Data: []Model{
			{
				ID:            "qwen3-coder-plus",
				Name:          "Qwen: Qwen3 Coder Plus",
				ContextLength: 1048576,
				Architecture: Arch{
					Modality:         "text->text",
					InputModalities:  []string{"text"},
					OutputModalities: []string{"text"},
					Tokenizer:        "Qwen3",
					InstructType:     nil,
				},
				PerRequestLimits: nil,
				SupportedParameters: []string{
					"frequency_penalty",
					"logit_bias",
					"logprobs",
					"max_tokens",
					"min_p",
					"presence_penalty",
					"repetition_penalty",
					"response_format",
					"seed",
					"stop",
					"structured_outputs",
					"temperature",
					"tool_choice",
					"tools",
					"top_k",
					"top_logprobs",
					"top_p",
				},
			},
			{
				ID:            "qwen3-coder-flash",
				Name:          "Qwen: Qwen3 Coder Flash",
				ContextLength: 262144,
				Architecture: Arch{
					Modality:         "text->text",
					InputModalities:  []string{"text"},
					OutputModalities: []string{"text"},
					Tokenizer:        "Qwen3",
					InstructType:     nil,
				},
				PerRequestLimits: nil,
				SupportedParameters: []string{
					"frequency_penalty",
					"logit_bias",
					"logprobs",
					"max_tokens",
					"min_p",
					"presence_penalty",
					"repetition_penalty",
					"response_format",
					"seed",
					"stop",
					"structured_outputs",
					"temperature",
					"tool_choice",
					"tools",
					"top_k",
					"top_logprobs",
					"top_p",
				},
			},
		},
	}

	// Marshal the response to JSON
	modelsData, err := json.Marshal(modelsResponse)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to marshal models data: %v", err), http.StatusInternalServerError)
		logging.NewLogger().ErrorLog("Failed to marshal models data: %v", err)
		return
	}

	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(modelsData); err != nil {
		logging.NewLogger().ErrorLog("Error writing models data to response: %v", err)
	}
}

// SetProxyHeaders sets the required headers for the outgoing proxy request.
func SetProxyHeaders(req *http.Request, accessToken string) {
	// Clear all existing headers
	req.Header = make(http.Header)

	// Set only the headers that the proxy explicitly defines
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json") // Always JSON for body
	req.Header.Set("User-Agent", fmt.Sprintf("QwenCode/0.0.11 (%s; %s)", runtime.GOOS, runtime.GOARCH))
	req.Header.Set("X-DashScope-CacheControl", "enable")
	req.Header.Set("X-DashScope-UserAgent", fmt.Sprintf("QwenCode/0.0.11 (%s; %s)", runtime.GOOS, runtime.GOARCH))
	req.Header.Set("X-DashScope-AuthType", "qwen-oauth")
}