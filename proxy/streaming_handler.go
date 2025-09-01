package proxy

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"os"
	"strconv"

	"github.com/sunbankio/qwencoder-proxy/logging"
)

// handleStreamingResponseV2 is the new streaming handler using the refactored architecture
// This is a drop-in replacement for the original handleStreamingResponse function
func handleStreamingResponseV2(w *responseWriterWrapper, resp *http.Response, ctx context.Context) {
	logger := logging.NewLogger()
	
	// Copy all headers from the upstream response to the client response,
	// deferring to the upstream service to set correct streaming headers.
	for name, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(name, value)
		}
	}
	w.WriteHeader(resp.StatusCode)

	// Create the new stream processor
	processor := NewStreamProcessor(w, ctx)
	reader := bufio.NewReader(resp.Body)

	logger.DebugLog("Starting streaming response processing with new architecture")

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err != io.EOF {
				logger.ErrorLog("Error reading from upstream: %v", err)
			}
			break
		}

		// Process the line using the new architecture
		if err := processor.ProcessLine(line); err != nil {
			// Check if it's a context cancellation (client disconnect)
			if err == context.Canceled {
				logger.DebugLog("Client disconnected, stopping stream processing")
				return
			}
			
			// For other errors, log and continue processing if possible
			logger.ErrorLog("Error processing stream line: %v", err)
			
			// If the processor is in terminating state, break the loop
			if processor.state.Current == StateTerminating {
				break
			}
		}

		// If processor is in terminating state, break the loop
		if processor.state.Current == StateTerminating {
			break
		}
	}

	// Log final statistics
	logger.DebugLog("Stream processing completed. Chunks processed: %d, Errors: %d, Duration: %v",
		processor.state.ChunkCount,
		processor.state.ErrorCount,
		processor.state.LastValidChunk.Sub(processor.state.StartTime))
}

// StreamingConfig holds configuration for the streaming handler
type StreamingConfig struct {
	EnableNewArchitecture bool
	MaxErrors             int
	BufferSize            int
	TimeoutSeconds        int
}

// DefaultStreamingConfig returns the default streaming configuration with environment variable support
func DefaultStreamingConfig() *StreamingConfig {
	// Check environment variable for new architecture enablement
	enableNew := false
	if envVal := os.Getenv("ENABLE_NEW_STREAMING_ARCHITECTURE"); envVal != "" {
		if val, err := strconv.ParseBool(envVal); err == nil {
			enableNew = val
		}
	}

	// Check for gradual rollout percentage
	rolloutPercentage := 0
	if envVal := os.Getenv("NEW_STREAMING_ROLLOUT_PERCENTAGE"); envVal != "" {
		if val, err := strconv.Atoi(envVal); err == nil && val >= 0 && val <= 100 {
			rolloutPercentage = val
		}
	}

	// If rollout percentage is set and we're not explicitly enabled, use percentage
	if rolloutPercentage > 0 && !enableNew {
		// Simple deterministic rollout - in production you'd use request hash or user ID
		// For now, enable if percentage >= 50 (simplified demo)
		enableNew = (rolloutPercentage >= 50)
	}

	return &StreamingConfig{
		EnableNewArchitecture: enableNew,
		MaxErrors:             getEnvInt("STREAMING_MAX_ERRORS", 10),
		BufferSize:            getEnvInt("STREAMING_BUFFER_SIZE", 4096),
		TimeoutSeconds:        getEnvInt("STREAMING_TIMEOUT_SECONDS", 900),
	}
}

// getEnvInt gets an integer environment variable with a default value
func getEnvInt(key string, defaultValue int) int {
	if envVal := os.Getenv(key); envVal != "" {
		if val, err := strconv.Atoi(envVal); err == nil {
			return val
		}
	}
	return defaultValue
}

// HandleStreamingResponseWithConfig handles streaming responses with configurable architecture
func HandleStreamingResponseWithConfig(w *responseWriterWrapper, resp *http.Response, ctx context.Context, config *StreamingConfig) {
	if config == nil {
		config = DefaultStreamingConfig()
	}

	logger := logging.NewLogger()
	
	if config.EnableNewArchitecture {
		logger.InfoLog("Using new streaming architecture (v2)")
		handleStreamingResponseV2(w, resp, ctx)
	} else {
		logger.DebugLog("Using legacy streaming architecture (v1)")
		handleStreamingResponse(w, resp, ctx)
	}
}

// GetStreamingConfig returns the current streaming configuration (for monitoring/debugging)
func GetStreamingConfig() *StreamingConfig {
	return DefaultStreamingConfig()
}