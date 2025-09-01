package proxy

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"

	"qwenproxy/logging"
	"qwenproxy/qwenclient"
)

// ProxyHandler handles incoming requests and proxies them to the target endpoint
func ProxyHandler(w http.ResponseWriter, r *http.Request) {
	logging.NewLogger().DebugLog("Incoming Request Content-Length: %d", r.ContentLength)

	var requestBodyBytes []byte
	if r.Body != nil {
		var err error
		requestBodyBytes, err = io.ReadAll(r.Body)
		if err != nil {
			logging.NewLogger().ErrorLog("Failed to read request body: %v", err)
			http.Error(w, "Failed to read request body", http.StatusInternalServerError)
			return
		}
		r.Body = io.NopCloser(bytes.NewBuffer(requestBodyBytes))
	}
	logging.NewLogger().DebugLog("Request Body: %s", string(requestBodyBytes))

	accessToken, targetEndpoint, err := qwenclient.GetValidTokenAndEndpoint()
	if err != nil {
		errorMsg := err.Error()
		if strings.Contains(errorMsg, "credentials not found") || strings.Contains(errorMsg, "failed to refresh token") {
			http.Error(w, fmt.Sprintf("Authentication required: %v. Please restart the proxy to re-authenticate.", err), http.StatusUnauthorized)
			return
		}
		http.Error(w, fmt.Sprintf("Failed to get valid token: %v", err), http.StatusInternalServerError)
		return
	}

	// Construct the targetURL, handling potential duplicate /v1
	requestPath := r.URL.Path
	if strings.HasPrefix(requestPath, "/v1") && strings.HasSuffix(targetEndpoint, "/v1") {
		requestPath = strings.TrimPrefix(requestPath, "/v1")
	}
	targetURL := targetEndpoint + requestPath

	var requestJSON map[string]interface{}
	isClientStreaming := false
	if len(requestBodyBytes) > 0 {
		err = json.Unmarshal(requestBodyBytes, &requestJSON)
		if err == nil {
			if streamVal, ok := requestJSON["stream"].(bool); ok && streamVal {
				isClientStreaming = true
			}
		} else {
			logging.NewLogger().ErrorLog("Failed to unmarshal request body for stream check: %v", err)
		}
	}
	logging.NewLogger().DebugLog("isClientStreaming evaluated to: %t", isClientStreaming)

	req, err := http.NewRequest(r.Method, targetURL, bytes.NewBuffer(requestBodyBytes))
	if err != nil {
		http.Error(w, "Failed to create proxy request", http.StatusInternalServerError)
		return
	}

	SetProxyHeaders(req, accessToken)

	client := SharedHTTPClient

	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to send request to target endpoint: %v", err), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	logging.NewLogger().DebugLog("Upstream Response Status: %s", resp.Status)
	logging.NewLogger().DebugLog("Upstream Response Headers: %v", resp.Header)

	if isClientStreaming {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Cache-Control")
		w.WriteHeader(resp.StatusCode)

		reader := bufio.NewReader(resp.Body)
		stutteringProcessor := stutteringProcess()

		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if err != io.EOF {
					logging.NewLogger().ErrorLog("Error reading from upstream: %v", err)
				}
				break
			}
			logging.NewLogger().DebugLog("Raw upstream line: %s", strings.TrimSpace(line))

			if strings.HasPrefix(line, "data: ") {
				data := strings.TrimPrefix(line, "data: ")
				data = strings.TrimRight(data, "\n")
				logging.NewLogger().DebugLog("Extracted data part: %s", data)

				jsonChunk := chunkToJson(data) // Pass the extracted data, not the raw line
				if jsonChunk == nil {
					logging.NewLogger().DebugLog("chunkToJson returned nil for data: %s", data)
					continue // Skip this malformed or uninteresting chunk
				}
				logging.NewLogger().DebugLog("chunkToJson output: %v", jsonChunk)

				if data == "[DONE]" {
					fmt.Fprintf(w, "data: [DONE]\n\n")
					if flusher, ok := w.(http.Flusher); ok {
						flusher.Flush()
					}
					break
				}

				processedOutput := stutteringProcessor(data) // Pass the extracted data
				logging.NewLogger().DebugLog("stutteringProcessor output: %s", strings.TrimSpace(processedOutput))

				if processedOutput != "" {
					fmt.Fprintf(w, "%s\n", processedOutput)
					if flusher, ok := w.(http.Flusher); ok {
						flusher.Flush()
					}
				}
			} else if strings.HasPrefix(line, "event: ") || strings.HasPrefix(line, ":") || line == "\n" {
				logging.NewLogger().DebugLog("Non-data line: %s", strings.TrimSpace(line))
				fmt.Fprintf(w, "%s", line)
				if flusher, ok := w.(http.Flusher); ok {
					flusher.Flush()
				}
			}
		}
	} else {
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
}

// ModelsHandler handles requests to /v1/models and serves the models.json file
func ModelsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	logging.NewLogger().DebugLog("ModelsHandler received request")
	modelsData, err := os.ReadFile("models.json")
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to read models.json: %v", err), http.StatusInternalServerError)
		logging.NewLogger().ErrorLog("Failed to read models.json: %v", err)
		return
	}

	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(modelsData); err != nil {
		logging.NewLogger().ErrorLog("Error writing models data to response: %v", err)
	}
}

// SetProxyHeaders sets the required headers for the outgoing proxy request.
func SetProxyHeaders(req *http.Request, accessToken string) {
	// Copy headers from original request, but set necessary ones
	for name, values := range req.Header {
		if strings.EqualFold(name, "Authorization") || strings.EqualFold(name, "Content-Type") {
			continue // Handled below or not relevant
		}
		for _, value := range values {
			req.Header.Add(name, value)
		}
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json") // Always JSON for body
	req.Header.Set("User-Agent", fmt.Sprintf("QwenCode/0.0.9 (%s; %s)", runtime.GOOS, runtime.GOARCH))
	req.Header.Set("X-DashScope-CacheControl", "enable")
	req.Header.Set("X-DashScope-UserAgent", fmt.Sprintf("QwenCode/0.0.9 (%s; %s)", runtime.GOOS, runtime.GOARCH))
	req.Header.Set("X-DashScope-AuthType", "qwen-oauth")
}

func stutteringProcess() func(chunk string) string { // Renamed dataChunk back to chunk
	stuttering := true
	buf := ""
	return func(chunk string) string { // Changed dataChunk back to chunk
		if !stuttering {
			return "data: " + chunk + "\n\n"
		}
		raw := chunkToJson(chunk) // Pass chunk directly, not dataChunk
		if len(raw) == 0 {
			return "data: " + chunk + "\n\n"
		}
		extracted := extractDeltaContent(raw)
		if hasPrefixRelationship(extracted, buf) {
			buf = extracted
			return ""
		} else {
			stuttering = false

			modifiedChunk, err := prependDeltaContent(buf, raw)
			if err != nil {
				logging.NewLogger().ErrorLog("Error prepending delta content: %v", err)
				return ""
			}
			return modifiedChunk
		}
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

func prependDeltaContent(buf string, raw map[string]interface{}) (string, error) {
	prefix := "data: "
	// it's safe to do this, because raw is validated in chunkToJson
	raw["choices"].([]interface{})[0].(map[string]interface{})["delta"].(map[string]interface{})["content"] = buf + raw["choices"].([]interface{})[0].(map[string]interface{})["delta"].(map[string]interface{})["content"].(string)

	modifiedChunkBytes, err := json.Marshal(raw)
	if err != nil {
		return "", fmt.Errorf("failed to marshal modified chunk: %w", err)
	}
	return prefix + string(modifiedChunkBytes) + "\n\n", nil
}

func chunkToJson(chunk string) map[string]interface{} {
	trimmedChunk := strings.TrimSpace(chunk)

	// Special handling for [DONE] message which is not valid JSON
	if trimmedChunk == "[DONE]" {
		return nil
	}

	jsonStr := trimmedChunk // The "data:" prefix is already removed at this point

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
