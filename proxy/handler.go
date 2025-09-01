package proxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"qwenproxy/logging"
	"qwenproxy/qwenclient"
)

var deepDebug = flag.Bool("deepdebug", false, "Enable deep debugging to log raw responses to file")

// Model represents the structure of a model
type Model struct {
	ID                  string   `json:"id"`
	Name                string   `json:"name"`
	ContextLength       int      `json:"context_length"`
	Architecture        Arch     `json:"architecture"`
	Pricing             Price    `json:"pricing"`
	TopProvider         Provider `json:"top_provider"`
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

// Price represents the pricing section of a model
type Price struct {
	Prompt            string `json:"prompt"`
	Completion        string `json:"completion"`
	Request           string `json:"request"`
	Image             string `json:"image"`
	WebSearch         string `json:"web_search"`
	InternalReasoning string `json:"internal_reasoning"`
}

// Provider represents the top_provider section of a model
type Provider struct {
	ContextLength       int  `json:"context_length"`
	MaxCompletionTokens *int `json:"max_completion_tokens"`
	IsModerated         bool `json:"is_moderated"`
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
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		clientIP = ip
	} else if ip := r.Header.Get("X-Real-IP"); ip != "" {
		clientIP = ip
	}

	requestBodyBytes := readRequestBody(w, r)
	if requestBodyBytes == nil {
		return // Error already handled
	}

	// Check if the authorization token is "nostream" and modify streaming behavior accordingly
	nostreamMode := false
	if r.Header.Get("Authorization") == "Bearer nostream" {
		nostreamMode = true
	}

	// Check if streaming is enabled (considering nostream mode)
	isClientStreaming := false
	if !nostreamMode {
		isClientStreaming = checkIfStreaming(requestBodyBytes)
	}
	// If nostreamMode is true, isClientStreaming remains false

	accessToken, targetEndpoint, err := qwenclient.GetValidTokenAndEndpoint()
	if err != nil {
		handleAuthError(w, err)
		return
	}

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
		handleStreamingResponse(responseWriter, resp, r.Context())
	} else {
		handleNonStreamingResponse(responseWriter, resp)
	}

	// Log the request
	duration := time.Since(startTime).Milliseconds()
	logging.NewLogger().ProxyRequestLog(
		clientIP,
		r.Method,
		r.URL.Path,
		len(requestBodyBytes),
		isClientStreaming,
		resp.StatusCode,           // upstream status
		responseWriter.statusCode, // client status
		responseWriter.size,       // response size
		duration,
	)
}

// handleStreamingResponse processes streaming responses with stuttering logic and connection cancellation
func handleStreamingResponse(w *responseWriterWrapper, resp *http.Response, ctx context.Context) {
	// Copy all headers from the upstream response to the client response,
	// deferring to the upstream service to set correct streaming headers.
	for name, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(name, value)
		}
	}
	w.WriteHeader(resp.StatusCode)

	// Deep debug file for capturing raw response
	var debugFile *os.File
	if *deepDebug {
		var err error
		debugFile, err = os.Create("/tmp/qwenproxy_deepdebug.log")
		if err != nil {
			logging.NewLogger().ErrorLog("Failed to create debug file: %v", err)
		}
		defer func() {
			if debugFile != nil {
				debugFile.Close()
			}
		}()
	}

	reader := bufio.NewReader(resp.Body)
	stuttering := true
	buf := "" // Buffered content for stuttering control

	for {
		// Check if client has disconnected
		select {
		case <-ctx.Done():
			logging.NewLogger().DebugLog("Client disconnected during streaming, stopping response")
			return
		default:
		}

		line, err := reader.ReadString('\n')
		if err != nil {
			if err != io.EOF {
				logging.NewLogger().ErrorLog("Error reading from upstream: %v", err)
			}
			break
		}
		logging.NewLogger().DebugLog("Raw upstream line: %s", strings.TrimSpace(line))

		// If the line is empty or only contains whitespace, skip it.
		if len(strings.TrimSpace(line)) == 0 {
			continue
		}

		if stuttering && strings.HasPrefix(line, "data: ") {
			// Extract JSON data from the full line
			data := strings.TrimPrefix(line, "data: ")
			data = strings.TrimRight(data, "\n")
			
			// Determine if stuttering continues
			stillStuttering, stutterErr := stutteringProcess(buf, data)

			if stutterErr != nil {
				logging.NewLogger().ErrorLog("Error in stutteringProcess: %v", stutterErr)
				// If an error occurs, send the current data and stop stuttering
				fmt.Fprintf(w, "data: %s\n\n", data)
				w.Flush()
				stuttering = false // Stop stuttering on error
				buf = ""           // Clear buffer
				
				// Deep debug logging
				if *deepDebug && debugFile != nil {
					debugFile.WriteString(fmt.Sprintf("[ERROR] Stuttering process error: %v\n", stutterErr))
					debugFile.WriteString(fmt.Sprintf("[ERROR] Sending current data: data: %s\n\n", data))
					debugFile.Sync()
				}
				continue
			}
			if stillStuttering {
				// Stuttering continues: update buffer with current data, suppress output
				buf = data  // Buffer just the JSON data, not the full line
				// Deep debug logging
				if *deepDebug && debugFile != nil {
					debugFile.WriteString(fmt.Sprintf("[STUTTERING] Buffering data: %s", data))
					debugFile.Sync()
				}
				continue
			}
			// Stuttering has resolved: flush buffered content then current content
			fmt.Fprintf(w, "data: %s\n\n", buf)   // Flush buffered JSON data with proper formatting
			fmt.Fprintf(w, "data: %s\n\n", data)  // Flush current JSON data with proper formatting
			w.Flush()
			
			// Deep debug logging
			if *deepDebug && debugFile != nil {
				debugFile.WriteString(fmt.Sprintf("[RESOLVED] Flushing buffered: data: %s\n\n", buf))
				debugFile.WriteString(fmt.Sprintf("[RESOLVED] Flushing current: data: %s\n\n", data))
				debugFile.Sync()
			}
			
			stuttering = false // Stuttering has ended
			buf = ""           // Clear buffer
			continue           // Skip the general forwarding logic below
		}
		// forward the line directly (data or non-data)
		if strings.HasPrefix(line, "data: ") {
			// For data lines, extract JSON and format properly
			data := strings.TrimPrefix(line, "data: ")
			data = strings.TrimRight(data, "\n")
			
			// Handle [DONE] message
			if data == "[DONE]" {
				fmt.Fprintf(w, "data: [DONE]\n\n")
				w.Flush()
				if *deepDebug && debugFile != nil {
					debugFile.WriteString("[FORWARD] [DONE] message\n")
					debugFile.Sync()
				}
				break
			}
			
			fmt.Fprintf(w, "data: %s\n\n", data)
		} else {
			// For non-data lines, forward as-is
			fmt.Fprintf(w, "%s", line)
		}
		w.Flush()
		
		// Deep debug logging
		if *deepDebug && debugFile != nil {
			if strings.HasPrefix(line, "data: ") {
				data := strings.TrimPrefix(line, "data: ")
				data = strings.TrimRight(data, "\n")
				if data == "[DONE]" {
					debugFile.WriteString("[FORWARD] [DONE] message\n")
				} else {
					debugFile.WriteString(fmt.Sprintf("[FORWARD] Data line: data: %s\n\n", data))
				}
			} else {
				debugFile.WriteString(fmt.Sprintf("[FORWARD] Non-data line: %s", line))
			}
			debugFile.Sync()
		}

	}
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
				Name:          "Qwen: Qwen3 Coder ",
				ContextLength: 262144,
				Architecture: Arch{
					Modality:         "text->text",
					InputModalities:  []string{"text"},
					OutputModalities: []string{"text"},
					Tokenizer:        "Qwen3",
					InstructType:     nil,
				},
				Pricing: Price{
					Prompt:            "0.0000002",
					Completion:        "0.0000008",
					Request:           "0",
					Image:             "0",
					WebSearch:         "0",
					InternalReasoning: "0",
				},
				TopProvider: Provider{
					ContextLength:       262144,
					MaxCompletionTokens: nil,
					IsModerated:         false,
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
	req.Header.Set("User-Agent", fmt.Sprintf("QwenCode/0.0.9 (%s; %s)", runtime.GOOS, runtime.GOARCH))
	req.Header.Set("X-DashScope-CacheControl", "enable")
	req.Header.Set("X-DashScope-UserAgent", fmt.Sprintf("QwenCode/0.0.9 (%s; %s)", runtime.GOOS, runtime.GOARCH))
	req.Header.Set("X-DashScope-AuthType", "qwen-oauth")
}

// stutteringProcess determines if stuttering is still occurring.
// It assumes stuttering is active when called by ProxyHandler.
// Returns:
//   - true if stuttering continues (meaning the current chunk should be buffered and suppressed).
//   - false if stuttering has resolved (meaning the buffered content and current chunk should be flushed).
//   - err: Any error encountered during processing.
func stutteringProcess(buf string, currentChunkData string) (bool, error) {
	rawCurrentChunk := chunkToJson(currentChunkData)
	if rawCurrentChunk == nil {
		// If current chunk is malformed/uninteresting, consider stuttering resolved for this path,
		// allowing the main handler to decide how to proceed (e.g., just forward).
		return false, nil
	}
	extractedCurrentContent := extractDeltaContent(rawCurrentChunk)

	if buf == "" {
		// This is the very first content chunk, so we consider it part of the stuttering.
		// It will be stored in 'buf' by the calling ProxyHandler.
		return true, nil // Still stuttering
	}

	// 'buf' now holds the JSON string of the previously buffered chunk.
	rawBufferedChunk := chunkToJson(buf)
	if rawBufferedChunk == nil {
		// If buffered chunk is malformed, stuttering cannot be determined.
		// Consider stuttering resolved to avoid blocking.
		return false, nil
	}
	extractedBufferedContent := extractDeltaContent(rawBufferedChunk)

	// Check if current content is a continuation (prefix relationship) of the buffered content.
	if hasPrefixRelationship(extractedCurrentContent, extractedBufferedContent) {
		return true, nil // Still stuttering
	} else {
		return false, nil // Stuttering has resolved
	}
}

func hasPrefixRelationship(a, b string) bool {
	if len(a) < len(b) {
		return strings.HasPrefix(b, a)
	}
	return strings.HasPrefix(a, b)
}

func extractDeltaContent(raw map[string]interface{}) string {
	// it's safe to do this, because raw is validated in chunkToJson
	return raw["choices"].([]interface{})[0].(map[string]interface{})["delta"].(map[string]interface{})["content"].(string)
}

func chunkToJson(chunk string) map[string]interface{} {
	trimmedChunk := strings.TrimSpace(chunk)

	// Special handling for [DONE] message which is not valid JSON
	if trimmedChunk == "[DONE]" {
		return nil
	}

	jsonStr := trimmedChunk // The "data:" prefix should already be removed at this point

	var raw map[string]interface{}
	err := json.Unmarshal([]byte(jsonStr), &raw)
	if err != nil {
		return nil // Malformed JSON, return nil
	}

	// Check for choices[0].delta.content and its length
	if choices, ok := raw["choices"].([]interface{}); ok && len(choices) > 0 {
		if choiceMap, ok := choices[0].(map[string]interface{}); ok {
			if delta, ok := choiceMap["delta"].(map[string]interface{}); ok {
				if _, ok := delta["content"].(string); ok { // Only check if content exists as a string
					return raw
				}
			}
		}
	}

	return nil // Missing required fields or content is not a string, or content is empty
}
