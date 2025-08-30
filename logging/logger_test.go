package logging

import (
	"testing"
)

func TestLogger(t *testing.T) {
	logger := NewLogger()
	
	// Test that we can create a logger
	if logger == nil {
		t.Error("Failed to create logger")
	}
	
	// Test logging functions (these should not panic)
	logger.StreamLog("Test stream log")
	logger.NonStreamLog("Test non-stream log")
	logger.DoneLog("Test done log")
	logger.DoneNonStreamLog("Test done non-stream log")
	logger.SeparatorLog()
	logger.ErrorLog("Test error log")
	logger.WarningLog("Test warning log")
}