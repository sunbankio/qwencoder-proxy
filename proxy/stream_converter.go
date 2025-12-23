package proxy

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/sunbankio/qwencoder-proxy/converter"
	"github.com/sunbankio/qwencoder-proxy/logging"
	"github.com/sunbankio/qwencoder-proxy/provider"
)

// StreamConverter wraps a native provider stream and converts it to OpenAI SSE format
type StreamConverter struct {
	stream    io.ReadCloser
	converter converter.Converter
	model     string
	logger    *logging.Logger
	scanner   *bufio.Scanner
	done      bool
	sentData  bool
}

// NewStreamConverter creates a new stream converter
func NewStreamConverter(stream io.ReadCloser, conv converter.Converter, model string, logger *logging.Logger) *StreamConverter {
	return &StreamConverter{
		stream:    stream,
		converter: conv,
		model:     model,
		logger:    logger,
		scanner:   bufio.NewScanner(stream),
	}
}

// Read implements io.Reader interface
func (sc *StreamConverter) Read(p []byte) (n int, err error) {
	if sc.done {
		return 0, io.EOF
	}

	// Try to read the next chunk from the SSE stream
	chunk, err := sc.readNextSSEChunk()
	if err != nil {
		if err == io.EOF {
			// Send final [DONE] message
			doneMsg := "data: [DONE]\n\n"
			if len(p) < len(doneMsg) {
				return 0, fmt.Errorf("buffer too small for DONE message")
			}
			copy(p, []byte(doneMsg))
			sc.done = true
			return len(doneMsg), nil
		}
		return 0, err
	}

	if len(p) < len(chunk) {
		return 0, fmt.Errorf("buffer too small for chunk")
	}

	copy(p, chunk)
	return len(chunk), nil
}

// Close implements io.Closer interface
func (sc *StreamConverter) Close() error {
	return sc.stream.Close()
}

// readNextSSEChunk reads and converts the next SSE chunk from the native stream
func (sc *StreamConverter) readNextSSEChunk() ([]byte, error) {
	var buffer []string
	
	for sc.scanner.Scan() {
		line := sc.scanner.Text()
		
		if strings.HasPrefix(line, "data: ") {
			// Extract the JSON data after "data: "
			buffer = append(buffer, line[6:])
		} else if line == "" && len(buffer) > 0 {
			// Empty line indicates end of SSE event, process the buffered data
			jsonData := strings.Join(buffer, "\n")
			sc.logger.DebugLog("[StreamConverter] Processing SSE chunk: %s", jsonData)
			
			// Parse the JSON data
			var nativeChunk interface{}
			if err := json.Unmarshal([]byte(jsonData), &nativeChunk); err != nil {
				sc.logger.DebugLog("[StreamConverter] Failed to parse SSE chunk: %v", err)
				buffer = []string{} // Clear buffer and continue
				continue
			}

			// Extract the actual response from the wrapper (for Gemini/Antigravity format)
			var nativeResp interface{}
			if responseMap, ok := nativeChunk.(map[string]interface{}); ok {
				if response, hasResponse := responseMap["response"]; hasResponse {
					// Gemini/Antigravity format: {"response": {...}, "traceId": "..."}
					nativeResp = response
				} else {
					// Direct format
					nativeResp = nativeChunk
				}
			} else {
				nativeResp = nativeChunk
			}

			sc.logger.DebugLog("[StreamConverter] Extracted native response: %+v", nativeResp)

			// Convert to OpenAI format
			openAIChunk, err := sc.converter.ToOpenAIStreamChunk(nativeResp, sc.model)
			if err != nil {
				sc.logger.DebugLog("[StreamConverter] Failed to convert chunk: %v", err)
				buffer = []string{} // Clear buffer and continue
				continue
			}

			sc.logger.DebugLog("[StreamConverter] Converted chunk: %+v", openAIChunk)

			// Marshal to JSON
			chunkBytes, err := json.Marshal(openAIChunk)
			if err != nil {
				sc.logger.DebugLog("[StreamConverter] Failed to marshal chunk: %v", err)
				buffer = []string{} // Clear buffer and continue
				continue
			}

			// Format as SSE
			sseChunk := fmt.Sprintf("data: %s\n\n", string(chunkBytes))
			sc.logger.DebugLog("[StreamConverter] Generated SSE chunk: %s", sseChunk)
			
			buffer = []string{} // Clear buffer for next event
			return []byte(sseChunk), nil
		}
	}

	if err := sc.scanner.Err(); err != nil {
		sc.logger.DebugLog("[StreamConverter] Scanner error: %v", err)
		return nil, err
	}

	sc.logger.DebugLog("[StreamConverter] Stream ended")
	return nil, io.EOF
}

// ConvertedStreamResponse handles streaming with format conversion
func ConvertedStreamResponse(w http.ResponseWriter, r *http.Request, factory *provider.Factory, p provider.Provider, nativeReq interface{}, model string, logger *logging.Logger) error {
	ctx := r.Context()
	stream, err := p.GenerateContentStream(ctx, model, nativeReq)
	if err != nil {
		return err
	}

	// Record success for routing
	factory.RecordSuccess(model, p.Name())
	logger.DebugLog("[Common] Recorded success for streaming provider %s with model %s", p.Name(), model)

	// Get the appropriate converter
	convFactory := converter.NewFactory()
	conv, err := convFactory.Get(p.Protocol())
	if err != nil {
		return fmt.Errorf("failed to get converter for protocol %s: %w", p.Protocol(), err)
	}

	// Create stream converter
	convertedStream := NewStreamConverter(stream, conv, model, logger)
	defer convertedStream.Close()

	// Set streaming headers
	SetStreamingHeaders(w)

	// Copy converted stream to response
	return CopyStreamToResponse(w, convertedStream, logger)
}