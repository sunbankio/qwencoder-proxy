package proxy

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/sunbankio/qwencoder-proxy/converter"
	"github.com/sunbankio/qwencoder-proxy/logging"
	"github.com/sunbankio/qwencoder-proxy/provider"
)

// GenerateAndConvert handles non-streaming content generation and conversion to OpenAI format
func GenerateAndConvert(ctx context.Context, p provider.Provider, conv converter.Converter, nativeReq interface{}, model string) (interface{}, error) {
	nativeResp, err := p.GenerateContent(ctx, model, nativeReq)
	if err != nil {
		return nil, err
	}
	return conv.ToOpenAIResponse(nativeResp, model)
}

// SetStreamingHeaders sets the necessary HTTP headers for SSE streaming
func SetStreamingHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Transfer-Encoding", "chunked")
}

// CopyStreamToResponse copies blocks from an io.ReadCloser to a http.ResponseWriter with flushing
func CopyStreamToResponse(w http.ResponseWriter, stream io.ReadCloser, logger *logging.Logger) error {
	defer stream.Close()

	flusher, ok := w.(http.Flusher)
	if !ok {
		return fmt.Errorf("streaming not supported")
	}

	buf := make([]byte, 4096)
	for {
		n, err := stream.Read(buf)
		if n > 0 {
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				return writeErr
			}
			flusher.Flush()
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// StreamResponse handles streaming content generation and writing to the response writer
func StreamResponse(w http.ResponseWriter, r *http.Request, factory *provider.Factory, p provider.Provider, nativeReq interface{}, model string, logger *logging.Logger) error {
	ctx := r.Context()
	stream, err := p.GenerateContentStream(ctx, model, nativeReq)
	if err != nil {
		return err
	}

	// Record success for routing
	factory.RecordSuccess(model, p.Name())
	logger.DebugLog("[Common] Recorded success for streaming provider %s with model %s", p.Name(), model)

	// Set streaming headers
	SetStreamingHeaders(w)

	return CopyStreamToResponse(w, stream, logger)
}
