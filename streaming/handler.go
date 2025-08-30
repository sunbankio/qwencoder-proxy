package streaming

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"time"

	"qwenproxy/logging"
	"qwenproxy/utils"
)

// StreamProxyHandler handles incoming requests and proxies them to the target endpoint for streaming
func StreamProxyHandler(w http.ResponseWriter, r *http.Request, accessToken, targetEndpoint string, originalBody map[string]interface{}, startTime time.Time, client *http.Client) {
	// Variables to track usage information
	inputTokens := 0
	outputTokens := 0
	var rawUsage *Usage // Variable to store raw usage structure
	modelName := ""

	// Initialize with values from the original request if available
	if model, ok := originalBody["model"].(string); ok {
		modelName = model
	}

	// Create a context with a timeout for the upstream request
	ctx, cancel := context.WithTimeout(r.Context(), StreamingTimeoutSeconds*time.Second) // 90 seconds timeout
	defer cancel()                                                                       // Ensure the context is cancelled to release resources

	// For streaming requests, set the stream flag to true for the upstream request
	originalBody["stream"] = true
	// Ensure stream_options includes usage information
	streamOptions, ok := originalBody["stream_options"].(map[string]interface{})
	if !ok {
		streamOptions = make(map[string]interface{})
	}
	streamOptions["include_usage"] = true
	originalBody["stream_options"] = streamOptions

	// Marshal the modified body for logging purposes only
	modifiedBodyBytes, err := json.Marshal(originalBody)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to marshal streaming request body: %v", err), http.StatusInternalServerError)
		return
	}
	logging.NewLogger().StreamLog("%s bytes %s to %s", utils.FormatIntWithCommas(int64(len(modifiedBodyBytes))), r.Method, r.URL.Path)

	// Create a new request to the target endpoint with the modified request body for streaming
	req, err := http.NewRequestWithContext(ctx, r.Method, targetEndpoint, bytes.NewBuffer(modifiedBodyBytes))
	if err != nil {
		http.Error(w, "Failed to create proxy request with original body", http.StatusInternalServerError)
		return
	}
	// Set ContentLength to -1 to indicate that we don't know the length (streaming)
	req.ContentLength = -1

	// Copy headers from original request, but set necessary ones
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json") // Always JSON for body
	req.Header.Set("User-Agent", fmt.Sprintf("QwenCode/0.0.9 (%s; %s)", runtime.GOOS, runtime.GOARCH))
	req.Header.Set("X-DashScope-CacheControl", "enable")
	req.Header.Set("X-DashScope-UserAgent", fmt.Sprintf("QwenCode/0.0.9 (%s; %s)", runtime.GOOS, runtime.GOARCH))
	req.Header.Set("X-DashScope-AuthType", "qwen-oauth")

	// Use the shared HTTP client with connection pool optimization and timeout

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

	// Set necessary headers for SSE to the client
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Cache-Control")
	w.WriteHeader(resp.StatusCode) // Use upstream status code

	reader := bufio.NewReader(resp.Body)
	stuttering := true  // Flag to indicate if we are in stuttering phase
	stutteringBuf := "" // To accumulate content during stuttering phase

	// Use a channel to communicate lines from the upstream reader
	lineChan := make(chan string)
	errChan := make(chan error)

	go func() {
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				errChan <- err
				return
			}
			lineChan <- line
		}
	}()

	// Create a timer for read timeout
	readTimeout := time.NewTimer(time.Duration(ReadTimeoutSeconds) * time.Second)
	defer readTimeout.Stop()

	for {
		select {
		case <-ctx.Done():
			// Client disconnected, stop proxying
			logging.NewLogger().WarningLog("Client disconnected: %v", ctx.Err())
			return // Exit the handler
		case <-readTimeout.C:
			// Read timeout exceeded
			logging.NewLogger().WarningLog("Read timeout exceeded (%d seconds)", ReadTimeoutSeconds)
			return // Exit the handler
		case err := <-errChan:
			if err != io.EOF {
				logging.NewLogger().ErrorLog("Error reading from upstream: %v", err)
			}
			// Call HandleDoneMessage to send the final [DONE] message and log usage information
			HandleDoneMessage(w, modelName, rawUsage, inputTokens, outputTokens, startTime)
			return // Exit the handler
		case line := <-lineChan:
			// Reset the read timeout timer
			if !readTimeout.Stop() {
				select {
				case <-readTimeout.C:
				default:
				}
			}
			readTimeout.Reset(time.Duration(ReadTimeoutSeconds) * time.Second)

			// Process the line from the upstream response
			if strings.HasPrefix(line, "data: ") {
				data := strings.TrimPrefix(line, "data: ")
				data = strings.TrimRight(data, "\n")

				if data == "[DONE]" {
					// Handle the [DONE] message
					HandleDoneMessage(w, modelName, rawUsage, inputTokens, outputTokens, startTime)
					return // Exit the handler
				}

				// Try to unmarshal the data into a ChatCompletionChunk
				var chunk ChatCompletionChunk
				if err := json.Unmarshal([]byte(data), &chunk); err != nil {
					logging.NewLogger().ErrorLog("Failed to unmarshal chunk: %v, data: %s", err, data)
					continue // Skip this chunk and continue with the next one
				}

				// Handle special case: final usage chunk with empty choices array
				// Also handle standard chunks with usage information
				HandleUsageData(chunk, &inputTokens, &outputTokens, &rawUsage)

				// Process the content of the chunk
				if len(chunk.Choices) > 0 {
					newContent := chunk.Choices[0].Delta.Content
					// Check if we are in stuttering phase and handle it
					if HandleStuttering(&stuttering, &stutteringBuf, &newContent) {
						continue // Skip sending this chunk
					}
					// If newContent is not empty, send it to the client as a properly formatted JSON object
					if newContent != "" {
						// Create a ChatCompletionChunk object with the new content and model name
						responseChunk := ChatCompletionChunk{
							Choices: []Choice{
								{
									Delta: Delta{
										Content: newContent,
									},
								},
							},
							Model: modelName,
						}
						// Marshal the response chunk to JSON
						responseBytes, err := json.Marshal(responseChunk)
						if err != nil {
							logging.NewLogger().ErrorLog("Failed to marshal response chunk: %v", err)
							continue // Skip this chunk and continue with the next one
						}
						// Send the JSON object with the data: prefix
						fmt.Fprintf(w, "data: %s\n\n", responseBytes)
						if flusher, ok := w.(http.Flusher); ok {
							flusher.Flush()
						}
					}
				}
			} else if strings.HasPrefix(line, "event: ") {
				// Pass through event lines
				fmt.Fprintf(w, "%s", line)
				if flusher, ok := w.(http.Flusher); ok {
					flusher.Flush()
				}
			} else if strings.HasPrefix(line, ":") {
				// Pass through comment lines (lines starting with ':')
				fmt.Fprintf(w, "%s", line)
				if flusher, ok := w.(http.Flusher); ok {
					flusher.Flush()
				}
			} else if line == "\n" {
				// Pass through empty lines
				fmt.Fprintf(w, "\n")
				if flusher, ok := w.(http.Flusher); ok {
					flusher.Flush()
				}
			}
		}
	}
}