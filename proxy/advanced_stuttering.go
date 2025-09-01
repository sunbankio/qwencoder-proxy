package proxy

import (
	"strings"
	"time"

	"github.com/sunbankio/qwencoder-proxy/logging"
)

// Phase 4: Improved Stuttering Detection

// StutteringResult represents the result of stuttering analysis
type StutteringResult struct {
	IsStuttering    bool
	Confidence      float64
	Reason          string
	ShouldBuffer    bool
	ShouldFlush     bool
	NextAction      StutteringAction
}

// StutteringAction represents the recommended action based on stuttering analysis
type StutteringAction int

const (
	StutteringActionBuffer StutteringAction = iota
	StutteringActionFlush
	StutteringActionForward
	StutteringActionWait
)

func (sa StutteringAction) String() string {
	switch sa {
	case StutteringActionBuffer:
		return "Buffer"
	case StutteringActionFlush:
		return "Flush"
	case StutteringActionForward:
		return "Forward"
	case StutteringActionWait:
		return "Wait"
	default:
		return "Unknown"
	}
}

// ContentChunk represents a chunk of content with metadata
type ContentChunk struct {
	Content     string
	Timestamp   time.Time
	Length      int
	TokenCount  int
	ChunkIndex  int
}

// StutteringDetector provides advanced stuttering detection
type StutteringDetector struct {
	windowSize          int
	similarityThreshold float64
	contentHistory      []ContentChunk
	timeWindow          time.Duration
	minConfidence       float64
	logger              *logging.Logger
}

// NewStutteringDetector creates a new advanced stuttering detector
func NewStutteringDetector() *StutteringDetector {
	return &StutteringDetector{
		windowSize:          5,
		similarityThreshold: 0.8,
		timeWindow:          2 * time.Second,
		minConfidence:       0.7,
		contentHistory:      make([]ContentChunk, 0),
		logger:              logging.NewLogger(),
	}
}

// AnalyzeStuttering performs comprehensive stuttering analysis
func (sd *StutteringDetector) AnalyzeStuttering(current, previous string) StutteringResult {
	now := time.Now()
	
	// Create content chunks
	currentChunk := ContentChunk{
		Content:    current,
		Timestamp:  now,
		Length:     len(current),
		TokenCount: sd.estimateTokenCount(current),
		ChunkIndex: len(sd.contentHistory),
	}
	
	// Add to history
	sd.addToHistory(currentChunk)
	
	// If this is the first chunk, it's always stuttering
	if len(sd.contentHistory) <= 1 {
		return StutteringResult{
			IsStuttering: true,
			Confidence:   1.0,
			Reason:       "first chunk",
			ShouldBuffer: true,
			NextAction:   StutteringActionBuffer,
		}
	}
	
	// Perform multiple analysis methods
	prefixResult := sd.analyzePrefixPattern(current, previous)
	lengthResult := sd.analyzeLengthProgression()
	timingResult := sd.analyzeTimingPattern()
	contentResult := sd.analyzeContentSimilarity()
	
	// Combine results with weighted scoring
	combinedScore := sd.combineAnalysisResults(prefixResult, lengthResult, timingResult, contentResult)
	
	// Determine final result
	isStuttering := combinedScore.Confidence >= sd.minConfidence
	
	result := StutteringResult{
		IsStuttering: isStuttering,
		Confidence:   combinedScore.Confidence,
		Reason:       combinedScore.Reason,
		ShouldBuffer: isStuttering,
		ShouldFlush:  !isStuttering,
	}
	
	if isStuttering {
		result.NextAction = StutteringActionBuffer
	} else {
		result.NextAction = StutteringActionFlush
	}
	
	sd.logger.DebugLog("Stuttering analysis: %+v", result)
	return result
}

// analyzePrefixPattern analyzes prefix relationships between chunks
func (sd *StutteringDetector) analyzePrefixPattern(current, previous string) AnalysisResult {
	if previous == "" {
		return AnalysisResult{
			Confidence: 1.0,
			Reason:     "no previous content",
			Weight:     0.3,
		}
	}
	
	// Check for exact prefix relationship
	if strings.HasPrefix(current, previous) || strings.HasPrefix(previous, current) {
		similarity := sd.calculateStringSimilarity(current, previous)
		return AnalysisResult{
			Confidence: similarity,
			Reason:     "prefix relationship detected",
			Weight:     0.4,
		}
	}
	
	// Check for partial overlap
	overlap := sd.findLongestCommonPrefix(current, previous)
	if len(overlap) > 0 {
		overlapRatio := float64(len(overlap)) / float64(maxInt(len(current), len(previous)))
		if overlapRatio > 0.5 {
			return AnalysisResult{
				Confidence: overlapRatio,
				Reason:     "partial overlap detected",
				Weight:     0.3,
			}
		}
	}
	
	return AnalysisResult{
		Confidence: 0.0,
		Reason:     "no prefix pattern",
		Weight:     0.4,
	}
}

// analyzeLengthProgression analyzes the progression of content lengths
func (sd *StutteringDetector) analyzeLengthProgression() AnalysisResult {
	if len(sd.contentHistory) < 2 {
		return AnalysisResult{
			Confidence: 0.5,
			Reason:     "insufficient history",
			Weight:     0.2,
		}
	}
	
	// Look at recent chunks
	recentChunks := sd.getRecentChunks(3)
	if len(recentChunks) < 2 {
		return AnalysisResult{
			Confidence: 0.5,
			Reason:     "insufficient recent chunks",
			Weight:     0.2,
		}
	}
	
	// Check if lengths are increasing (typical stuttering pattern)
	isIncreasing := true
	for i := 1; i < len(recentChunks); i++ {
		if recentChunks[i].Length <= recentChunks[i-1].Length {
			isIncreasing = false
			break
		}
	}
	
	if isIncreasing {
		// Calculate confidence based on growth rate
		growthRate := float64(recentChunks[len(recentChunks)-1].Length) / float64(recentChunks[0].Length)
		confidence := growthRate / 2.0
		if confidence > 1.0 {
			confidence = 1.0
		}
		
		return AnalysisResult{
			Confidence: confidence,
			Reason:     "increasing length pattern",
			Weight:     0.25,
		}
	}
	
	return AnalysisResult{
		Confidence: 0.2,
		Reason:     "no clear length progression",
		Weight:     0.25,
	}
}

// analyzeTimingPattern analyzes the timing between chunks
func (sd *StutteringDetector) analyzeTimingPattern() AnalysisResult {
	if len(sd.contentHistory) < 2 {
		return AnalysisResult{
			Confidence: 0.5,
			Reason:     "insufficient timing data",
			Weight:     0.15,
		}
	}
	
	recentChunks := sd.getRecentChunks(3)
	if len(recentChunks) < 2 {
		return AnalysisResult{
			Confidence: 0.5,
			Reason:     "insufficient recent timing data",
			Weight:     0.15,
		}
	}
	
	// Calculate average interval
	var totalInterval time.Duration
	for i := 1; i < len(recentChunks); i++ {
		interval := recentChunks[i].Timestamp.Sub(recentChunks[i-1].Timestamp)
		totalInterval += interval
	}
	
	avgInterval := totalInterval / time.Duration(len(recentChunks)-1)
	
	// Stuttering typically has short intervals
	if avgInterval < 500*time.Millisecond {
		confidence := 1.0 - (float64(avgInterval) / float64(500*time.Millisecond))
		return AnalysisResult{
			Confidence: confidence,
			Reason:     "rapid succession timing",
			Weight:     0.15,
		}
	}
	
	return AnalysisResult{
		Confidence: 0.3,
		Reason:     "normal timing pattern",
		Weight:     0.15,
	}
}

// analyzeContentSimilarity analyzes content similarity patterns
func (sd *StutteringDetector) analyzeContentSimilarity() AnalysisResult {
	if len(sd.contentHistory) < 2 {
		return AnalysisResult{
			Confidence: 0.5,
			Reason:     "insufficient content history",
			Weight:     0.2,
		}
	}
	
	recentChunks := sd.getRecentChunks(3)
	if len(recentChunks) < 2 {
		return AnalysisResult{
			Confidence: 0.5,
			Reason:     "insufficient recent content",
			Weight:     0.2,
		}
	}
	
	// Calculate average similarity between consecutive chunks
	var totalSimilarity float64
	comparisons := 0
	
	for i := 1; i < len(recentChunks); i++ {
		similarity := sd.calculateStringSimilarity(
			recentChunks[i].Content,
			recentChunks[i-1].Content,
		)
		totalSimilarity += similarity
		comparisons++
	}
	
	if comparisons == 0 {
		return AnalysisResult{
			Confidence: 0.5,
			Reason:     "no content comparisons",
			Weight:     0.2,
		}
	}
	
	avgSimilarity := totalSimilarity / float64(comparisons)
	
	if avgSimilarity > sd.similarityThreshold {
		return AnalysisResult{
			Confidence: avgSimilarity,
			Reason:     "high content similarity",
			Weight:     0.2,
		}
	}
	
	return AnalysisResult{
		Confidence: avgSimilarity * 0.5, // Lower confidence for low similarity
		Reason:     "low content similarity",
		Weight:     0.2,
	}
}

// AnalysisResult represents the result of a single analysis method
type AnalysisResult struct {
	Confidence float64
	Reason     string
	Weight     float64
}

// combineAnalysisResults combines multiple analysis results with weighted scoring
func (sd *StutteringDetector) combineAnalysisResults(results ...AnalysisResult) AnalysisResult {
	var weightedSum float64
	var totalWeight float64
	var reasons []string
	
	for _, result := range results {
		weightedSum += result.Confidence * result.Weight
		totalWeight += result.Weight
		if result.Confidence > 0.5 {
			reasons = append(reasons, result.Reason)
		}
	}
	
	finalConfidence := weightedSum / totalWeight
	combinedReason := strings.Join(reasons, ", ")
	
	if combinedReason == "" {
		combinedReason = "combined analysis indicates no stuttering"
	}
	
	return AnalysisResult{
		Confidence: finalConfidence,
		Reason:     combinedReason,
		Weight:     1.0,
	}
}

// Helper methods

// addToHistory adds a chunk to the content history
func (sd *StutteringDetector) addToHistory(chunk ContentChunk) {
	sd.contentHistory = append(sd.contentHistory, chunk)
	
	// Keep only recent history within time window
	cutoff := time.Now().Add(-sd.timeWindow)
	var filtered []ContentChunk
	
	for _, c := range sd.contentHistory {
		if c.Timestamp.After(cutoff) {
			filtered = append(filtered, c)
		}
	}
	
	sd.contentHistory = filtered
	
	// Also limit by window size
	if len(sd.contentHistory) > sd.windowSize {
		sd.contentHistory = sd.contentHistory[len(sd.contentHistory)-sd.windowSize:]
	}
}

// getRecentChunks returns the most recent N chunks
func (sd *StutteringDetector) getRecentChunks(n int) []ContentChunk {
	if len(sd.contentHistory) <= n {
		return sd.contentHistory
	}
	return sd.contentHistory[len(sd.contentHistory)-n:]
}

// estimateTokenCount estimates the number of tokens in content
func (sd *StutteringDetector) estimateTokenCount(content string) int {
	// Simple estimation: split by spaces and common punctuation
	words := strings.FieldsFunc(content, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == ',' || r == '.' || r == '!' || r == '?'
	})
	return len(words)
}

// calculateStringSimilarity calculates similarity between two strings
func (sd *StutteringDetector) calculateStringSimilarity(s1, s2 string) float64 {
	if s1 == s2 {
		return 1.0
	}
	
	if len(s1) == 0 || len(s2) == 0 {
		return 0.0
	}
	
	// Use Levenshtein distance for similarity
	distance := sd.levenshteinDistance(s1, s2)
	maxLen := maxInt(len(s1), len(s2))
	
	similarity := 1.0 - (float64(distance) / float64(maxLen))
	if similarity < 0.0 {
		similarity = 0.0
	}
	return similarity
}

// levenshteinDistance calculates the Levenshtein distance between two strings
func (sd *StutteringDetector) levenshteinDistance(s1, s2 string) int {
	if len(s1) == 0 {
		return len(s2)
	}
	if len(s2) == 0 {
		return len(s1)
	}
	
	// Create matrix
	matrix := make([][]int, len(s1)+1)
	for i := range matrix {
		matrix[i] = make([]int, len(s2)+1)
	}
	
	// Initialize first row and column
	for i := 0; i <= len(s1); i++ {
		matrix[i][0] = i
	}
	for j := 0; j <= len(s2); j++ {
		matrix[0][j] = j
	}
	
	// Fill matrix
	for i := 1; i <= len(s1); i++ {
		for j := 1; j <= len(s2); j++ {
			cost := 0
			if s1[i-1] != s2[j-1] {
				cost = 1
			}
			
			deletion := matrix[i-1][j] + 1
			insertion := matrix[i][j-1] + 1
			substitution := matrix[i-1][j-1] + cost
			
			matrix[i][j] = minInt(deletion, minInt(insertion, substitution))
		}
	}
	
	return matrix[len(s1)][len(s2)]
}

// findLongestCommonPrefix finds the longest common prefix between two strings
func (sd *StutteringDetector) findLongestCommonPrefix(s1, s2 string) string {
	minLen := minInt(len(s1), len(s2))
	for i := 0; i < minLen; i++ {
		if s1[i] != s2[i] {
			return s1[:i]
		}
	}
	return s1[:minLen]
}

// SmartBuffer implements intelligent buffering with multiple flush policies
type SmartBuffer struct {
	chunks      []ParsedChunk
	maxSize     int
	maxAge      time.Duration
	flushPolicy FlushPolicy
	logger      *logging.Logger
	startTime   time.Time
}

// FlushPolicy defines when to flush the buffer
type FlushPolicy int

const (
	FlushOnSize FlushPolicy = iota
	FlushOnAge
	FlushOnPattern
	FlushOnConfidence
)

// NewSmartBuffer creates a new smart buffer
func NewSmartBuffer(maxSize int, maxAge time.Duration, policy FlushPolicy) *SmartBuffer {
	return &SmartBuffer{
		chunks:      make([]ParsedChunk, 0),
		maxSize:     maxSize,
		maxAge:      maxAge,
		flushPolicy: policy,
		logger:      logging.NewLogger(),
		startTime:   time.Now(),
	}
}

// Add adds a chunk to the buffer
func (sb *SmartBuffer) Add(chunk *ParsedChunk) bool {
	sb.chunks = append(sb.chunks, *chunk)
	return sb.shouldFlush()
}

// shouldFlush determines if the buffer should be flushed
func (sb *SmartBuffer) shouldFlush() bool {
	switch sb.flushPolicy {
	case FlushOnSize:
		return len(sb.chunks) >= sb.maxSize
	case FlushOnAge:
		return time.Since(sb.startTime) >= sb.maxAge
	case FlushOnPattern:
		return sb.detectFlushPattern()
	case FlushOnConfidence:
		return sb.calculateFlushConfidence() > 0.8
	default:
		return len(sb.chunks) >= sb.maxSize
	}
}

// detectFlushPattern detects patterns that indicate buffer should be flushed
func (sb *SmartBuffer) detectFlushPattern() bool {
	if len(sb.chunks) < 2 {
		return false
	}
	
	// Look for decreasing content similarity (indicates stuttering resolved)
	recent := sb.chunks[len(sb.chunks)-2:]
	if len(recent) == 2 {
		// Simple pattern detection - can be enhanced
		return len(recent[1].ContentText) > len(recent[0].ContentText)*2
	}
	
	return false
}

// calculateFlushConfidence calculates confidence that buffer should be flushed
func (sb *SmartBuffer) calculateFlushConfidence() float64 {
	if len(sb.chunks) == 0 {
		return 0.0
	}
	
	// Simple confidence calculation based on buffer age and size
	ageRatio := float64(time.Since(sb.startTime)) / float64(sb.maxAge)
	sizeRatio := float64(len(sb.chunks)) / float64(sb.maxSize)
	
	result := ageRatio + sizeRatio
	if result > 1.0 {
		result = 1.0
	}
	return result
}

// Flush returns all buffered chunks and clears the buffer
func (sb *SmartBuffer) Flush() []ParsedChunk {
	chunks := make([]ParsedChunk, len(sb.chunks))
	copy(chunks, sb.chunks)
	
	sb.chunks = sb.chunks[:0] // Clear buffer
	sb.startTime = time.Now() // Reset timer
	
	sb.logger.DebugLog("Flushed %d chunks from smart buffer", len(chunks))
	return chunks
}

// GetSize returns the current buffer size
func (sb *SmartBuffer) GetSize() int {
	return len(sb.chunks)
}

// IsEmpty returns true if the buffer is empty
func (sb *SmartBuffer) IsEmpty() bool {
	return len(sb.chunks) == 0
}

// Utility functions

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}