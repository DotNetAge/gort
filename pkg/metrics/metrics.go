// Package metrics provides a metrics collection system for monitoring gort system
// performance and health. It supports pluggable collectors for integration with
// various monitoring backends (Prometheus, StatsD, etc.).
//
// Metrics Architecture:
//
//	┌─────────────┐     ┌─────────────┐     ┌─────────────┐
//	│   System    │────▶│  Collector  │────▶│   Backend   │
//	│  Components │     │  Interface  │     │ (Prometheus)│
//	└─────────────┘     └─────────────┘     └─────────────┘
//
// Metric Types:
//
//   - Counters: Messages received/sent, errors
//   - Gauges: Active sessions, channel status
//   - Histograms: Processing durations, retry attempts
//
// Basic Usage:
//
//	// Use the global collector
//	metrics.RecordMessageReceived("dingtalk", "inbound", "text")
//	metrics.RecordMessageSent("feishu", "outbound", "image")
//
//	// Or use a custom collector
//	stats := metrics.NewStatsCollector()
//	metrics.SetGlobalCollector(stats)
//
//	// Get statistics
//	stats := stats.GetStats()
//	fmt.Printf("Messages: %d, Errors: %d\n", stats.MessagesReceived, stats.Errors)
//
// Custom Collector Implementation:
//
//	type PrometheusCollector struct {
//	    messagesReceived *prometheus.CounterVec
//	}
//
//	func (c *PrometheusCollector) RecordMessageReceived(channel, direction, msgType string) {
//	    c.messagesReceived.WithLabelValues(channel, direction, msgType).Inc()
//	}
//
//	// Implement other methods...
//
// Thread Safety:
//
// All collectors in this package are safe for concurrent use.
// The global collector functions are also thread-safe.
package metrics

import (
	"sync"
	"time"
)

// Collector defines the interface for collecting metrics.
// Implementations can integrate with various monitoring systems
// such as Prometheus, StatsD, CloudWatch, etc.
//
// All methods should be safe for concurrent use.
type Collector interface {
	// RecordMessageReceived records a message being received.
	//
	// Parameters:
	//   - channel: The channel ID (e.g., "dingtalk", "feishu")
	//   - direction: The message direction ("inbound" or "outbound")
	//   - msgType: The message type (e.g., "text", "image")
	RecordMessageReceived(channel, direction, msgType string)

	// RecordMessageSent records a message being sent.
	//
	// Parameters:
	//   - channel: The channel ID
	//   - direction: The message direction
	//   - msgType: The message type
	RecordMessageSent(channel, direction, msgType string)

	// RecordMessageDuration records the time taken to process a message.
	//
	// Parameters:
	//   - channel: The channel ID
	//   - duration: The processing duration
	RecordMessageDuration(channel string, duration time.Duration)

	// RecordChannelStatus records the current status of a channel.
	//
	// Parameters:
	//   - channel: The channel ID
	//   - channelType: The type of channel (e.g., "dingtalk", "feishu")
	//   - running: Whether the channel is currently running
	RecordChannelStatus(channel, channelType string, running bool)

	// RecordChannelError records an error from a channel.
	//
	// Parameters:
	//   - channel: The channel ID
	//   - channelType: The type of channel
	//   - errorType: The type of error (e.g., "connection", "auth")
	RecordChannelError(channel, channelType, errorType string)

	// RecordSessionActive records the current number of active sessions.
	//
	// Parameters:
	//   - count: The number of active sessions
	RecordSessionActive(count int)

	// RecordSessionDuration records the duration of a session.
	//
	// Parameters:
	//   - duration: The session duration
	RecordSessionDuration(duration time.Duration)

	// RecordMiddlewareDuration records the time spent in middleware.
	//
	// Parameters:
	//   - middleware: The middleware name
	//   - duration: The execution duration
	RecordMiddlewareDuration(middleware string, duration time.Duration)

	// RecordRetry records a retry attempt.
	//
	// Parameters:
	//   - operation: The operation being retried
	//   - attempts: The number of attempts made
	//   - success: Whether the operation eventually succeeded
	RecordRetry(operation string, attempts int, success bool)
}

// DefaultCollector is a no-op collector.
// Use this when metrics collection is not needed.
type DefaultCollector struct{}

// RecordMessageReceived is a no-op.
func (d *DefaultCollector) RecordMessageReceived(channel, direction, msgType string) {}

// RecordMessageSent is a no-op.
func (d *DefaultCollector) RecordMessageSent(channel, direction, msgType string) {}

// RecordMessageDuration is a no-op.
func (d *DefaultCollector) RecordMessageDuration(channel string, duration time.Duration) {}

// RecordChannelStatus is a no-op.
func (d *DefaultCollector) RecordChannelStatus(channel, channelType string, running bool) {}

// RecordChannelError is a no-op.
func (d *DefaultCollector) RecordChannelError(channel, channelType, errorType string) {}

// RecordSessionActive is a no-op.
func (d *DefaultCollector) RecordSessionActive(count int) {}

// RecordSessionDuration is a no-op.
func (d *DefaultCollector) RecordSessionDuration(duration time.Duration) {}

// RecordMiddlewareDuration is a no-op.
func (d *DefaultCollector) RecordMiddlewareDuration(middleware string, duration time.Duration) {}

// RecordRetry is a no-op.
func (d *DefaultCollector) RecordRetry(operation string, attempts int, success bool) {}

// Stats holds basic statistics.
// This is a simple data structure for retrieving collected metrics.
type Stats struct {
	// MessagesReceived is the total number of messages received.
	MessagesReceived int64

	// MessagesSent is the total number of messages sent.
	MessagesSent int64

	// Errors is the total number of errors encountered.
	Errors int64

	// ActiveSessions is the current number of active sessions.
	ActiveSessions int64
}

// StatsCollector is a simple collector that tracks basic statistics.
// It provides thread-safe access to counters and gauges.
//
// This type is safe for concurrent use.
type StatsCollector struct {
	mu    sync.RWMutex
	stats Stats
}

// NewStatsCollector creates a new StatsCollector.
//
// Returns a new StatsCollector with zeroed statistics.
//
// Example:
//
//	collector := metrics.NewStatsCollector()
//	metrics.SetGlobalCollector(collector)
func NewStatsCollector() *StatsCollector {
	return &StatsCollector{
		stats: Stats{},
	}
}

// RecordMessageReceived increments the received message counter.
func (s *StatsCollector) RecordMessageReceived(channel, direction, msgType string) {
	s.mu.Lock()
	s.stats.MessagesReceived++
	s.mu.Unlock()
}

// RecordMessageSent increments the sent message counter.
func (s *StatsCollector) RecordMessageSent(channel, direction, msgType string) {
	s.mu.Lock()
	s.stats.MessagesSent++
	s.mu.Unlock()
}

// RecordMessageDuration is currently a no-op for StatsCollector.
func (s *StatsCollector) RecordMessageDuration(channel string, duration time.Duration) {}

// RecordChannelStatus is currently a no-op for StatsCollector.
func (s *StatsCollector) RecordChannelStatus(channel, channelType string, running bool) {}

// RecordChannelError increments the error counter.
func (s *StatsCollector) RecordChannelError(channel, channelType, errorType string) {
	s.mu.Lock()
	s.stats.Errors++
	s.mu.Unlock()
}

// RecordSessionActive updates the active session count.
func (s *StatsCollector) RecordSessionActive(count int) {
	s.mu.Lock()
	s.stats.ActiveSessions = int64(count)
	s.mu.Unlock()
}

// RecordSessionDuration is currently a no-op for StatsCollector.
func (s *StatsCollector) RecordSessionDuration(duration time.Duration) {}

// RecordMiddlewareDuration is currently a no-op for StatsCollector.
func (s *StatsCollector) RecordMiddlewareDuration(middleware string, duration time.Duration) {}

// RecordRetry is currently a no-op for StatsCollector.
func (s *StatsCollector) RecordRetry(operation string, attempts int, success bool) {}

// GetStats returns a copy of the current statistics.
//
// Returns a Stats struct containing the current values.
// The returned Stats is a snapshot and will not be updated.
//
// Example:
//
//	stats := collector.GetStats()
//	fmt.Printf("Total messages: %d\n", stats.MessagesReceived)
func (s *StatsCollector) GetStats() Stats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.stats
}

// Global collector instance with thread-safe access.
var (
	globalCollector Collector = &DefaultCollector{}
	globalMu        sync.RWMutex
)

// SetGlobalCollector sets the global metrics collector.
// All subsequent metric recordings will use this collector.
//
// Parameters:
//   - c: The collector to set as global
//
// Example:
//
//	collector := metrics.NewStatsCollector()
//	metrics.SetGlobalCollector(collector)
func SetGlobalCollector(c Collector) {
	globalMu.Lock()
	globalCollector = c
	globalMu.Unlock()
}

// GetGlobalCollector returns the global metrics collector.
//
// Returns the currently configured global collector.
//
// Example:
//
//	collector := metrics.GetGlobalCollector()
//	collector.RecordMessageReceived("channel", "inbound", "text")
func GetGlobalCollector() Collector {
	globalMu.RLock()
	defer globalMu.RUnlock()
	return globalCollector
}

// RecordMessageReceived records a received message using the global collector.
//
// Parameters:
//   - channel: The channel ID
//   - direction: The message direction
//   - msgType: The message type
//
// Example:
//
//	metrics.RecordMessageReceived("dingtalk", "inbound", "text")
func RecordMessageReceived(channel, direction, msgType string) {
	globalMu.RLock()
	c := globalCollector
	globalMu.RUnlock()
	c.RecordMessageReceived(channel, direction, msgType)
}

// RecordMessageSent records a sent message using the global collector.
//
// Parameters:
//   - channel: The channel ID
//   - direction: The message direction
//   - msgType: The message type
//
// Example:
//
//	metrics.RecordMessageSent("feishu", "outbound", "image")
func RecordMessageSent(channel, direction, msgType string) {
	globalMu.RLock()
	c := globalCollector
	globalMu.RUnlock()
	c.RecordMessageSent(channel, direction, msgType)
}

// RecordMessageDuration records message processing duration using the global collector.
//
// Parameters:
//   - channel: The channel ID
//   - duration: The processing duration
//
// Example:
//
//	start := time.Now()
//	processMessage(msg)
//	metrics.RecordMessageDuration("dingtalk", time.Since(start))
func RecordMessageDuration(channel string, duration time.Duration) {
	globalMu.RLock()
	c := globalCollector
	globalMu.RUnlock()
	c.RecordMessageDuration(channel, duration)
}

// RecordChannelStatus records channel status using the global collector.
//
// Parameters:
//   - channel: The channel ID
//   - channelType: The type of channel
//   - running: Whether the channel is running
//
// Example:
//
//	metrics.RecordChannelStatus("my-dingtalk", "dingtalk", true)
func RecordChannelStatus(channel, channelType string, running bool) {
	globalMu.RLock()
	c := globalCollector
	globalMu.RUnlock()
	c.RecordChannelStatus(channel, channelType, running)
}

// RecordChannelError records a channel error using the global collector.
//
// Parameters:
//   - channel: The channel ID
//   - channelType: The type of channel
//   - errorType: The type of error
//
// Example:
//
//	metrics.RecordChannelError("my-dingtalk", "dingtalk", "connection")
func RecordChannelError(channel, channelType, errorType string) {
	globalMu.RLock()
	c := globalCollector
	globalMu.RUnlock()
	c.RecordChannelError(channel, channelType, errorType)
}

// RecordSessionActive records active session count using the global collector.
//
// Parameters:
//   - count: The number of active sessions
//
// Example:
//
//	metrics.RecordSessionActive(len(sessions))
func RecordSessionActive(count int) {
	globalMu.RLock()
	c := globalCollector
	globalMu.RUnlock()
	c.RecordSessionActive(count)
}

// RecordSessionDuration records session duration using the global collector.
//
// Parameters:
//   - duration: The session duration
//
// Example:
//
//	metrics.RecordSessionDuration(time.Since(sessionStart))
func RecordSessionDuration(duration time.Duration) {
	globalMu.RLock()
	c := globalCollector
	globalMu.RUnlock()
	c.RecordSessionDuration(duration)
}

// RecordMiddlewareDuration records middleware execution time using the global collector.
//
// Parameters:
//   - middleware: The middleware name
//   - duration: The execution duration
//
// Example:
//
//	start := time.Now()
//	next(ctx, msg)
//	metrics.RecordMiddlewareDuration("auth", time.Since(start))
func RecordMiddlewareDuration(middleware string, duration time.Duration) {
	globalMu.RLock()
	c := globalCollector
	globalMu.RUnlock()
	c.RecordMiddlewareDuration(middleware, duration)
}

// RecordRetry records retry attempts using the global collector.
//
// Parameters:
//   - operation: The operation being retried
//   - attempts: The number of attempts made
//   - success: Whether the operation succeeded
//
// Example:
//
//	metrics.RecordRetry("api-call", 3, true)
func RecordRetry(operation string, attempts int, success bool) {
	globalMu.RLock()
	c := globalCollector
	globalMu.RUnlock()
	c.RecordRetry(operation, attempts, success)
}
