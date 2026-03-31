package retry

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRetry_Success(t *testing.T) {
	callCount := 0
	err := Retry(context.Background(), func() error {
		callCount++
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, 1, callCount)
}

func TestRetry_SuccessAfterRetry(t *testing.T) {
	callCount := 0
	err := Retry(context.Background(), func() error {
		callCount++
		if callCount < 3 {
			return Retryable(errors.New("temporary error"))
		}
		return nil
	}, WithMaxAttempts(3), WithInitialDelay(time.Millisecond))

	require.NoError(t, err)
	assert.Equal(t, 3, callCount)
}

func TestRetry_MaxAttemptsExceeded(t *testing.T) {
	callCount := 0
	err := Retry(context.Background(), func() error {
		callCount++
		return Retryable(errors.New("always fails"))
	}, WithMaxAttempts(3), WithInitialDelay(time.Millisecond))

	require.Error(t, err)
	assert.Equal(t, 3, callCount)
	assert.True(t, IsRetryable(err))
}

func TestRetry_NonRetryableError(t *testing.T) {
	callCount := 0
	expectedErr := errors.New("permanent error")
	err := Retry(context.Background(), func() error {
		callCount++
		return NonRetryable(expectedErr)
	}, WithMaxAttempts(3), WithInitialDelay(time.Millisecond))

	require.Error(t, err)
	assert.Equal(t, 1, callCount)
	assert.False(t, IsRetryable(err))
}

func TestRetry_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	callCount := 0
	err := Retry(ctx, func() error {
		callCount++
		return Retryable(errors.New("error"))
	}, WithMaxAttempts(3))

	require.Error(t, err)
	assert.Equal(t, ErrContextCanceled, err)
	assert.Equal(t, 0, callCount)
}

func TestRetry_ContextCanceledDuringDelay(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	callCount := 0
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	err := Retry(ctx, func() error {
		callCount++
		return Retryable(errors.New("error"))
	}, WithMaxAttempts(10), WithInitialDelay(time.Second))

	require.Error(t, err)
	assert.Equal(t, ErrContextCanceled, err)
}

func TestRetryWithResult_Success(t *testing.T) {
	callCount := 0
	result, err := RetryWithResult(context.Background(), func() (string, error) {
		callCount++
		return "success", nil
	})

	require.NoError(t, err)
	assert.Equal(t, "success", result)
	assert.Equal(t, 1, callCount)
}

func TestRetryWithResult_SuccessAfterRetry(t *testing.T) {
	callCount := 0
	result, err := RetryWithResult(context.Background(), func() (string, error) {
		callCount++
		if callCount < 2 {
			return "", Retryable(errors.New("temporary error"))
		}
		return "success", nil
	}, WithMaxAttempts(3), WithInitialDelay(time.Millisecond))

	require.NoError(t, err)
	assert.Equal(t, "success", result)
	assert.Equal(t, 2, callCount)
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	assert.Equal(t, DefaultMaxAttempts, config.MaxAttempts)
	assert.Equal(t, DefaultInitialDelay, config.InitialDelay)
	assert.Equal(t, DefaultMaxDelay, config.MaxDelay)
	assert.Equal(t, DefaultMultiplier, config.Multiplier)
	assert.Equal(t, DefaultJitter, config.Jitter)
}

func TestOptions(t *testing.T) {
	config := DefaultConfig()
	WithMaxAttempts(5)(&config)
	WithInitialDelay(200 * time.Millisecond)(&config)
	WithMaxDelay(30 * time.Second)(&config)
	WithMultiplier(3.0)(&config)
	WithJitter(0.5)(&config)

	assert.Equal(t, 5, config.MaxAttempts)
	assert.Equal(t, 200*time.Millisecond, config.InitialDelay)
	assert.Equal(t, 30*time.Second, config.MaxDelay)
	assert.Equal(t, 3.0, config.Multiplier)
	assert.Equal(t, 0.5, config.Jitter)
}

func TestIsRetryable(t *testing.T) {
	assert.True(t, IsRetryable(Retryable(errors.New("error"))))
	assert.False(t, IsRetryable(NonRetryable(errors.New("error"))))
	assert.False(t, IsRetryable(errors.New("plain error")))
}

func TestRetryableError(t *testing.T) {
	originalErr := errors.New("original error")
	retryableErr := Retryable(originalErr)

	assert.Equal(t, originalErr.Error(), retryableErr.Error())
	assert.True(t, errors.Is(retryableErr, originalErr))
}

func TestCalculateDelay(t *testing.T) {
	delay := 100 * time.Millisecond

	for i := 0; i < 100; i++ {
		actualDelay := calculateDelay(delay, 0.1)
		assert.GreaterOrEqual(t, actualDelay, time.Duration(float64(delay)*0.9))
		assert.LessOrEqual(t, actualDelay, time.Duration(float64(delay)*1.1))
	}
}

func TestCalculateDelay_NoJitter(t *testing.T) {
	delay := 100 * time.Millisecond
	actualDelay := calculateDelay(delay, 0)
	assert.Equal(t, delay, actualDelay)
}

func TestRetry_MaxDelay(t *testing.T) {
	callCount := 0
	start := time.Now()
	err := Retry(context.Background(), func() error {
		callCount++
		return Retryable(errors.New("error"))
	}, WithMaxAttempts(3), WithInitialDelay(time.Second), WithMaxDelay(100*time.Millisecond))

	elapsed := time.Since(start)
	require.Error(t, err)
	assert.Less(t, elapsed, 500*time.Millisecond)
}

func TestRetry_ZeroMaxAttempts(t *testing.T) {
	callCount := 0
	err := Retry(context.Background(), func() error {
		callCount++
		return nil
	}, WithMaxAttempts(0))

	require.NoError(t, err)
	assert.Equal(t, 1, callCount)
}
