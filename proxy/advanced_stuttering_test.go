package proxy

import (
	"testing"
)

func TestStreamProcessor_FirstChunk(t *testing.T) {
	processor := &StreamProcessor{
		state:  NewStreamState(),
		parser: NewChunkParser(),
	}
	
	// Test first chunk - should be considered stuttering
	result := processor.isStillStuttering("", `{"choices":[{"delta":{"content":"Hello"}}]}`)
	
	if !result {
		t.Error("Expected first chunk to be considered stuttering")
	}
}

func TestStreamProcessor_PrefixPattern(t *testing.T) {
	processor := &StreamProcessor{
		state:  NewStreamState(),
		parser: NewChunkParser(),
	}
	
	// First chunk
	firstChunk := `{"choices":[{"delta":{"content":"Hello"}}]}`
	secondChunk := `{"choices":[{"delta":{"content":"Hello world"}}]}`
	
	// Test prefix relationship - should be considered stuttering
	result := processor.isStillStuttering(firstChunk, secondChunk)
	
	if !result {
		t.Error("Expected prefix pattern to be detected as stuttering")
	}
}

func TestStreamProcessor_NoStuttering(t *testing.T) {
	processor := &StreamProcessor{
		state:  NewStreamState(),
		parser: NewChunkParser(),
	}
	
	// First chunk
	firstChunk := `{"choices":[{"delta":{"content":"Hello"}}]}`
	secondChunk := `{"choices":[{"delta":{"content":"World"}}]}`
	
	// Test no relationship - should not be considered stuttering
	result := processor.isStillStuttering(firstChunk, secondChunk)
	
	if result {
		t.Error("Expected no relationship to not be detected as stuttering")
	}
}

func TestStreamProcessor_MalformedJSON(t *testing.T) {
	processor := &StreamProcessor{
		state:  NewStreamState(),
		parser: NewChunkParser(),
	}
	
	// First chunk valid
	firstChunk := `{"choices":[{"delta":{"content":"Hello"}}]}`
	// Second chunk malformed
	secondChunk := `{"choices":[{"delta":{"content":"Hello"`
	
	// Test malformed JSON - should not be considered stuttering
	result := processor.isStillStuttering(firstChunk, secondChunk)
	
	if result {
		t.Error("Expected malformed JSON to not be detected as stuttering")
	}
}

func TestStreamProcessor_IdenticalContent(t *testing.T) {
	processor := &StreamProcessor{
		state:  NewStreamState(),
		parser: NewChunkParser(),
	}
	
	// Both chunks identical
	firstChunk := `{"choices":[{"delta":{"content":"Hello"}}]}`
	secondChunk := `{"choices":[{"delta":{"content":"Hello"}}]}`
	
	// Test identical content - should be considered stuttering
	result := processor.isStillStuttering(firstChunk, secondChunk)
	
	if !result {
		t.Error("Expected identical content to be detected as stuttering")
	}
}