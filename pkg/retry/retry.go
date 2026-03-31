// Package retry provides a configurable retry mechanism with exponential backoff.
package retry

import (
	"context"
	"errors"
	"math/rand"
	"time"
)

// Default configuration values.
const (
	DefaultMaxAttempts  = 3
	DefaultInitialDelay = 100 * time.Millisecond
	DefaultMaxDelay     = 10 * time.Second
	DefaultMultiplier   = 2.0
	DefaultJitter       = 0.1
)

// Error definitions.
var (
	ErrMaxAttemptsExceeded = errors.New("maximum retry attempts exceeded")
	ErrContextCanceled     = errors.New("context canceled")
)

// RetryableError wraps an error to indicate it can be retried.
type RetryableError struct {
	Err       error
	Retryable bool
}

// Error implements the error interface.
func (e *RetryableError) Error() string {
	return e.Err.Error()
}

// Unwrap returns the underlying error.
func (e *RetryableError) Unwrap() error {
	return e.Err
}

// IsRetryable returns true if the error is retryable.
func IsRetryable(err error) bool {
	var retryableErr *RetryableError
	if errors.As(err, &retryableErr) {
		return retryableErr.Retryable
	}
	return false
}

// Retryable wraps an error as retryable.
func Retryable(err error) error {
	return &RetryableError{Err: err, Retryable: true}
}

// NonRetryable wraps an error as non-retryable.
func NonRetryable(err error) error {
	return &RetryableError{Err: err, Retryable: false}
}

// Config contains retry configuration.
type Config struct {
	MaxAttempts  int           // Maximum number of attempts (including initial)
	InitialDelay time.Duration // Initial delay between retries
	MaxDelay     time.Duration // Maximum delay between retries
	Multiplier   float64       // Delay multiplier for exponential backoff
	Jitter       float64       // Jitter factor (0-1) to add randomness
}

// DefaultConfig returns a Config with default values.
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
type Option func(*Config)

// WithMaxAttempts sets the maximum number of attempts.
func WithMaxAttempts(max int) Option {
	return func(c *Config) {
		c.MaxAttempts = max
	}
}

// WithInitialDelay sets the initial delay.
func WithInitialDelay(delay time.Duration) Option {
	return func(c *Config) {
		c.InitialDelay = delay
	}
}

// WithMaxDelay sets the maximum delay.
func WithMaxDelay(delay time.Duration) Option {
	return func(c *Config) {
		c.MaxDelay = delay
	}
}

// WithMultiplier sets the delay multiplier.
func WithMultiplier(multiplier float64) Option {
	return func(c *Config) {
		c.Multiplier = multiplier
	}
}

// WithJitter sets the jitter factor.
func WithJitter(jitter float64) Option {
	return func(c *Config) {
		c.Jitter = jitter
	}
}

// Operation is a function that can be retried.
type Operation func() error

// Retry executes the operation with retry logic.
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
func RetryWithResult[T any](ctx context.Context, op func() (T, error), opts ...Option) (T, error) {
	var result T
	err := Retry(ctx, func() error {
		var err error
		result, err = op()
		return err
	}, opts...)
	return result, err
}

func calculateDelay(delay time.Duration, jitter float64) time.Duration {
	if jitter <= 0 {
		return delay
	}

	jitterRange := float64(delay) * jitter
	jitterAmount := rand.Float64() * jitterRange * 2
	return time.Duration(float64(delay) + jitterAmount - jitterRange)
}

// IsRetryableError checks if an error indicates the operation can be retried.
func IsRetryableError(err error) bool {
	return IsRetryable(err)
}

// Attempts returns the number of attempts made.
type attemptKey struct{}

// ContextWithAttempts returns a context with attempt counter.
func ContextWithAttempts(ctx context.Context, attempt int) context.Context {
	return context.WithValue(ctx, attemptKey{}, attempt)
}

// AttemptsFromContext returns the attempt number from context.
func AttemptsFromContext(ctx context.Context) int {
	if v := ctx.Value(attemptKey{}); v != nil {
		return v.(int)
	}
	return 0
}
