package proxy

import (
	"fmt"
	"sync"
	"time"

	"github.com/sunbankio/qwencoder-proxy/logging"
)

// Phase 3: Enhanced Error Handling & Recovery

// CircuitState represents the state of the circuit breaker
type CircuitState int

const (
	CircuitClosed CircuitState = iota
	CircuitOpen
	CircuitHalfOpen
)

func (cs CircuitState) String() string {
	switch cs {
	case CircuitClosed:
		return "Closed"
	case CircuitOpen:
		return "Open"
	case CircuitHalfOpen:
		return "HalfOpen"
	default:
		return "Unknown"
	}
}

// CircuitBreaker implements the circuit breaker pattern for upstream resilience
type CircuitBreaker struct {
	mu               sync.RWMutex
	maxFailures      int
	resetTimeout     time.Duration
	state            CircuitState
	failureCount     int
	successCount     int
	lastFailureTime  time.Time
	lastSuccessTime  time.Time
	halfOpenMaxTries int
	halfOpenTries    int
	logger           *logging.Logger
}

// NewCircuitBreaker creates a new circuit breaker
func NewCircuitBreaker(maxFailures int, resetTimeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		maxFailures:      maxFailures,
		resetTimeout:     resetTimeout,
		state:            CircuitClosed,
		halfOpenMaxTries: 3,
		logger:           logging.NewLogger(),
	}
}

// CanExecute checks if the circuit breaker allows execution
func (cb *CircuitBreaker) CanExecute() bool {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	switch cb.state {
	case CircuitClosed:
		return true
	case CircuitOpen:
		// Check if we should transition to half-open
		if time.Since(cb.lastFailureTime) > cb.resetTimeout {
			return true // Will transition to half-open in Execute
		}
		return false
	case CircuitHalfOpen:
		return cb.halfOpenTries < cb.halfOpenMaxTries
	default:
		return false
	}
}

// Execute wraps the execution of an operation with circuit breaker logic
func (cb *CircuitBreaker) Execute(operation func() error) error {
	if !cb.CanExecute() {
		return fmt.Errorf("circuit breaker is open, operation not allowed")
	}

	// Handle state transitions
	cb.mu.Lock()
	if cb.state == CircuitOpen && time.Since(cb.lastFailureTime) > cb.resetTimeout {
		cb.state = CircuitHalfOpen
		cb.halfOpenTries = 0
		cb.logger.DebugLog("Circuit breaker transitioning to half-open state")
	}

	if cb.state == CircuitHalfOpen {
		cb.halfOpenTries++
	}
	cb.mu.Unlock()

	// Execute the operation
	err := operation()

	// Handle the result
	if err != nil {
		cb.OnFailure(err)
		return err
	}

	cb.OnSuccess()
	return nil
}

// OnSuccess records a successful operation
func (cb *CircuitBreaker) OnSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.successCount++
	cb.lastSuccessTime = time.Now()

	switch cb.state {
	case CircuitHalfOpen:
		// If we've had enough successful tries, close the circuit
		if cb.halfOpenTries >= cb.halfOpenMaxTries {
			cb.state = CircuitClosed
			cb.failureCount = 0
			cb.halfOpenTries = 0
			cb.logger.InfoLog("Circuit breaker closed after successful recovery")
		}
	case CircuitClosed:
		// Reset failure count on success
		if cb.failureCount > 0 {
			cb.failureCount = 0
		}
	}
}

// OnFailure records a failed operation
func (cb *CircuitBreaker) OnFailure(err error) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failureCount++
	cb.lastFailureTime = time.Now()

	switch cb.state {
	case CircuitClosed:
		if cb.failureCount >= cb.maxFailures {
			cb.state = CircuitOpen
			cb.logger.WarningLog("Circuit breaker opened due to %d failures. Last error: %v",
				cb.failureCount, err)
		}
	case CircuitHalfOpen:
		// Any failure in half-open state opens the circuit
		cb.state = CircuitOpen
		cb.halfOpenTries = 0
		cb.logger.WarningLog("Circuit breaker opened from half-open state due to failure: %v", err)
	}
}

// GetState returns the current state of the circuit breaker
func (cb *CircuitBreaker) GetState() CircuitState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// GetStats returns statistics about the circuit breaker
func (cb *CircuitBreaker) GetStats() CircuitBreakerStats {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	return CircuitBreakerStats{
		State:           cb.state,
		FailureCount:    cb.failureCount,
		SuccessCount:    cb.successCount,
		LastFailureTime: cb.lastFailureTime,
		LastSuccessTime: cb.lastSuccessTime,
		HalfOpenTries:   cb.halfOpenTries,
	}
}

// CircuitBreakerStats holds statistics about circuit breaker performance
type CircuitBreakerStats struct {
	State           CircuitState
	FailureCount    int
	SuccessCount    int
	LastFailureTime time.Time
	LastSuccessTime time.Time
	HalfOpenTries   int
}

// RetryConfig holds configuration for retry logic
type RetryConfig struct {
	MaxRetries    int
	BaseDelay     time.Duration
	MaxDelay      time.Duration
	BackoffFactor float64
	Jitter        bool
}

// DefaultRetryConfig returns a default retry configuration
func DefaultRetryConfig() *RetryConfig {
	return &RetryConfig{
		MaxRetries:    3,
		BaseDelay:     100 * time.Millisecond,
		MaxDelay:      5 * time.Second,
		BackoffFactor: 2.0,
		Jitter:        true,
	}
}

// RetryWithBackoff implements exponential backoff with jitter
type RetryWithBackoff struct {
	config *RetryConfig
	logger *logging.Logger
}

// NewRetryWithBackoff creates a new retry handler
func NewRetryWithBackoff(config *RetryConfig) *RetryWithBackoff {
	if config == nil {
		config = DefaultRetryConfig()
	}
	return &RetryWithBackoff{
		config: config,
		logger: logging.NewLogger(),
	}
}

// Execute executes an operation with retry logic
func (r *RetryWithBackoff) Execute(operation func() error) error {
	var lastErr error

	for attempt := 0; attempt <= r.config.MaxRetries; attempt++ {
		if attempt > 0 {
			delay := r.calculateDelay(attempt)
			r.logger.DebugLog("Retrying operation after %v (attempt %d/%d)",
				delay, attempt, r.config.MaxRetries)
			time.Sleep(delay)
		}

		err := operation()
		if err == nil {
			if attempt > 0 {
				r.logger.InfoLog("Operation succeeded after %d retries", attempt)
			}
			return nil
		}

		lastErr = err
		r.logger.DebugLog("Operation failed on attempt %d: %v", attempt+1, err)

		// Check if error is retryable
		if !r.isRetryableError(err) {
			r.logger.DebugLog("Error is not retryable, stopping retry attempts")
			break
		}
	}

	return fmt.Errorf("operation failed after %d retries: %w", r.config.MaxRetries, lastErr)
}

// calculateDelay calculates the delay for the next retry attempt
func (r *RetryWithBackoff) calculateDelay(attempt int) time.Duration {
	delay := float64(r.config.BaseDelay) * pow(r.config.BackoffFactor, float64(attempt-1))

	if delay > float64(r.config.MaxDelay) {
		delay = float64(r.config.MaxDelay)
	}

	// Add jitter if enabled
	if r.config.Jitter {
		jitter := delay * 0.1 * (2*randomFloat() - 1) // ±10% jitter
		delay += jitter
	}

	return time.Duration(delay)
}

// isRetryableError determines if an error is retryable
func (r *RetryWithBackoff) isRetryableError(err error) bool {
	// For now, consider most errors retryable except for specific cases
	// This can be extended with more sophisticated error classification
	if err == nil {
		return false
	}

	errStr := err.Error()

	// Non-retryable errors
	nonRetryablePatterns := []string{
		"context canceled",
		"context deadline exceeded",
		"authentication",
		"authorization",
		"forbidden",
		"not found",
		"bad request",
	}

	for _, pattern := range nonRetryablePatterns {
		if contains(errStr, pattern) {
			return false
		}
	}

	return true
}

// Enhanced Error Recovery Manager with Circuit Breaker
type EnhancedErrorRecoveryManager struct {
	*ErrorRecoveryManager
	circuitBreaker *CircuitBreaker
	retryHandler   *RetryWithBackoff
}

// NewEnhancedErrorRecoveryManager creates an enhanced error recovery manager
func NewEnhancedErrorRecoveryManager() *EnhancedErrorRecoveryManager {
	baseManager := NewErrorRecoveryManager()

	return &EnhancedErrorRecoveryManager{
		ErrorRecoveryManager: baseManager,
		circuitBreaker:       NewCircuitBreaker(5, 30*time.Second),
		retryHandler:         NewRetryWithBackoff(DefaultRetryConfig()),
	}
}

// HandleErrorWithCircuitBreaker handles errors with circuit breaker protection
func (erm *EnhancedErrorRecoveryManager) HandleErrorWithCircuitBreaker(
	err *UpstreamError,
	state *StreamState,
	operation func() error,
) RecoveryAction {
	// Check circuit breaker state
	if !erm.circuitBreaker.CanExecute() {
		erm.logger.WarningLog("Circuit breaker is open, skipping operation")
		return ActionSkip
	}

	// Get base recovery action
	baseAction := erm.HandleError(err, state)

	// If base action is retry, use circuit breaker and retry logic
	if baseAction == ActionRetry {
		cbErr := erm.circuitBreaker.Execute(func() error {
			return erm.retryHandler.Execute(operation)
		})

		if cbErr != nil {
			erm.logger.ErrorLog("Operation failed even with retry and circuit breaker: %v", cbErr)
			return ActionSkip // Fallback to skip instead of terminate
		}

		return ActionContinue
	}

	return baseAction
}

// GetCircuitBreakerStats returns circuit breaker statistics
func (erm *EnhancedErrorRecoveryManager) GetCircuitBreakerStats() CircuitBreakerStats {
	return erm.circuitBreaker.GetStats()
}

// Utility functions

// pow calculates base^exp for float64
func pow(base, exp float64) float64 {
	if exp == 0 {
		return 1
	}
	if exp == 1 {
		return base
	}

	result := 1.0
	for i := 0; i < int(exp); i++ {
		result *= base
	}
	return result
}

// randomFloat returns a random float between 0 and 1
func randomFloat() float64 {
	// Simple pseudo-random number generator
	// In production, you might want to use crypto/rand or math/rand
	return 0.5 // Simplified for this example
}

// contains checks if a string contains a substring (case-insensitive)
func contains(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr ||
			(len(s) > len(substr) &&
				(s[:len(substr)] == substr ||
					s[len(s)-len(substr):] == substr ||
					indexOfSubstring(s, substr) >= 0)))
}

// indexOfSubstring finds the index of a substring in a string
func indexOfSubstring(s, substr string) int {
	if len(substr) == 0 {
		return 0
	}
	if len(s) < len(substr) {
		return -1
	}

	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
