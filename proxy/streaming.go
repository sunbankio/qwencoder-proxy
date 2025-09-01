package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/sunbankio/qwencoder-proxy/logging"
)

// Phase 1.1: Stream State Manager

// StreamingState represents the current state of stream processing
type StreamingState int

const (
	StateInitial StreamingState = iota
	StateStuttering
	StateNormalFlow
	StateRecovering
	StateTerminating
)

func (s StreamingState) String() string {
	switch s {
	case StateInitial:
		return "Initial"
	case StateStuttering:
		return "Stuttering"
	case StateNormalFlow:
		return "NormalFlow"
	case StateRecovering:
		return "Recovering"
	case StateTerminating:
		return "Terminating"
	default:
		return "Unknown"
	}
}

// StreamState holds the current state of stream processing
type StreamState struct {
	Current        StreamingState
	IsStuttering   bool
	Buffer         string
	ChunkCount     int
	ErrorCount     int
	LastValidChunk time.Time
	StartTime      time.Time
}

// NewStreamState creates a new stream state
func NewStreamState() *StreamState {
	return &StreamState{
		Current:        StateInitial,
		IsStuttering:   false,
		Buffer:         "",
		ChunkCount:     0,
		ErrorCount:     0,
		LastValidChunk: time.Now(),
		StartTime:      time.Now(),
	}
}

// TransitionTo changes the state and logs the transition
func (s *StreamState) TransitionTo(newState StreamingState, reason string) {
	oldState := s.Current
	s.Current = newState
	logging.NewLogger().DebugLog("State transition: %s -> %s (reason: %s)", 
		oldState.String(), newState.String(), reason)
}

// IncrementChunk increments the chunk counter and updates last valid chunk time
func (s *StreamState) IncrementChunk() {
	s.ChunkCount++
	s.LastValidChunk = time.Now()
}

// IncrementError increments the error counter
func (s *StreamState) IncrementError() {
	s.ErrorCount++
}

// Phase 1.2: Robust Chunk Parser

// ChunkType represents the type of chunk received
type ChunkType int

const (
	ChunkTypeData ChunkType = iota
	ChunkTypeDone
	ChunkTypeMalformed
	ChunkTypeEmpty
	ChunkTypeUnknown
)

func (c ChunkType) String() string {
	switch c {
	case ChunkTypeData:
		return "Data"
	case ChunkTypeDone:
		return "Done"
	case ChunkTypeMalformed:
		return "Malformed"
	case ChunkTypeEmpty:
		return "Empty"
	case ChunkTypeUnknown:
		return "Unknown"
	default:
		return "Invalid"
	}
}

// ParsedChunk represents a parsed chunk with metadata
type ParsedChunk struct {
	Type        ChunkType
	RawLine     string
	Content     string
	IsValid     bool
	Error       error
	Metadata    map[string]interface{}
	ParsedAt    time.Time
	HasContent  bool
	ContentText string
}

// ChunkParser handles parsing of streaming chunks
type ChunkParser struct {
	maxRetries int
	logger     *logging.Logger
}

// NewChunkParser creates a new chunk parser
func NewChunkParser() *ChunkParser {
	return &ChunkParser{
		maxRetries: 3,
		logger:     logging.NewLogger(),
	}
}

// Parse parses a raw line into a ParsedChunk
func (cp *ChunkParser) Parse(rawLine string) *ParsedChunk {
	chunk := &ParsedChunk{
		RawLine:  rawLine,
		ParsedAt: time.Now(),
		Metadata: make(map[string]interface{}),
	}

	// Handle empty or whitespace-only lines
	trimmed := strings.TrimSpace(rawLine)
	if len(trimmed) == 0 {
		chunk.Type = ChunkTypeEmpty
		chunk.IsValid = false
		return chunk
	}

	// Handle non-data lines (pass through)
	if !strings.HasPrefix(rawLine, "data: ") {
		chunk.Type = ChunkTypeUnknown
		chunk.Content = rawLine
		chunk.IsValid = true
		return chunk
	}

	// Extract data content
	data := strings.TrimPrefix(rawLine, "data: ")
	data = strings.TrimRight(data, "\n")
	chunk.Content = data

	// Handle [DONE] message
	if data == "[DONE]" {
		chunk.Type = ChunkTypeDone
		chunk.IsValid = true
		return chunk
	}

	// Try to parse as JSON
	var jsonData map[string]interface{}
	if err := json.Unmarshal([]byte(data), &jsonData); err != nil {
		chunk.Type = ChunkTypeMalformed
		chunk.Error = fmt.Errorf("failed to parse JSON: %w", err)
		chunk.IsValid = false
		cp.logger.DebugLog("Malformed JSON chunk: %s, error: %v", data, err)
		return chunk
	}

	// Validate structure and extract content
	if content, hasContent := cp.extractContentFromJSON(jsonData); hasContent {
		chunk.Type = ChunkTypeData
		chunk.IsValid = true
		chunk.HasContent = true
		chunk.ContentText = content
		chunk.Metadata = jsonData
	} else {
		chunk.Type = ChunkTypeData
		chunk.IsValid = true
		chunk.HasContent = false
		chunk.Metadata = jsonData
	}

	return chunk
}

// extractContentFromJSON extracts content from the JSON structure
func (cp *ChunkParser) extractContentFromJSON(jsonData map[string]interface{}) (string, bool) {
	choices, ok := jsonData["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		return "", false
	}

	choiceMap, ok := choices[0].(map[string]interface{})
	if !ok {
		return "", false
	}

	delta, ok := choiceMap["delta"].(map[string]interface{})
	if !ok {
		return "", false
	}

	content, ok := delta["content"].(string)
	if !ok {
		return "", false
	}

	return content, true
}

// chunkToJson parses a JSON string and validates its structure
func (cp *ChunkParser) chunkToJson(chunk string) map[string]interface{} {
	trimmedChunk := strings.TrimSpace(chunk)

	// Special handling for [DONE] message which is not valid JSON
	if trimmedChunk == "[DONE]" {
		return nil
	}

	var raw map[string]interface{}
	err := json.Unmarshal([]byte(trimmedChunk), &raw)
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

// Phase 1.3: Error Recovery Manager

// ErrorType represents different types of errors that can occur
type ErrorType int

const (
	ErrorMalformedJSON ErrorType = iota
	ErrorNetworkTimeout
	ErrorConnectionLost
	ErrorInvalidChunk
	ErrorUpstreamOverload
	ErrorParsingFailure
)

func (e ErrorType) String() string {
	switch e {
	case ErrorMalformedJSON:
		return "MalformedJSON"
	case ErrorNetworkTimeout:
		return "NetworkTimeout"
	case ErrorConnectionLost:
		return "ConnectionLost"
	case ErrorInvalidChunk:
		return "InvalidChunk"
	case ErrorUpstreamOverload:
		return "UpstreamOverload"
	case ErrorParsingFailure:
		return "ParsingFailure"
	default:
		return "Unknown"
	}
}

// ErrorSeverity represents the severity of an error
type ErrorSeverity int

const (
	SeverityLow ErrorSeverity = iota
	SeverityMedium
	SeverityHigh
	SeverityCritical
)

// UpstreamError represents an error from upstream processing
type UpstreamError struct {
	Type        ErrorType
	Severity    ErrorSeverity
	Recoverable bool
	RetryAfter  time.Duration
	Message     string
	Cause       error
	Timestamp   time.Time
}

// Error implements the error interface
func (ue *UpstreamError) Error() string {
	return fmt.Sprintf("[%s] %s: %s", ue.Type.String(), ue.Message, ue.Cause)
}

// RecoveryStrategy defines how to handle different types of errors
type RecoveryStrategy func(error *UpstreamError, state *StreamState) RecoveryAction

// RecoveryAction represents the action to take for error recovery
type RecoveryAction int

const (
	ActionContinue RecoveryAction = iota
	ActionRetry
	ActionSkip
	ActionTerminate
	ActionBuffer
)

// ErrorRecoveryManager manages error recovery strategies
type ErrorRecoveryManager struct {
	maxErrors          int
	recoveryStrategies map[ErrorType]RecoveryStrategy
	logger             *logging.Logger
}

// NewErrorRecoveryManager creates a new error recovery manager
func NewErrorRecoveryManager() *ErrorRecoveryManager {
	erm := &ErrorRecoveryManager{
		maxErrors:          10,
		recoveryStrategies: make(map[ErrorType]RecoveryStrategy),
		logger:             logging.NewLogger(),
	}

	// Set up default recovery strategies
	erm.setupDefaultStrategies()
	return erm
}

// setupDefaultStrategies sets up the default recovery strategies
func (erm *ErrorRecoveryManager) setupDefaultStrategies() {
	// Malformed JSON: Skip and continue
	erm.recoveryStrategies[ErrorMalformedJSON] = func(err *UpstreamError, state *StreamState) RecoveryAction {
		erm.logger.WarningLog("Skipping malformed JSON chunk: %s", err.Message)
		return ActionSkip
	}

	// Network timeout: Retry with backoff
	erm.recoveryStrategies[ErrorNetworkTimeout] = func(err *UpstreamError, state *StreamState) RecoveryAction {
		if state.ErrorCount < 3 {
			erm.logger.WarningLog("Network timeout, retrying: %s", err.Message)
			return ActionRetry
		}
		erm.logger.ErrorLog("Too many network timeouts, terminating: %s", err.Message)
		return ActionTerminate
	}

	// Invalid chunk: Continue processing
	erm.recoveryStrategies[ErrorInvalidChunk] = func(err *UpstreamError, state *StreamState) RecoveryAction {
		erm.logger.DebugLog("Invalid chunk encountered, continuing: %s", err.Message)
		return ActionContinue
	}

	// Parsing failure: Skip and continue
	erm.recoveryStrategies[ErrorParsingFailure] = func(err *UpstreamError, state *StreamState) RecoveryAction {
		erm.logger.WarningLog("Parsing failure, skipping chunk: %s", err.Message)
		return ActionSkip
	}
}

// HandleError processes an error and returns the appropriate recovery action
func (erm *ErrorRecoveryManager) HandleError(err *UpstreamError, state *StreamState) RecoveryAction {
	strategy, exists := erm.recoveryStrategies[err.Type]
	if !exists {
		erm.logger.ErrorLog("No recovery strategy for error type: %s", err.Type.String())
		return ActionTerminate
	}

	return strategy(err, state)
}

// StreamProcessor coordinates the stream processing components
type StreamProcessor struct {
	state        *StreamState
	parser       *ChunkParser
	recovery     *ErrorRecoveryManager
	writer       *responseWriterWrapper
	ctx          context.Context
	logger       *logging.Logger
}

// NewStreamProcessor creates a new stream processor
func NewStreamProcessor(writer *responseWriterWrapper, ctx context.Context) *StreamProcessor {
	return &StreamProcessor{
		state:    NewStreamState(),
		parser:   NewChunkParser(),
		recovery: NewErrorRecoveryManager(),
		writer:   writer,
		ctx:      ctx,
		logger:   logging.NewLogger(),
	}
}

// ProcessLine processes a single line from the upstream
func (sp *StreamProcessor) ProcessLine(rawLine string) error {
	// Check for client disconnection
	select {
	case <-sp.ctx.Done():
		sp.state.TransitionTo(StateTerminating, "client disconnected")
		sp.logger.DebugLog("Client disconnected during streaming, stopping response")
		return sp.ctx.Err()
	default:
	}

	// Parse the chunk
	chunk := sp.parser.Parse(rawLine)
	sp.logger.DebugLog("Parsed chunk: type=%s, valid=%t, hasContent=%t", 
		chunk.Type.String(), chunk.IsValid, chunk.HasContent)

	// Handle based on current state
	switch sp.state.Current {
	case StateInitial:
		return sp.handleInitialChunk(chunk)
	case StateStuttering:
		return sp.handleStutteringChunk(chunk)
	case StateNormalFlow:
		return sp.handleNormalChunk(chunk)
	case StateRecovering:
		return sp.handleRecoveryChunk(chunk)
	case StateTerminating:
		return nil
	default:
		return fmt.Errorf("unknown state: %s", sp.state.Current.String())
	}
}

// handleInitialChunk handles the first chunk received
func (sp *StreamProcessor) handleInitialChunk(chunk *ParsedChunk) error {
	switch chunk.Type {
	case ChunkTypeEmpty:
		// Skip empty chunks
		return nil
	case ChunkTypeData:
		if chunk.HasContent {
			// First content chunk - enter stuttering mode
			sp.state.Buffer = chunk.Content
			sp.state.TransitionTo(StateStuttering, "first content chunk")
			sp.logger.DebugLog("Entering stuttering mode with first chunk: %s", chunk.ContentText)
		} else {
			// Non-content data chunk - forward directly
			sp.forwardChunk(chunk)
			sp.state.TransitionTo(StateNormalFlow, "non-content data chunk")
		}
	case ChunkTypeDone:
		// Immediate DONE - forward and terminate
		sp.forwardChunk(chunk)
		sp.state.TransitionTo(StateTerminating, "received DONE")
	case ChunkTypeMalformed, ChunkTypeUnknown:
		// Handle error
		return sp.handleChunkError(chunk)
	}
	
	sp.state.IncrementChunk()
	return nil
}

// handleStutteringChunk handles chunks during stuttering phase
func (sp *StreamProcessor) handleStutteringChunk(chunk *ParsedChunk) error {
	switch chunk.Type {
	case ChunkTypeEmpty:
		return nil
	case ChunkTypeData:
		if !chunk.HasContent {
			// Non-content chunk during stuttering - forward and continue
			sp.forwardChunk(chunk)
			return nil
		}

		// Check if stuttering continues
		if sp.isStillStuttering(sp.state.Buffer, chunk.Content) {
			// Update buffer and continue stuttering
			sp.state.Buffer = chunk.Content
			sp.logger.DebugLog("Stuttering continues, buffering: %s", chunk.ContentText)
		} else {
			// Stuttering resolved - flush buffer and current chunk
			sp.flushBufferedContent()
			sp.forwardChunk(chunk)
			sp.state.TransitionTo(StateNormalFlow, "stuttering resolved")
			sp.logger.DebugLog("Stuttering resolved, flushed buffer and current chunk")
		}
	case ChunkTypeDone:
		// DONE during stuttering - flush buffer and terminate
		sp.flushBufferedContent()
		sp.forwardChunk(chunk)
		sp.state.TransitionTo(StateTerminating, "received DONE during stuttering")
	case ChunkTypeMalformed, ChunkTypeUnknown:
		return sp.handleChunkError(chunk)
	}

	sp.state.IncrementChunk()
	return nil
}

// handleNormalChunk handles chunks during normal flow
func (sp *StreamProcessor) handleNormalChunk(chunk *ParsedChunk) error {
	switch chunk.Type {
	case ChunkTypeEmpty:
		return nil
	case ChunkTypeData, ChunkTypeUnknown:
		sp.forwardChunk(chunk)
	case ChunkTypeDone:
		sp.forwardChunk(chunk)
		sp.state.TransitionTo(StateTerminating, "received DONE")
	case ChunkTypeMalformed:
		return sp.handleChunkError(chunk)
	}

	sp.state.IncrementChunk()
	return nil
}

// handleRecoveryChunk handles chunks during recovery phase
func (sp *StreamProcessor) handleRecoveryChunk(chunk *ParsedChunk) error {
	// During recovery, try to get back to normal flow
	if chunk.IsValid {
		sp.state.TransitionTo(StateNormalFlow, "recovered from error")
		return sp.handleNormalChunk(chunk)
	}
	
	// Still having issues
	return sp.handleChunkError(chunk)
}

// handleChunkError handles errors in chunk processing
func (sp *StreamProcessor) handleChunkError(chunk *ParsedChunk) error {
	upstreamErr := &UpstreamError{
		Type:        ErrorParsingFailure,
		Severity:    SeverityMedium,
		Recoverable: true,
		Message:     "chunk processing failed",
		Cause:       chunk.Error,
		Timestamp:   time.Now(),
	}

	if chunk.Type == ChunkTypeMalformed {
		upstreamErr.Type = ErrorMalformedJSON
	}

	action := sp.recovery.HandleError(upstreamErr, sp.state)
	sp.state.IncrementError()

	switch action {
	case ActionContinue:
		return nil
	case ActionSkip:
		return nil
	case ActionRetry:
		sp.state.TransitionTo(StateRecovering, "retrying after error")
		return nil
	case ActionTerminate:
		sp.state.TransitionTo(StateTerminating, "terminating due to error")
		return upstreamErr
	default:
		return upstreamErr
	}
}

// isStillStuttering checks if stuttering is still occurring
func (sp *StreamProcessor) isStillStuttering(bufferedContent, currentContent string) bool {
	if bufferedContent == "" {
		return true // First chunk is always considered stuttering
	}

	// Parse the JSON to extract actual content for comparison
	bufferedChunk := sp.parser.chunkToJson(bufferedContent)
	if bufferedChunk == nil {
		return false // If buffered content is malformed, stop stuttering
	}

	bufferedText, hasBufContent := sp.parser.extractContentFromJSON(bufferedChunk)
	if !hasBufContent {
		return false // No content to compare
	}

	currentChunk := sp.parser.chunkToJson(currentContent)
	if currentChunk == nil {
		return false // If current content is malformed, stop stuttering
	}

	currentText, hasCurContent := sp.parser.extractContentFromJSON(currentChunk)
	if !hasCurContent {
		return false // No content to compare
	}

	// Check for prefix relationship (stuttering pattern)
	if len(currentText) < len(bufferedText) {
		return strings.HasPrefix(bufferedText, currentText)
	}
	return strings.HasPrefix(currentText, bufferedText)
}

// flushBufferedContent flushes the buffered content
func (sp *StreamProcessor) flushBufferedContent() {
	if sp.state.Buffer != "" {
		fmt.Fprintf(sp.writer, "data: %s\n\n", sp.state.Buffer)
		sp.writer.Flush()
		sp.logger.DebugLog("Flushed buffered content: %s", sp.state.Buffer)
		sp.state.Buffer = ""
	}
}

// forwardChunk forwards a chunk to the client
func (sp *StreamProcessor) forwardChunk(chunk *ParsedChunk) {
	switch chunk.Type {
	case ChunkTypeData:
		fmt.Fprintf(sp.writer, "data: %s\n\n", chunk.Content)
		sp.logger.DebugLog("Forwarded data chunk: %s", chunk.Content)
	case ChunkTypeDone:
		fmt.Fprintf(sp.writer, "data: [DONE]\n\n")
		sp.logger.DebugLog("Forwarded DONE chunk")
	case ChunkTypeUnknown:
		fmt.Fprintf(sp.writer, "%s", chunk.RawLine)
		sp.logger.DebugLog("Forwarded unknown chunk: %s", strings.TrimSpace(chunk.RawLine))
	}
	sp.writer.Flush()
}