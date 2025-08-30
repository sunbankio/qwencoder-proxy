package streaming

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"qwenproxy/logging"
	"qwenproxy/utils"
)

// HasPrefixRelationship checks if one string is a prefix of the other.
// This is used in stuttering detection logic to determine if two content strings
// have a prefix relationship, which helps identify duplicated or overlapping content
// in streaming responses.
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