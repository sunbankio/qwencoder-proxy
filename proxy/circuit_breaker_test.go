package proxy

import (
	"errors"
	"testing"
	"time"
)

func TestCircuitBreaker_InitialState(t *testing.T) {
	cb := NewCircuitBreaker(3, 1*time.Second)
	
	if cb.GetState() != CircuitClosed {
		t.Errorf("Expected initial state to be Closed, got %s", cb.GetState().String())
	}
	
	if !cb.CanExecute() {
		t.Error("Expected circuit breaker to allow execution initially")
	}
}

func TestCircuitBreaker_FailureThreshold(t *testing.T) {
	cb := NewCircuitBreaker(2, 1*time.Second)
	
	// First failure - should remain closed
	cb.OnFailure(errors.New("test error 1"))
	if cb.GetState() != CircuitClosed {
		t.Errorf("Expected state to remain Closed after 1 failure, got %s", cb.GetState().String())
	}
	
	// Second failure - should open
	cb.OnFailure(errors.New("test error 2"))
	if cb.GetState() != CircuitOpen {
		t.Errorf("Expected state to be Open after 2 failures, got %s", cb.GetState().String())
	}
	
	// Should not allow execution when open
	if cb.CanExecute() {
		t.Error("Expected circuit breaker to not allow execution when open")
	}
}

func TestCircuitBreaker_Recovery(t *testing.T) {
	cb := NewCircuitBreaker(1, 100*time.Millisecond)
	
	// Trigger failure to open circuit
	cb.OnFailure(errors.New("test error"))
	if cb.GetState() != CircuitOpen {
		t.Errorf("Expected state to be Open, got %s", cb.GetState().String())
	}
	
	// Wait for reset timeout
	time.Sleep(150 * time.Millisecond)
	
	// Should allow execution after timeout (will transition to half-open)
	if !cb.CanExecute() {
		t.Error("Expected circuit breaker to allow execution after timeout")
	}
	
	// Execute successful operation
	err := cb.Execute(func() error {
		return nil
	})
	
	if err != nil {
		t.Errorf("Expected successful execution, got error: %v", err)
	}
	
	// After successful half-open tries, should be closed
	// Need to execute enough times to close the circuit
	for i := 0; i < 3; i++ {
		err := cb.Execute(func() error { return nil })
		if err != nil {
			t.Errorf("Expected successful execution, got error: %v", err)
		}
	}
	
	if cb.GetState() != CircuitClosed {
		t.Errorf("Expected state to be Closed after successful recovery, got %s", cb.GetState().String())
	}
}

func TestCircuitBreaker_HalfOpenFailure(t *testing.T) {
	cb := NewCircuitBreaker(1, 100*time.Millisecond)
	
	// Open the circuit
	cb.OnFailure(errors.New("initial error"))
	
	// Wait for reset timeout
	time.Sleep(150 * time.Millisecond)
	
	// Execute failing operation in half-open state
	err := cb.Execute(func() error {
		return errors.New("half-open failure")
	})
	
	if err == nil {
		t.Error("Expected error from failing operation")
	}
	
	// Should be open again
	if cb.GetState() != CircuitOpen {
		t.Errorf("Expected state to be Open after half-open failure, got %s", cb.GetState().String())
	}
}

func TestCircuitBreaker_Execute(t *testing.T) {
	cb := NewCircuitBreaker(2, 1*time.Second)
	
	// Test successful execution
	executed := false
	err := cb.Execute(func() error {
		executed = true
		return nil
	})
	
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
	
	if !executed {
		t.Error("Expected operation to be executed")
	}
	
	// Test failed execution
	err = cb.Execute(func() error {
		return errors.New("operation failed")
	})
	
	if err == nil {
		t.Error("Expected error from failing operation")
	}
}

func TestCircuitBreaker_Stats(t *testing.T) {
	cb := NewCircuitBreaker(2, 1*time.Second)
	
	// Record some operations
	cb.OnSuccess()
	cb.OnFailure(errors.New("test error"))
	
	stats := cb.GetStats()
	
	if stats.SuccessCount != 1 {
		t.Errorf("Expected success count to be 1, got %d", stats.SuccessCount)
	}
	
	if stats.FailureCount != 1 {
		t.Errorf("Expected failure count to be 1, got %d", stats.FailureCount)
	}
	
	if stats.State != CircuitClosed {
		t.Errorf("Expected state to be Closed, got %s", stats.State.String())
	}
}

func TestRetryWithBackoff_Success(t *testing.T) {
	config := &RetryConfig{
		MaxRetries:    2,
		BaseDelay:     10 * time.Millisecond,
		MaxDelay:      100 * time.Millisecond,
		BackoffFactor: 2.0,
		Jitter:        false,
	}
	
	retry := NewRetryWithBackoff(config)
	
	attempts := 0
	err := retry.Execute(func() error {
		attempts++
		if attempts < 2 {
			return errors.New("temporary failure")
		}
		return nil
	})
	
	if err != nil {
		t.Errorf("Expected success after retries, got error: %v", err)
	}
	
	if attempts != 2 {
		t.Errorf("Expected 2 attempts, got %d", attempts)
	}
}

func TestRetryWithBackoff_MaxRetriesExceeded(t *testing.T) {
	config := &RetryConfig{
		MaxRetries:    2,
		BaseDelay:     1 * time.Millisecond,
		MaxDelay:      10 * time.Millisecond,
		BackoffFactor: 2.0,
		Jitter:        false,
	}
	
	retry := NewRetryWithBackoff(config)
	
	attempts := 0
	err := retry.Execute(func() error {
		attempts++
		return errors.New("persistent failure")
	})
	
	if err == nil {
		t.Error("Expected error after max retries exceeded")
	}
	
	expectedAttempts := config.MaxRetries + 1 // Initial attempt + retries
	if attempts != expectedAttempts {
		t.Errorf("Expected %d attempts, got %d", expectedAttempts, attempts)
	}
}

func TestRetryWithBackoff_NonRetryableError(t *testing.T) {
	retry := NewRetryWithBackoff(DefaultRetryConfig())
	
	attempts := 0
	err := retry.Execute(func() error {
		attempts++
		return errors.New("context canceled")
	})
	
	if err == nil {
		t.Error("Expected error for non-retryable error")
	}
	
	if attempts != 1 {
		t.Errorf("Expected only 1 attempt for non-retryable error, got %d", attempts)
	}
}

func TestRetryWithBackoff_DelayCalculation(t *testing.T) {
	config := &RetryConfig{
		MaxRetries:    3,
		BaseDelay:     100 * time.Millisecond,
		MaxDelay:      1 * time.Second,
		BackoffFactor: 2.0,
		Jitter:        false,
	}
	
	retry := NewRetryWithBackoff(config)
	
	// Test delay calculation
	delay1 := retry.calculateDelay(1)
	delay2 := retry.calculateDelay(2)
	delay3 := retry.calculateDelay(3)
	
	expectedDelay1 := 100 * time.Millisecond
	expectedDelay2 := 200 * time.Millisecond
	expectedDelay3 := 400 * time.Millisecond
	
	if delay1 != expectedDelay1 {
		t.Errorf("Expected delay1 to be %v, got %v", expectedDelay1, delay1)
	}
	
	if delay2 != expectedDelay2 {
		t.Errorf("Expected delay2 to be %v, got %v", expectedDelay2, delay2)
	}
	
	if delay3 != expectedDelay3 {
		t.Errorf("Expected delay3 to be %v, got %v", expectedDelay3, delay3)
	}
}

func TestRetryWithBackoff_MaxDelay(t *testing.T) {
	config := &RetryConfig{
		MaxRetries:    5,
		BaseDelay:     100 * time.Millisecond,
		MaxDelay:      300 * time.Millisecond,
		BackoffFactor: 2.0,
		Jitter:        false,
	}
	
	retry := NewRetryWithBackoff(config)
	
	// Test that delay doesn't exceed max delay
	delay4 := retry.calculateDelay(4) // Would be 800ms without max delay
	
	if delay4 > config.MaxDelay {
		t.Errorf("Expected delay to not exceed max delay %v, got %v", config.MaxDelay, delay4)
	}
}

func TestEnhancedErrorRecoveryManager(t *testing.T) {
	erm := NewEnhancedErrorRecoveryManager()
	state := NewStreamState()
	
	// Test circuit breaker integration
	upstreamErr := &UpstreamError{
		Type:        ErrorNetworkTimeout,
		Severity:    SeverityMedium,
		Recoverable: true,
		Message:     "network timeout",
	}
	
	// Mock operation that succeeds
	operation := func() error {
		return nil
	}
	
	action := erm.HandleErrorWithCircuitBreaker(upstreamErr, state, operation)
	
	// Should continue after successful retry
	if action != ActionContinue {
		t.Errorf("Expected ActionContinue, got %d", action)
	}
	
	// Test circuit breaker stats
	stats := erm.GetCircuitBreakerStats()
	if stats.State != CircuitClosed {
		t.Errorf("Expected circuit breaker to be closed, got %s", stats.State.String())
	}
}

func TestEnhancedErrorRecoveryManager_CircuitOpen(t *testing.T) {
	erm := NewEnhancedErrorRecoveryManager()
	state := NewStreamState()
	
	// Force circuit breaker to open by recording failures
	for i := 0; i < 6; i++ {
		erm.circuitBreaker.OnFailure(errors.New("test failure"))
	}
	
	upstreamErr := &UpstreamError{
		Type:        ErrorNetworkTimeout,
		Severity:    SeverityMedium,
		Recoverable: true,
		Message:     "network timeout",
	}
	
	operation := func() error {
		return errors.New("operation failed")
	}
	
	action := erm.HandleErrorWithCircuitBreaker(upstreamErr, state, operation)
	
	// Should skip when circuit is open
	if action != ActionSkip {
		t.Errorf("Expected ActionSkip when circuit is open, got %d", action)
	}
}

// Utility function tests
func TestUtilityFunctions(t *testing.T) {
	// Test pow function
	if pow(2, 3) != 8 {
		t.Errorf("Expected pow(2, 3) to be 8, got %f", pow(2, 3))
	}
	
	if pow(2, 0) != 1 {
		t.Errorf("Expected pow(2, 0) to be 1, got %f", pow(2, 0))
	}
	
	// Test contains function
	if !contains("hello world", "world") {
		t.Error("Expected contains('hello world', 'world') to be true")
	}
	
	if contains("hello", "world") {
		t.Error("Expected contains('hello', 'world') to be false")
	}
	
	// Test indexOfSubstring function
	if indexOfSubstring("hello world", "world") != 6 {
		t.Errorf("Expected indexOfSubstring('hello world', 'world') to be 6, got %d", 
			indexOfSubstring("hello world", "world"))
	}
	
	if indexOfSubstring("hello", "world") != -1 {
		t.Errorf("Expected indexOfSubstring('hello', 'world') to be -1, got %d", 
			indexOfSubstring("hello", "world"))
	}
}

// Benchmark tests
func BenchmarkCircuitBreaker_Execute(b *testing.B) {
	cb := NewCircuitBreaker(10, 1*time.Second)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = cb.Execute(func() error {
			return nil
		})
	}
}

func BenchmarkRetryWithBackoff_Execute(b *testing.B) {
	retry := NewRetryWithBackoff(DefaultRetryConfig())
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = retry.Execute(func() error {
			return nil
		})
	}
}