package streaming

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"qwenproxy/logging"
	"qwenproxy/utils"
	"net/http" // Add this import for http.ResponseWriter
)

// HasPrefixRelationship checks if one string is a prefix of the other.
func HasPrefixRelationship(a, b string) bool {
	if len(a) < len(b) {
		return strings.HasPrefix(b, a)
	}
	return strings.HasPrefix(a, b)
}

// HandleDoneMessage handles the [DONE] message in the streaming response
func HandleDoneMessage(w http.ResponseWriter, modelName string, rawUsage *Usage, inputTokens, outputTokens int, startTime time.Time) {
	fmt.Fprintf(w, "data: [DONE]\n\n")
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	// Log the final information when the stream ends
	duration := time.Since(startTime).Milliseconds()
	if rawUsage != nil {
		usageBytes, _ := json.Marshal(rawUsage)
		logging.NewLogger().DoneLog("Model: %s, Raw Usage: %s, Duration: %s ms",
			modelName, string(usageBytes), utils.FormatIntWithCommas(duration))
		logging.NewLogger().SeparatorLog()
	} else {
		logging.NewLogger().DoneLog("Model: %s, Input Tokens: %s, Output Tokens: %s, Duration: %s ms",
			modelName, utils.FormatIntWithCommas(int64(inputTokens)), utils.FormatIntWithCommas(int64(outputTokens)), utils.FormatIntWithCommas(duration))
		logging.NewLogger().SeparatorLog()
	}
}

// HandleUsageData processes usage data from streaming chunks
func HandleUsageData(chunk ChatCompletionChunk, inputTokens, outputTokens *int, rawUsage **Usage) {
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

// StutteringHandler is a function that processes incoming data lines
// and handles stuttering by buffering and releasing chunks.
// It returns the data string(s) that should be sent to the client.
type StutteringHandler func(data string) (string, error)

// NewStutteringHandler creates a new StutteringHandler closure.
func NewStutteringHandler() StutteringHandler {
	stuttering := true                 // Flag to indicate if we are in stuttering phase
	stutteringLastDeltaContent := ""   // Last delta content received during stuttering
	stutteringChunkBuf := ""           // Buffer for the current stuttering chunk
	initialSuppressionCount := 0       // Count of content-bearing chunks for initial suppression

	return func(data string) (string, error) {
		if strings.TrimSpace(data) == "[DONE]" {
			return "data: [DONE]\n\n", nil
		}

		var currentChunk ChatCompletionChunk
		if err := json.Unmarshal([]byte(data), &currentChunk); err != nil {
			return "", fmt.Errorf("failed to unmarshal current chunk for stuttering: %w", err)
		}

		currentDeltaContent := ""
		if len(currentChunk.Choices) > 0 {
			currentDeltaContent = currentChunk.Choices[0].Delta.Content
		}

		if stuttering {
			if currentDeltaContent != "" {
				initialSuppressionCount++
			}

			if initialSuppressionCount <= 1 { // Suppress the first content-bearing chunk (Line 1 in streamingtest.txt)
				stutteringLastDeltaContent = currentDeltaContent
				stutteringChunkBuf = data
				return "", nil // Suppress output
			}
			// From here onwards, we are past the initial suppression.
			// The original stuttering logic should apply.

			if HasPrefixRelationship(currentDeltaContent, stutteringLastDeltaContent) {
				// This is stuttering, update buffer and suppress output
				stutteringLastDeltaContent = currentDeltaContent
				stutteringChunkBuf = data // Store the full data string for the chunk
				return "", nil            // Suppress output
			} else {
				// Found genuinely new content, stuttering phase ends
				stuttering = false
				// Return the buffered chunk first, then the current new chunk
				// Ensure that if stutteringChunkBuf is empty (e.g., first chunk was already valid)
				// we only return the current data.
				if stutteringChunkBuf != "" {
					return "data: " + stutteringChunkBuf + "\n\n" + "data: " + data + "\n\n", nil
				} else {
					return "data: " + data + "\n\n", nil
				}
			}
		}

		// If not in stuttering phase, pass through directly
		return "data: " + data + "\n\n", nil
	}
}

// HandleStuttering processes the stuttering logic for streaming chunks
func HandleStuttering(stuttering *bool, stutteringBuf *string, newContent *string) bool {
	if *stuttering {
		if HasPrefixRelationship(*stutteringBuf, *newContent) {
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