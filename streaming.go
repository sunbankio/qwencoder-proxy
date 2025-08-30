package main

import (
	"bufio"
	"bytes" // Added for non-streaming requests
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"runtime" // Added runtime import
	"strings"
	"time"
)

// Define structs to match the OpenAI API response structure for streaming
type ChatCompletionChunk struct {
	Choices []Choice `json:"choices"`
	Usage   *Usage   `json:"usage,omitempty"`
	Model   string   `json:"model,omitempty"`
}

type Choice struct {
	Delta        Delta  `json:"delta"`
	FinishReason string `json:"finish_reason,omitempty"`
}

type Delta struct {
	Content string `json:"content"`
}

type Usage struct {
	PromptTokens        int            `json:"prompt_tokens,omitempty"`
	CompletionTokens    int            `json:"completion_tokens,omitempty"`
	TotalTokens         int            `json:"total_tokens,omitempty"`
	PromptTokensDetails *TokensDetails `json:"prompt_tokens_details,omitempty"`
	// Note: The upstream API doesn't seem to send completion_tokens_details
	// but we'll keep this field in case it's added in the future
	CompletionTokensDetails *TokensDetails `json:"completion_tokens_details,omitempty"`
}

type TokensDetails struct {
	CacheType       string `json:"cache_type,omitempty"`
	CachedTokens    int    `json:"cached_tokens,omitempty"`
	ReasoningTokens int    `json:"reasoning_tokens,omitempty"`
}

// hasPrefixRelationship checks if one string is a prefix of the other.
// This is used in stuttering detection logic to determine if two content strings
// have a prefix relationship, which helps identify duplicated or overlapping content
// in streaming responses.
func hasPrefixRelationship(a, b string) bool {
	if len(a) < len(b) {
		return strings.HasPrefix(b, a)
	}
	return strings.HasPrefix(a, b)
}

// handleDoneMessage handles the [DONE] message in the streaming response
func handleDoneMessage(w http.ResponseWriter, modelName string, rawUsage *Usage, inputTokens, outputTokens int, startTime time.Time) {
	fmt.Fprintf(w, "data: [DONE]\n\n")
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	// Log the final information when the stream ends
	duration := time.Since(startTime).Milliseconds()
	if rawUsage != nil {
		usageBytes, _ := json.Marshal(rawUsage)
		log.Printf("[DONE] - Model: %s, Raw Usage: %s, Duration: %s ms\n====================================",
			modelName, string(usageBytes), formatIntWithCommas(duration))
	} else {
		log.Printf("[DONE] - Model: %s, Input Tokens: %s, Output Tokens: %s, Duration: %s ms\n====================================",
			modelName, formatIntWithCommas(int64(inputTokens)), formatIntWithCommas(int64(outputTokens)), formatIntWithCommas(duration))
	}
}

// handleUsageData processes usage data from streaming chunks
func handleUsageData(chunk ChatCompletionChunk, inputTokens, outputTokens *int, rawUsage **Usage) {
	// Handle special case: final usage chunk with empty choices array
	if len(chunk.Choices) == 0 && chunk.Usage != nil {
		// This is a final usage chunk, update our token counts
		if chunk.Usage.PromptTokens > 0 {
			*inputTokens = chunk.Usage.PromptTokens
		}
		if chunk.Usage.CompletionTokens > 0 {
			*outputTokens = chunk.Usage.CompletionTokens
		}
		// Fallback to TotalTokens if individual token counts are not available
		if *inputTokens == 0 && *outputTokens == 0 && chunk.Usage.TotalTokens > 0 {
			// We can't distinguish between input and output tokens, so we'll use TotalTokens as output
			*outputTokens = chunk.Usage.TotalTokens
		}
		// Store the raw usage structure
		*rawUsage = chunk.Usage
		return
	}

	// Track token usage if available (for standard chunks with non-null usage)
	if chunk.Usage != nil {
		if chunk.Usage.PromptTokens > 0 {
			*inputTokens = chunk.Usage.PromptTokens
		}
		if chunk.Usage.CompletionTokens > 0 {
			*outputTokens = chunk.Usage.CompletionTokens
		}
		// Fallback to TotalTokens if individual token counts are not available
		if *inputTokens == 0 && *outputTokens == 0 && chunk.Usage.TotalTokens > 0 {
			// We can't distinguish between input and output tokens, so we'll use TotalTokens as output
			*outputTokens = chunk.Usage.TotalTokens
		}
		// Store the raw usage structure
		*rawUsage = chunk.Usage
	}
}

// handleStuttering processes the stuttering logic for streaming chunks
func handleStuttering(stuttering *bool, stutteringBuf *string, newContent *string) bool {
	if *stuttering {
		if hasPrefixRelationship(*stutteringBuf, *newContent) {
			// If it's a duplicate/prefix, buffer it and don't send anything yet
			*stutteringBuf = *newContent // Update buffer with new (possibly duplicated) content
			return true                  // Skip sending this chunk
		} else {
			// Found genuinely new content, stuttering phase ends
			*stuttering = false
			// Prepend the buffered content to the new content
			*newContent = *stutteringBuf + *newContent
			*stutteringBuf = "" // Clear buffer
		}
	}
	return false // Don't skip sending this chunk
}

// StreamProxyHandler handles incoming requests and proxies them to the target endpoint for streaming
func StreamProxyHandler(w http.ResponseWriter, r *http.Request, accessToken, targetEndpoint string, originalBody map[string]interface{}, startTime time.Time) {
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
	log.Printf("[REQUESTING]: %s bytes %s to %s", formatIntWithCommas(int64(len(modifiedBodyBytes))), r.Method, r.URL.Path)

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
	client := SharedHTTPClient

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
			log.Printf("Client disconnected: %v", ctx.Err())
			return // Exit the handler
		case <-readTimeout.C:
			// Read timeout exceeded
			log.Printf("Read timeout exceeded (%d seconds)", ReadTimeoutSeconds)
			http.Error(w, fmt.Sprintf("Read timeout exceeded (%d seconds)", ReadTimeoutSeconds), http.StatusRequestTimeout)
			return // Exit the handler
		case line := <-lineChan:
			// Remove the debug logging of raw lines
			// log.Printf("DEBUG_UPSTREAM_RAW_LINE: %s", strings.TrimSpace(line))

			if strings.HasPrefix(line, "data: ") {
				data := strings.TrimPrefix(line, "data: ")
				data = strings.TrimSpace(data)

				// Handle [DONE] message properly
				if data == "[DONE]" {
					handleDoneMessage(w, modelName, rawUsage, inputTokens, outputTokens, startTime)
					return // Exit on [DONE]
				}

				var chunk ChatCompletionChunk
				unmarshalErr := json.Unmarshal([]byte(data), &chunk)
				if unmarshalErr != nil {
					log.Printf("Error unmarshalling streaming JSON: %v, Data: %s", unmarshalErr, data)
					continue
				}

				// Log the raw data for final usage chunks (where choices array is empty)
				// if len(chunk.Choices) == 0 && chunk.Usage != nil {
				// 	log.Printf("DEBUG_UPSTREAM_FINAL_USAGE_RAW: %s", data)
				// }

				// Handle usage data
				handleUsageData(chunk, &inputTokens, &outputTokens, &rawUsage)

				// Update model name if available in the chunk
				if chunk.Model != "" {
					modelName = chunk.Model
				}

				if len(chunk.Choices) > 0 {
					newContent := chunk.Choices[0].Delta.Content

					// Handle stuttering logic
					if handleStuttering(&stuttering, &stutteringBuf, &newContent) {
						continue // Skip sending this chunk
					}

					// If not stuttering phase, or if stuttering just ended, send the chunk
					// For the combined initial chunk, or subsequent non-stuttering chunks
					// Re-marshal the chunk with the potentially modified newContent
					chunk.Choices[0].Delta.Content = newContent
					updatedData, marshalErr := json.Marshal(chunk)
					if marshalErr != nil {
						log.Printf("Error re-marshalling chunk: %v", marshalErr)
						continue
					}
					fmt.Fprintf(w, "data: %s\n\n", updatedData)
					if flusher, ok := w.(http.Flusher); ok {
						flusher.Flush()
					}
				}
			}
			// Reset the read timeout timer since we received a chunk
			if !readTimeout.Stop() {
				select {
				case <-readTimeout.C:
				default:
				}
			}
			readTimeout.Reset(time.Duration(ReadTimeoutSeconds) * time.Second)
		case err := <-errChan:
			if err == io.EOF {
				// Use our helper function to handle the DONE message
				handleDoneMessage(w, modelName, rawUsage, inputTokens, outputTokens, startTime)
				log.Println("Upstream stream ended.")
			} else {
				log.Printf("Error reading stream from upstream: %v", err)
			}
			return // Exit the handler
		}
	}
}
