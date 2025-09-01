package proxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestStreamState(t *testing.T) {
	state := NewStreamState()

	// Test initial state
	if state.Current != StateInitial {
		t.Errorf("Expected initial state to be StateInitial, got %s", state.Current.String())
	}

	// Test state transition
	state.TransitionTo(StateStuttering, "test transition")
	if state.Current != StateStuttering {
		t.Errorf("Expected state to be StateStuttering, got %s", state.Current.String())
	}

	// Test chunk increment
	initialCount := state.ChunkCount
	state.IncrementChunk()
	if state.ChunkCount != initialCount+1 {
		t.Errorf("Expected chunk count to be %d, got %d", initialCount+1, state.ChunkCount)
	}

	// Test error increment
	initialErrors := state.ErrorCount
	state.IncrementError()
	if state.ErrorCount != initialErrors+1 {
		t.Errorf("Expected error count to be %d, got %d", initialErrors+1, state.ErrorCount)
	}
}

func TestChunkParser(t *testing.T) {
	parser := NewChunkParser()

	tests := []struct {
		name           string
		input          string
		expectedType   ChunkType
		expectedValid  bool
		expectedContent bool
		expectedText   string
	}{
		{
			name:           "Empty line",
			input:          "",
			expectedType:   ChunkTypeEmpty,
			expectedValid:  false,
			expectedContent: false,
		},
		{
			name:           "Whitespace only",
			input:          "   \n",
			expectedType:   ChunkTypeEmpty,
			expectedValid:  false,
			expectedContent: false,
		},
		{
			name:           "Non-data line",
			input:          "event: message\n",
			expectedType:   ChunkTypeUnknown,
			expectedValid:  true,
			expectedContent: false,
		},
		{
			name:           "DONE message",
			input:          "data: [DONE]\n",
			expectedType:   ChunkTypeDone,
			expectedValid:  true,
			expectedContent: false,
		},
		{
			name:           "Valid data chunk with content",
			input:          `data: {"choices":[{"delta":{"content":"Hello"}}]}` + "\n",
			expectedType:   ChunkTypeData,
			expectedValid:  true,
			expectedContent: true,
			expectedText:   "Hello",
		},
		{
			name:           "Valid data chunk without content",
			input:          `data: {"choices":[{"delta":{"role":"assistant"}}]}` + "\n",
			expectedType:   ChunkTypeData,
			expectedValid:  true,
			expectedContent: false,
		},
		{
			name:           "Malformed JSON",
			input:          `data: {"choices":[{"delta":{"content":"Hello"}` + "\n",
			expectedType:   ChunkTypeMalformed,
			expectedValid:  false,
			expectedContent: false,
		},
		{
			name:           "Invalid structure",
			input:          `data: {"invalid": "structure"}` + "\n",
			expectedType:   ChunkTypeData,
			expectedValid:  true,
			expectedContent: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chunk := parser.Parse(tt.input)

			if chunk.Type != tt.expectedType {
				t.Errorf("Expected type %s, got %s", tt.expectedType.String(), chunk.Type.String())
			}

			if chunk.IsValid != tt.expectedValid {
				t.Errorf("Expected valid %t, got %t", tt.expectedValid, chunk.IsValid)
			}

			if chunk.HasContent != tt.expectedContent {
				t.Errorf("Expected hasContent %t, got %t", tt.expectedContent, chunk.HasContent)
			}

			if tt.expectedContent && chunk.ContentText != tt.expectedText {
				t.Errorf("Expected content text %q, got %q", tt.expectedText, chunk.ContentText)
			}
		})
	}
}

func TestErrorRecoveryManager(t *testing.T) {
	erm := NewErrorRecoveryManager()
	state := NewStreamState()

	tests := []struct {
		name           string
		errorType      ErrorType
		expectedAction RecoveryAction
	}{
		{
			name:           "Malformed JSON should skip",
			errorType:      ErrorMalformedJSON,
			expectedAction: ActionSkip,
		},
		{
			name:           "Invalid chunk should continue",
			errorType:      ErrorInvalidChunk,
			expectedAction: ActionContinue,
		},
		{
			name:           "Parsing failure should skip",
			errorType:      ErrorParsingFailure,
			expectedAction: ActionSkip,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := &UpstreamError{
				Type:        tt.errorType,
				Severity:    SeverityMedium,
				Recoverable: true,
				Message:     "test error",
				Timestamp:   time.Now(),
			}

			action := erm.HandleError(err, state)
			if action != tt.expectedAction {
				t.Errorf("Expected action %d, got %d", tt.expectedAction, action)
			}
		})
	}
}

func TestStreamProcessor_InitialState(t *testing.T) {
	// Create a mock response writer
	recorder := httptest.NewRecorder()
	wrapper := &responseWriterWrapper{
		ResponseWriter: recorder,
		statusCode:     http.StatusOK,
	}

	ctx := context.Background()
	processor := NewStreamProcessor(wrapper, ctx)

	// Test initial state
	if processor.state.Current != StateInitial {
		t.Errorf("Expected initial state to be StateInitial, got %s", processor.state.Current.String())
	}

	// Test processing empty chunk
	err := processor.ProcessLine("")
	if err != nil {
		t.Errorf("Expected no error for empty line, got %v", err)
	}

	// Test processing first content chunk (should enter stuttering)
	err = processor.ProcessLine(`data: {"choices":[{"delta":{"content":"Hello"}}]}`)
	if err != nil {
		t.Errorf("Expected no error for first content chunk, got %v", err)
	}

	if processor.state.Current != StateStuttering {
		t.Errorf("Expected state to be StateStuttering after first content chunk, got %s", processor.state.Current.String())
	}

	if processor.state.Buffer == "" {
		t.Error("Expected buffer to contain first chunk content")
	}
}

func TestStreamProcessor_StutteringFlow(t *testing.T) {
	recorder := httptest.NewRecorder()
	wrapper := &responseWriterWrapper{
		ResponseWriter: recorder,
		statusCode:     http.StatusOK,
	}

	ctx := context.Background()
	processor := NewStreamProcessor(wrapper, ctx)

	// Start with first chunk (enters stuttering)
	err := processor.ProcessLine(`data: {"choices":[{"delta":{"content":"Hello"}}]}`)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Send continuation chunk (should continue stuttering)
	err = processor.ProcessLine(`data: {"choices":[{"delta":{"content":"Hello world"}}]}`)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if processor.state.Current != StateStuttering {
		t.Errorf("Expected to still be stuttering, got %s", processor.state.Current.String())
	}

	// Send non-continuation chunk (should resolve stuttering)
	err = processor.ProcessLine(`data: {"choices":[{"delta":{"content":"Different"}}]}`)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if processor.state.Current != StateNormalFlow {
		t.Errorf("Expected to be in normal flow after stuttering resolved, got %s", processor.state.Current.String())
	}

	// Check that content was written
	output := recorder.Body.String()
	if !strings.Contains(output, "Hello world") {
		t.Error("Expected buffered content to be flushed")
	}
	if !strings.Contains(output, "Different") {
		t.Error("Expected current content to be written")
	}
}

func TestStreamProcessor_ErrorHandling(t *testing.T) {
	recorder := httptest.NewRecorder()
	wrapper := &responseWriterWrapper{
		ResponseWriter: recorder,
		statusCode:     http.StatusOK,
	}

	ctx := context.Background()
	processor := NewStreamProcessor(wrapper, ctx)

	// Test malformed JSON handling
	err := processor.ProcessLine(`data: {"malformed": json`)
	if err != nil {
		t.Errorf("Expected malformed JSON to be handled gracefully, got error: %v", err)
	}

	// Error count should be incremented
	if processor.state.ErrorCount == 0 {
		t.Error("Expected error count to be incremented")
	}
}

func TestStreamProcessor_ClientDisconnection(t *testing.T) {
	recorder := httptest.NewRecorder()
	wrapper := &responseWriterWrapper{
		ResponseWriter: recorder,
		statusCode:     http.StatusOK,
	}

	// Create a cancelled context to simulate client disconnection
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	processor := NewStreamProcessor(wrapper, ctx)

	// Processing should detect cancellation
	err := processor.ProcessLine(`data: {"choices":[{"delta":{"content":"Hello"}}]}`)
	if err == nil {
		t.Error("Expected error due to cancelled context")
	}

	if processor.state.Current != StateTerminating {
		t.Errorf("Expected state to be StateTerminating, got %s", processor.state.Current.String())
	}
}

func TestStreamProcessor_DONEMessage(t *testing.T) {
	recorder := httptest.NewRecorder()
	wrapper := &responseWriterWrapper{
		ResponseWriter: recorder,
		statusCode:     http.StatusOK,
	}

	ctx := context.Background()
	processor := NewStreamProcessor(wrapper, ctx)

	// Send DONE message
	err := processor.ProcessLine("data: [DONE]")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if processor.state.Current != StateTerminating {
		t.Errorf("Expected state to be StateTerminating after DONE, got %s", processor.state.Current.String())
	}

	// Check that DONE was written
	output := recorder.Body.String()
	if !strings.Contains(output, "[DONE]") {
		t.Error("Expected DONE message to be written")
	}
}

func TestStreamProcessor_NonContentChunks(t *testing.T) {
	recorder := httptest.NewRecorder()
	wrapper := &responseWriterWrapper{
		ResponseWriter: recorder,
		statusCode:     http.StatusOK,
	}

	ctx := context.Background()
	processor := NewStreamProcessor(wrapper, ctx)

	// Send non-content data chunk
	err := processor.ProcessLine(`data: {"choices":[{"delta":{"role":"assistant"}}]}`)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Should go directly to normal flow (no stuttering for non-content)
	if processor.state.Current != StateNormalFlow {
		t.Errorf("Expected state to be StateNormalFlow for non-content chunk, got %s", processor.state.Current.String())
	}

	// Check that chunk was forwarded
	output := recorder.Body.String()
	if !strings.Contains(output, "role") {
		t.Error("Expected non-content chunk to be forwarded")
	}
}

// Benchmark tests
func BenchmarkChunkParser_Parse(b *testing.B) {
	parser := NewChunkParser()
	testLine := `data: {"choices":[{"delta":{"content":"Hello world"}}]}`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		parser.Parse(testLine)
	}
}

func BenchmarkStreamProcessor_ProcessLine(b *testing.B) {
	recorder := httptest.NewRecorder()
	wrapper := &responseWriterWrapper{
		ResponseWriter: recorder,
		statusCode:     http.StatusOK,
	}

	ctx := context.Background()
	processor := NewStreamProcessor(wrapper, ctx)
	testLine := `data: {"choices":[{"delta":{"content":"Hello world"}}]}`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = processor.ProcessLine(testLine)
	}
}