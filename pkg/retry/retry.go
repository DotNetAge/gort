// Package retry provides a configurable retry mechanism with exponential backoff
// and jitter for handling transient failures in distributed systems.
//
// Retry Strategy:
//
//	Initial Delay
//	     │
//	     ▼
//	  ┌─────┐    ┌─────┐    ┌─────┐    ┌─────┐
//	  │ 1st │───▶│ 2nd │───▶│ 3rd │───▶│ ... │
//	  │     │    │     │    │     │    │     │
//	  └─────┘    └─────┘    └─────┘    └─────┘
//	     │          │          │
//	   100ms      200ms      400ms     (exponential)
//	   ±10ms      ±20ms      ±40ms     (jitter)
//
// The retry mechanism uses exponential backoff with configurable jitter to prevent
// thundering herd problems when services recover.
//
// Basic Usage:
//
//	// Simple retry with defaults
//	err := retry.Retry(ctx, func() error {
//	    return callExternalAPI()
//	})
//
//	// Custom configuration
//	err := retry.Retry(ctx, func() error {
//	    return callExternalAPI()
//	}, retry.WithMaxAttempts(5),
//	   retry.WithInitialDelay(500*time.Millisecond),
//	   retry.WithMaxDelay(30*time.Second))
//
//	// With result
//	result, err := retry.RetryWithResult(ctx, func() (string, error) {
//	    return fetchData()
//	})
//
//	// Mark errors as retryable or non-retryable
//	err := retry.Retry(ctx, func() error {
//	    err := callAPI()
//	    if isPermanentError(err) {
//	        return retry.NonRetryable(err)
//	    }
//	    return err
//	})
//
// Configuration Options:
//
//   - MaxAttempts: Maximum number of attempts (default: 3)
//   - InitialDelay: Initial delay between retries (default: 100ms)
//   - MaxDelay: Maximum delay between retries (default: 10s)
//   - Multiplier: Delay multiplier for exponential backoff (default: 2.0)
//   - Jitter: Random factor to add variability (default: 0.1)
//
// Thread Safety:
//
// All functions in this package are safe for concurrent use.
package retry

import (
	"context"
	"errors"
	"math/rand"
	"time"
)

// Default configuration values.
const (
	// DefaultMaxAttempts is the default maximum number of retry attempts.
	DefaultMaxAttempts = 3

	// DefaultInitialDelay is the default initial delay between retries.
	DefaultInitialDelay = 100 * time.Millisecond

	// DefaultMaxDelay is the default maximum delay between retries.
	DefaultMaxDelay = 10 * time.Second

	// DefaultMultiplier is the default delay multiplier for exponential backoff.
	DefaultMultiplier = 2.0

	// DefaultJitter is the default jitter factor (10% variability).
	DefaultJitter = 0.1
)

// Error definitions.
var (
	// ErrMaxAttemptsExceeded is returned when all retry attempts have been exhausted.
	ErrMaxAttemptsExceeded = errors.New("maximum retry attempts exceeded")

	// ErrContextCanceled is returned when the context is canceled during retry.
	ErrContextCanceled = errors.New("context canceled")
)

// RetryableError wraps an error to indicate it can be retried.
// Use Retryable() and NonRetryable() to create instances of this type.
type RetryableError struct {
	// Err is the underlying error.
	Err error

	// Retryable indicates whether this error should trigger a retry.
	Retryable bool
}

// Error implements the error interface.
// Returns the error message from the wrapped error.
func (e *RetryableError) Error() string {
	return e.Err.Error()
}

// Unwrap returns the underlying error.
// This allows errors.Is and errors.As to work with RetryableError.
func (e *RetryableError) Unwrap() error {
	return e.Err
}

// IsRetryable returns true if the error is retryable.
// It unwraps RetryableError to check the Retryable flag.
//
// Parameters:
//   - err: The error to check
//
// Returns true if the error should trigger a retry.
func IsRetryable(err error) bool {
	var retryableErr *RetryableError
	if errors.As(err, &retryableErr) {
		return retryableErr.Retryable
	}
	return false
}

// Retryable wraps an error as retryable.
// Use this to mark errors that should trigger a retry.
//
// Parameters:
//   - err: The error to wrap
//
// Returns a RetryableError that will trigger retries.
//
// Example:
//
//	if err := callAPI(); err != nil {
//	    return retry.Retryable(err)
//	}
func Retryable(err error) error {
	return &RetryableError{Err: err, Retryable: true}
}

// NonRetryable wraps an error as non-retryable.
// Use this to mark errors that should not trigger a retry.
//
// Parameters:
//   - err: The error to wrap
//
// Returns a RetryableError that will not trigger retries.
//
// Example:
//
//	if err := validateInput(); err != nil {
//	    return retry.NonRetryable(err)
//	}
func NonRetryable(err error) error {
	return &RetryableError{Err: err, Retryable: false}
}

// Config contains retry configuration.
// Use Option functions to modify the default configuration.
type Config struct {
	// MaxAttempts is the maximum number of attempts (including initial).
	MaxAttempts int

	// InitialDelay is the initial delay between retries.
	InitialDelay time.Duration

	// MaxDelay is the maximum delay between retries.
	MaxDelay time.Duration

	// Multiplier is the delay multiplier for exponential backoff.
	Multiplier float64

	// Jitter is the jitter factor (0-1) to add randomness.
	Jitter float64
}

// DefaultConfig returns a Config with default values.
//
// Returns a Config initialized with package defaults.
func DefaultConfig() Config {
	return Config{
		MaxAttempts:  DefaultMaxAttempts,
		InitialDelay: DefaultInitialDelay,
		MaxDelay:     DefaultMaxDelay,
		Multiplier:   DefaultMultiplier,
		Jitter:       DefaultJitter,
	}
}

// Option is a function that modifies Config.
// Use these with Retry() and RetryWithResult().
type Option func(*Config)

// WithMaxAttempts sets the maximum number of attempts.
//
// Parameters:
//   - max: The maximum number of attempts (minimum 1)
//
// Example:
//
//	retry.Retry(ctx, op, retry.WithMaxAttempts(5))
func WithMaxAttempts(max int) Option {
	return func(c *Config) {
		c.MaxAttempts = max
	}
}

// WithInitialDelay sets the initial delay.
//
// Parameters:
//   - delay: The initial delay between retries
//
// Example:
//
//	retry.Retry(ctx, op, retry.WithInitialDelay(500*time.Millisecond))
func WithInitialDelay(delay time.Duration) Option {
	return func(c *Config) {
		c.InitialDelay = delay
	}
}

// WithMaxDelay sets the maximum delay.
//
// Parameters:
//   - delay: The maximum delay between retries
//
// Example:
//
//	retry.Retry(ctx, op, retry.WithMaxDelay(30*time.Second))
func WithMaxDelay(delay time.Duration) Option {
	return func(c *Config) {
		c.MaxDelay = delay
	}
}

// WithMultiplier sets the delay multiplier.
//
// Parameters:
//   - multiplier: The multiplier for exponential backoff (e.g., 2.0 doubles the delay)
//
// Example:
//
//	retry.Retry(ctx, op, retry.WithMultiplier(1.5))
func WithMultiplier(multiplier float64) Option {
	return func(c *Config) {
		c.Multiplier = multiplier
	}
}

// WithJitter sets the jitter factor.
//
// Parameters:
//   - jitter: The jitter factor between 0 and 1 (0 = no jitter, 1 = 100% jitter)
//
// Example:
//
//	retry.Retry(ctx, op, retry.WithJitter(0.2))
func WithJitter(jitter float64) Option {
	return func(c *Config) {
		c.Jitter = jitter
	}
}

// Operation is a function that can be retried.
// It should return nil on success or an error on failure.
type Operation func() error

// Retry executes the operation with retry logic.
// It implements exponential backoff with jitter.
//
// Parameters:
//   - ctx: Context for cancellation
//   - op: The operation to retry
//   - opts: Optional configuration overrides
//
// Returns nil on success, or an error if all attempts fail.
//
// Example:
//
//	err := retry.Retry(ctx, func() error {
//	    resp, err := http.Get(url)
//	    if err != nil {
//	        return err
//	    }
//	    defer resp.Body.Close()
//	    if resp.StatusCode >= 500 {
//	        return fmt.Errorf("server error: %d", resp.StatusCode)
//	    }
//	    return nil
//	}, retry.WithMaxAttempts(5))
func Retry(ctx context.Context, op Operation, opts ...Option) error {
	config := DefaultConfig()
	for _, opt := range opts {
		opt(&config)
	}

	if config.MaxAttempts < 1 {
		config.MaxAttempts = 1
	}

	var lastErr error
	delay := config.InitialDelay
	if delay > config.MaxDelay {
		delay = config.MaxDelay
	}

	for attempt := 1; attempt <= config.MaxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return ErrContextCanceled
		default:
		}

		err := op()
		if err == nil {
			return nil
		}

		lastErr = err

		if !IsRetryable(err) {
			return err
		}

		if attempt == config.MaxAttempts {
			break
		}

		actualDelay := calculateDelay(delay, config.Jitter)

		select {
		case <-ctx.Done():
			return ErrContextCanceled
		case <-time.After(actualDelay):
		}

		delay = time.Duration(float64(delay) * config.Multiplier)
		if delay > config.MaxDelay {
			delay = config.MaxDelay
		}
	}

	return &RetryableError{
		Err:       errors.Join(ErrMaxAttemptsExceeded, lastErr),
		Retryable: true,
	}
}

// RetryWithResult executes the operation with retry logic and returns a result.
// This is a generic version of Retry for operations that return values.
//
// Parameters:
//   - ctx: Context for cancellation
//   - op: The operation that returns a value and error
//   - opts: Optional configuration overrides
//
// Returns the result and nil on success, or zero value and error on failure.
//
// Example:
//
//	data, err := retry.RetryWithResult(ctx, func() (string, error) {
//	    resp, err := http.Get(url)
//	    if err != nil {
//	        return "", err
//	    }
//	    defer resp.Body.Close()
//	    body, _ := io.ReadAll(resp.Body)
//	    return string(body), nil
//	})
func RetryWithResult[T any](ctx context.Context, op func() (T, error), opts ...Option) (T, error) {
	var result T
	err := Retry(ctx, func() error {
		var err error
		result, err = op()
		return err
	}, opts...)
	return result, err
}

// calculateDelay calculates the actual delay with jitter applied.
//
// Parameters:
//   - delay: The base delay
//   - jitter: The jitter factor (0-1)
//
// Returns the delay with jitter applied.
func calculateDelay(delay time.Duration, jitter float64) time.Duration {
	if jitter <= 0 {
		return delay
	}

	jitterRange := float64(delay) * jitter
	jitterAmount := rand.Float64() * jitterRange * 2
	return time.Duration(float64(delay) + jitterAmount - jitterRange)
}

// IsRetryableError checks if an error indicates the operation can be retried.
// This is an alias for IsRetryable for backward compatibility.
//
// Deprecated: Use IsRetryable instead.
func IsRetryableError(err error) bool {
	return IsRetryable(err)
}

// attemptKey is the context key for storing attempt count.
type attemptKey struct{}

// ContextWithAttempts returns a context with attempt counter.
// This can be used to track retry attempts within operations.
//
// Parameters:
//   - ctx: The parent context
//   - attempt: The current attempt number
//
// Returns a new context with the attempt number stored.
//
// Example:
//
//	ctx = retry.ContextWithAttempts(ctx, attempt)
//	attempt := retry.AttemptsFromContext(ctx)
func ContextWithAttempts(ctx context.Context, attempt int) context.Context {
	return context.WithValue(ctx, attemptKey{}, attempt)
}

// AttemptsFromContext returns the attempt number from context.
// Returns 0 if no attempt number is stored.
//
// Parameters:
//   - ctx: The context to read from
//
// Returns the attempt number, or 0 if not found.
//
// Example:
//
//	attempt := retry.AttemptsFromContext(ctx)
//	if attempt > 1 {
//	    log.Printf("Retry attempt %d", attempt)
//	}
func AttemptsFromContext(ctx context.Context) int {
	if v := ctx.Value(attemptKey{}); v != nil {
		return v.(int)
	}
	return 0
}
