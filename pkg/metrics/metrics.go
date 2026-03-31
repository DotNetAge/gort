package metrics

import (
	"sync"
	"time"
)

// Collector defines the interface for collecting metrics.
type Collector interface {
	RecordMessageReceived(channel, direction, msgType string)
	RecordMessageSent(channel, direction, msgType string)
	RecordMessageDuration(channel string, duration time.Duration)
	RecordChannelStatus(channel, channelType string, running bool)
	RecordChannelError(channel, channelType, errorType string)
	RecordSessionActive(count int)
	RecordSessionDuration(duration time.Duration)
	RecordMiddlewareDuration(middleware string, duration time.Duration)
	RecordRetry(operation string, attempts int, success bool)
}

// DefaultCollector is a no-op collector.
type DefaultCollector struct{}

func (d *DefaultCollector) RecordMessageReceived(channel, direction, msgType string)           {}
func (d *DefaultCollector) RecordMessageSent(channel, direction, msgType string)               {}
func (d *DefaultCollector) RecordMessageDuration(channel string, duration time.Duration)       {}
func (d *DefaultCollector) RecordChannelStatus(channel, channelType string, running bool)      {}
func (d *DefaultCollector) RecordChannelError(channel, channelType, errorType string)          {}
func (d *DefaultCollector) RecordSessionActive(count int)                                      {}
func (d *DefaultCollector) RecordSessionDuration(duration time.Duration)                       {}
func (d *DefaultCollector) RecordMiddlewareDuration(middleware string, duration time.Duration) {}
func (d *DefaultCollector) RecordRetry(operation string, attempts int, success bool)           {}

// Stats holds basic statistics.
type Stats struct {
	MessagesReceived int64
	MessagesSent     int64
	Errors           int64
	ActiveSessions   int64
}

// StatsCollector is a simple collector that tracks basic statistics.
type StatsCollector struct {
	mu    sync.RWMutex
	stats Stats
}

// NewStatsCollector creates a new StatsCollector.
func NewStatsCollector() *StatsCollector {
	return &StatsCollector{
		stats: Stats{},
	}
}

func (s *StatsCollector) RecordMessageReceived(channel, direction, msgType string) {
	s.mu.Lock()
	s.stats.MessagesReceived++
	s.mu.Unlock()
}

func (s *StatsCollector) RecordMessageSent(channel, direction, msgType string) {
	s.mu.Lock()
	s.stats.MessagesSent++
	s.mu.Unlock()
}

func (s *StatsCollector) RecordMessageDuration(channel string, duration time.Duration) {}

func (s *StatsCollector) RecordChannelStatus(channel, channelType string, running bool) {}

func (s *StatsCollector) RecordChannelError(channel, channelType, errorType string) {
	s.mu.Lock()
	s.stats.Errors++
	s.mu.Unlock()
}

func (s *StatsCollector) RecordSessionActive(count int) {
	s.mu.Lock()
	s.stats.ActiveSessions = int64(count)
	s.mu.Unlock()
}

func (s *StatsCollector) RecordSessionDuration(duration time.Duration) {}

func (s *StatsCollector) RecordMiddlewareDuration(middleware string, duration time.Duration) {
}

func (s *StatsCollector) RecordRetry(operation string, attempts int, success bool) {
}

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
func SetGlobalCollector(c Collector) {
	globalMu.Lock()
	globalCollector = c
	globalMu.Unlock()
}

// GetGlobalCollector returns the global metrics collector.
func GetGlobalCollector() Collector {
	globalMu.RLock()
	defer globalMu.RUnlock()
	return globalCollector
}

// RecordMessageReceived records a received message using the global collector.
func RecordMessageReceived(channel, direction, msgType string) {
	globalMu.RLock()
	c := globalCollector
	globalMu.RUnlock()
	c.RecordMessageReceived(channel, direction, msgType)
}

// RecordMessageSent records a sent message using the global collector.
func RecordMessageSent(channel, direction, msgType string) {
	globalMu.RLock()
	c := globalCollector
	globalMu.RUnlock()
	c.RecordMessageSent(channel, direction, msgType)
}

// RecordMessageDuration records message processing duration.
func RecordMessageDuration(channel string, duration time.Duration) {
	globalMu.RLock()
	c := globalCollector
	globalMu.RUnlock()
	c.RecordMessageDuration(channel, duration)
}

// RecordChannelStatus records channel status.
func RecordChannelStatus(channel, channelType string, running bool) {
	globalMu.RLock()
	c := globalCollector
	globalMu.RUnlock()
	c.RecordChannelStatus(channel, channelType, running)
}

// RecordChannelError records a channel error.
func RecordChannelError(channel, channelType, errorType string) {
	globalMu.RLock()
	c := globalCollector
	globalMu.RUnlock()
	c.RecordChannelError(channel, channelType, errorType)
}

// RecordSessionActive records active session count.
func RecordSessionActive(count int) {
	globalMu.RLock()
	c := globalCollector
	globalMu.RUnlock()
	c.RecordSessionActive(count)
}

// RecordSessionDuration records session duration.
func RecordSessionDuration(duration time.Duration) {
	globalMu.RLock()
	c := globalCollector
	globalMu.RUnlock()
	c.RecordSessionDuration(duration)
}

// RecordMiddlewareDuration records middleware execution time.
func RecordMiddlewareDuration(middleware string, duration time.Duration) {
	globalMu.RLock()
	c := globalCollector
	globalMu.RUnlock()
	c.RecordMiddlewareDuration(middleware, duration)
}

// RecordRetry records retry attempts.
func RecordRetry(operation string, attempts int, success bool) {
	globalMu.RLock()
	c := globalCollector
	globalMu.RUnlock()
	c.RecordRetry(operation, attempts, success)
}
