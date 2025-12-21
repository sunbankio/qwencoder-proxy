// Package proxy provides simple streaming functionality without stuttering detection
package proxy

import (
	"context"
	"net/http"
	"time"
)

// StreamProcessorState represents the state of stream processing
type StreamProcessorState int

const (
	StateInitial StreamProcessorState = iota
	StateProcessing
	StateTerminating
)

// StreamState holds the current state of stream processing
type StreamState struct {
	Current         StreamProcessorState
	ChunkCount      int
	ErrorCount      int
	StartTime       time.Time
	LastValidChunk  time.Time
}

// StreamProcessor handles streaming responses without stuttering detection
type StreamProcessor struct {
	writer *responseWriterWrapper
	ctx    context.Context
	state  *StreamState
}

// NewStreamProcessor creates a new stream processor
func NewStreamProcessor(w *responseWriterWrapper, ctx context.Context) *StreamProcessor {
	now := time.Now()
	return &StreamProcessor{
		writer: w,
		ctx:    ctx,
		state: &StreamState{
			Current:        StateInitial,
			StartTime:      now,
			LastValidChunk: now,
		},
	}
}

// ProcessLine processes a single line from the stream
func (sp *StreamProcessor) ProcessLine(line string) error {
	select {
	case <-sp.ctx.Done():
		return sp.ctx.Err()
	default:
		_, err := sp.writer.Write([]byte(line))
		if err != nil {
			sp.state.ErrorCount++
			return err
		}
		if flusher, ok := sp.writer.ResponseWriter.(http.Flusher); ok {
			flusher.Flush()
		}
		sp.state.ChunkCount++
		sp.state.LastValidChunk = time.Now()
		return nil
	}
}