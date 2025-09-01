package proxy

import (
	"bufio"
	"context"
	"io"
	"net/http"

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

// DefaultStreamingConfig returns the default streaming configuration
func DefaultStreamingConfig() *StreamingConfig {
	return &StreamingConfig{
		EnableNewArchitecture: false, // Start with false for gradual rollout
		MaxErrors:             10,
		BufferSize:            4096,
		TimeoutSeconds:        900,
	}
}

// HandleStreamingResponseWithConfig handles streaming responses with configurable architecture
func HandleStreamingResponseWithConfig(w *responseWriterWrapper, resp *http.Response, ctx context.Context, config *StreamingConfig) {
	if config == nil {
		config = DefaultStreamingConfig()
	}

	if config.EnableNewArchitecture {
		logging.NewLogger().DebugLog("Using new streaming architecture")
		handleStreamingResponseV2(w, resp, ctx)
	} else {
		logging.NewLogger().DebugLog("Using legacy streaming architecture")
		handleStreamingResponse(w, resp, ctx)
	}
}