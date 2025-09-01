package proxy

import (
	"testing"
	"time"
)

func TestStutteringDetector_FirstChunk(t *testing.T) {
	detector := NewStutteringDetector()
	
	result := detector.AnalyzeStuttering("Hello", "")
	
	if !result.IsStuttering {
		t.Error("Expected first chunk to be considered stuttering")
	}
	
	if result.Confidence != 1.0 {
		t.Errorf("Expected confidence 1.0 for first chunk, got %f", result.Confidence)
	}
	
	if result.NextAction != StutteringActionBuffer {
		t.Errorf("Expected StutteringActionBuffer for first chunk, got %s", result.NextAction.String())
	}
}

func TestStutteringDetector_PrefixPattern(t *testing.T) {
	detector := NewStutteringDetector()
	
	// First chunk
	detector.AnalyzeStuttering("Hello", "")
	
	// Second chunk with prefix relationship
	result := detector.AnalyzeStuttering("Hello world", "Hello")
	
	if !result.IsStuttering {
		t.Error("Expected prefix pattern to be detected as stuttering")
	}
	
	if result.Confidence < 0.7 {
		t.Errorf("Expected high confidence for prefix pattern, got %f", result.Confidence)
	}
}

func TestStutteringDetector_NoStuttering(t *testing.T) {
	detector := NewStutteringDetector()
	
	// First chunk
	detector.AnalyzeStuttering("Hello", "")
	
	// Second chunk with no relationship
	result := detector.AnalyzeStuttering("Goodbye", "Hello")
	
	if result.IsStuttering {
		t.Error("Expected no stuttering for unrelated content")
	}
	
	if result.NextAction != StutteringActionFlush {
		t.Errorf("Expected StutteringActionFlush for non-stuttering, got %s", result.NextAction.String())
	}
}

func TestStutteringDetector_LengthProgression(t *testing.T) {
	detector := NewStutteringDetector()
	
	// Simulate increasing length pattern
	detector.AnalyzeStuttering("Hi", "")
	detector.AnalyzeStuttering("Hi there", "Hi")
	result := detector.AnalyzeStuttering("Hi there friend", "Hi there")
	
	if !result.IsStuttering {
		t.Error("Expected increasing length pattern to be detected as stuttering")
	}
}

func TestStutteringDetector_TimingPattern(t *testing.T) {
	detector := NewStutteringDetector()
	
	// First chunk
	detector.AnalyzeStuttering("Hello", "")
	
	// Wait a bit to simulate normal timing
	time.Sleep(10 * time.Millisecond)
	
	// Second chunk
	result := detector.AnalyzeStuttering("Hello world", "Hello")
	
	// Should still detect stuttering due to content similarity
	if !result.IsStuttering {
		t.Error("Expected stuttering detection despite timing")
	}
}

func TestStutteringDetector_ContentSimilarity(t *testing.T) {
	detector := NewStutteringDetector()
	
	// Test high similarity
	detector.AnalyzeStuttering("The quick brown fox", "")
	result := detector.AnalyzeStuttering("The quick brown fox jumps", "The quick brown fox")
	
	if !result.IsStuttering {
		t.Error("Expected high content similarity to indicate stuttering")
	}
	
	// Test low similarity
	detector2 := NewStutteringDetector()
	detector2.AnalyzeStuttering("Hello", "")
	result2 := detector2.AnalyzeStuttering("Goodbye", "Hello")
	
	if result2.IsStuttering {
		t.Error("Expected low content similarity to indicate no stuttering")
	}
}

func TestStutteringDetector_StringSimilarity(t *testing.T) {
	detector := NewStutteringDetector()
	
	tests := []struct {
		s1       string
		s2       string
		expected float64
		minSim   float64
	}{
		{"hello", "hello", 1.0, 1.0},
		{"hello", "helloworld", 0.5, 0.4},
		{"abc", "def", 0.0, 0.0},
		{"", "hello", 0.0, 0.0},
		{"hello", "", 0.0, 0.0},
	}
	
	for _, tt := range tests {
		similarity := detector.calculateStringSimilarity(tt.s1, tt.s2)
		if tt.expected > 0 && similarity < tt.minSim {
			t.Errorf("Expected similarity between %q and %q to be at least %f, got %f", 
				tt.s1, tt.s2, tt.minSim, similarity)
		}
		if tt.expected == 0 && similarity != 0 {
			t.Errorf("Expected similarity between %q and %q to be 0, got %f", 
				tt.s1, tt.s2, similarity)
		}
	}
}

func TestStutteringDetector_LevenshteinDistance(t *testing.T) {
	detector := NewStutteringDetector()
	
	tests := []struct {
		s1       string
		s2       string
		expected int
	}{
		{"", "", 0},
		{"hello", "", 5},
		{"", "world", 5},
		{"hello", "hello", 0},
		{"hello", "helloworld", 5},
		{"kitten", "sitting", 3},
	}
	
	for _, tt := range tests {
		distance := detector.levenshteinDistance(tt.s1, tt.s2)
		if distance != tt.expected {
			t.Errorf("Expected Levenshtein distance between %q and %q to be %d, got %d", 
				tt.s1, tt.s2, tt.expected, distance)
		}
	}
}

func TestStutteringDetector_CommonPrefix(t *testing.T) {
	detector := NewStutteringDetector()
	
	tests := []struct {
		s1       string
		s2       string
		expected string
	}{
		{"hello", "hello", "hello"},
		{"hello", "helloworld", "hello"},
		{"abc", "def", ""},
		{"", "hello", ""},
		{"hello", "", ""},
		{"prefix_test", "prefix_other", "prefix_"},
	}
	
	for _, tt := range tests {
		prefix := detector.findLongestCommonPrefix(tt.s1, tt.s2)
		if prefix != tt.expected {
			t.Errorf("Expected common prefix between %q and %q to be %q, got %q", 
				tt.s1, tt.s2, tt.expected, prefix)
		}
	}
}

func TestStutteringDetector_TokenCount(t *testing.T) {
	detector := NewStutteringDetector()
	
	tests := []struct {
		content  string
		expected int
	}{
		{"", 0},
		{"hello", 1},
		{"hello world", 2},
		{"hello, world!", 2},
		{"The quick brown fox jumps over the lazy dog.", 9},
	}
	
	for _, tt := range tests {
		count := detector.estimateTokenCount(tt.content)
		if count != tt.expected {
			t.Errorf("Expected token count for %q to be %d, got %d", 
				tt.content, tt.expected, count)
		}
	}
}

func TestStutteringDetector_HistoryManagement(t *testing.T) {
	detector := NewStutteringDetector()
	detector.windowSize = 3
	
	// Add more chunks than window size
	for i := 0; i < 5; i++ {
		detector.AnalyzeStuttering("content", "")
		time.Sleep(1 * time.Millisecond) // Ensure different timestamps
	}
	
	// Should only keep windowSize chunks
	if len(detector.contentHistory) > detector.windowSize {
		t.Errorf("Expected history size to be at most %d, got %d", 
			detector.windowSize, len(detector.contentHistory))
	}
}

func TestSmartBuffer_Basic(t *testing.T) {
	buffer := NewSmartBuffer(3, 1*time.Second, FlushOnSize)
	
	if !buffer.IsEmpty() {
		t.Error("Expected new buffer to be empty")
	}
	
	if buffer.GetSize() != 0 {
		t.Errorf("Expected new buffer size to be 0, got %d", buffer.GetSize())
	}
}

func TestSmartBuffer_FlushOnSize(t *testing.T) {
	buffer := NewSmartBuffer(2, 1*time.Second, FlushOnSize)
	
	chunk1 := &ParsedChunk{Content: "chunk1"}
	chunk2 := &ParsedChunk{Content: "chunk2"}
	
	// Add first chunk - should not flush
	shouldFlush := buffer.Add(chunk1)
	if shouldFlush {
		t.Error("Expected buffer not to flush after first chunk")
	}
	
	// Add second chunk - should flush
	shouldFlush = buffer.Add(chunk2)
	if !shouldFlush {
		t.Error("Expected buffer to flush after reaching max size")
	}
	
	// Flush and verify
	chunks := buffer.Flush()
	if len(chunks) != 2 {
		t.Errorf("Expected 2 chunks after flush, got %d", len(chunks))
	}
	
	if !buffer.IsEmpty() {
		t.Error("Expected buffer to be empty after flush")
	}
}

func TestSmartBuffer_FlushOnAge(t *testing.T) {
	buffer := NewSmartBuffer(10, 50*time.Millisecond, FlushOnAge)
	
	chunk := &ParsedChunk{Content: "chunk"}
	buffer.Add(chunk)
	
	// Should not flush immediately
	if buffer.shouldFlush() {
		t.Error("Expected buffer not to flush immediately")
	}
	
	// Wait for age threshold
	time.Sleep(60 * time.Millisecond)
	
	// Should flush now
	if !buffer.shouldFlush() {
		t.Error("Expected buffer to flush after age threshold")
	}
}

func TestSmartBuffer_FlushOnConfidence(t *testing.T) {
	buffer := NewSmartBuffer(10, 1*time.Second, FlushOnConfidence)
	
	// Add some chunks
	for i := 0; i < 5; i++ {
		chunk := &ParsedChunk{Content: "chunk"}
		buffer.Add(chunk)
	}
	
	// Calculate confidence
	confidence := buffer.calculateFlushConfidence()
	if confidence < 0 || confidence > 1 {
		t.Errorf("Expected confidence to be between 0 and 1, got %f", confidence)
	}
}

func TestSmartBuffer_FlushOnPattern(t *testing.T) {
	buffer := NewSmartBuffer(10, 1*time.Second, FlushOnPattern)
	
	chunk1 := &ParsedChunk{Content: "short", ContentText: "short"}
	chunk2 := &ParsedChunk{Content: "much longer content", ContentText: "much longer content"}
	
	buffer.Add(chunk1)
	buffer.Add(chunk2)
	
	// Should detect pattern (significant length increase)
	if !buffer.detectFlushPattern() {
		t.Error("Expected pattern detection to trigger flush")
	}
}

// Benchmark tests
func BenchmarkStutteringDetector_AnalyzeStuttering(b *testing.B) {
	detector := NewStutteringDetector()
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		detector.AnalyzeStuttering("Hello world", "Hello")
	}
}

func BenchmarkStutteringDetector_StringSimilarity(b *testing.B) {
	detector := NewStutteringDetector()
	s1 := "The quick brown fox jumps over the lazy dog"
	s2 := "The quick brown fox jumps over the lazy cat"
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		detector.calculateStringSimilarity(s1, s2)
	}
}

func BenchmarkStutteringDetector_LevenshteinDistance(b *testing.B) {
	detector := NewStutteringDetector()
	s1 := "The quick brown fox"
	s2 := "The quick brown cat"
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		detector.levenshteinDistance(s1, s2)
	}
}

func BenchmarkSmartBuffer_Add(b *testing.B) {
	buffer := NewSmartBuffer(1000, 1*time.Second, FlushOnSize)
	chunk := &ParsedChunk{Content: "test chunk"}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buffer.Add(chunk)
		if buffer.shouldFlush() {
			buffer.Flush()
		}
	}
}

// Integration test
func TestAdvancedStutteringIntegration(t *testing.T) {
	detector := NewStutteringDetector()
	buffer := NewSmartBuffer(5, 1*time.Second, FlushOnConfidence)
	
	// Simulate a stuttering sequence
	testSequence := []string{
		"Hello",
		"Hello world",
		"Hello world, how",
		"Hello world, how are",
		"Hello world, how are you?",
		"I'm fine, thanks!", // This should break the stuttering pattern
	}
	
	var results []StutteringResult
	for i, content := range testSequence {
		var previous string
		if i > 0 {
			previous = testSequence[i-1]
		}
		
		result := detector.AnalyzeStuttering(content, previous)
		results = append(results, result)
		
		// Add to buffer if stuttering
		if result.IsStuttering {
			chunk := &ParsedChunk{
				Content:     content,
				ContentText: content,
			}
			buffer.Add(chunk)
		}
	}
	
	// First few should be stuttering
	for i := 0; i < 4; i++ {
		if !results[i].IsStuttering {
			t.Errorf("Expected chunk %d to be stuttering", i)
		}
	}
	
	// Last one should not be stuttering
	if results[len(results)-1].IsStuttering {
		t.Error("Expected last chunk to not be stuttering")
	}
	
	// Buffer should have accumulated chunks
	if buffer.IsEmpty() {
		t.Error("Expected buffer to contain chunks from stuttering sequence")
	}
}