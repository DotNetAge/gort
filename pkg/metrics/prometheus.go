package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const namespace = "gort"

// PrometheusCollector implements the Collector interface using Prometheus metrics.
type PrometheusCollector struct {
	messagesReceived *prometheus.CounterVec
	messagesSent     *prometheus.CounterVec
	messageDuration  *prometheus.HistogramVec
	channelStatus    *prometheus.GaugeVec
	channelErrors    *prometheus.CounterVec
	sessionsActive   prometheus.Gauge
	sessionDuration  prometheus.Histogram
	middlewareDuration *prometheus.HistogramVec
	retryAttempts    *prometheus.CounterVec
}

// NewPrometheusCollector creates a new PrometheusCollector with default metrics.
func NewPrometheusCollector() *PrometheusCollector {
	return NewPrometheusCollectorWithRegistry(prometheus.DefaultRegisterer)
}

// NewPrometheusCollectorWithRegistry creates a new PrometheusCollector with a custom registry.
func NewPrometheusCollectorWithRegistry(reg prometheus.Registerer) *PrometheusCollector {
	factory := promauto.With(reg)

	return &PrometheusCollector{
		messagesReceived: factory.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "messages_received_total",
				Help:      "Total number of messages received by channel.",
			},
			[]string{"channel", "direction", "type"},
		),
		messagesSent: factory.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "messages_sent_total",
				Help:      "Total number of messages sent by channel.",
			},
			[]string{"channel", "direction", "type"},
		),
		messageDuration: factory.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Name:      "message_duration_seconds",
				Help:      "Duration of message processing in seconds.",
				Buckets:   prometheus.DefBuckets,
			},
			[]string{"channel"},
		),
		channelStatus: factory.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "channel_status",
				Help:      "Current status of the channel (1=running, 0=stopped).",
			},
			[]string{"channel", "type"},
		),
		channelErrors: factory.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "channel_errors_total",
				Help:      "Total number of channel errors.",
			},
			[]string{"channel", "type", "error_type"},
		),
		sessionsActive: factory.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Name:      "sessions_active",
				Help:      "Number of active sessions.",
			},
		),
		sessionDuration: factory.NewHistogram(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Name:      "session_duration_seconds",
				Help:      "Duration of sessions in seconds.",
				Buckets:   prometheus.DefBuckets,
			},
		),
		middlewareDuration: factory.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Name:      "middleware_duration_seconds",
				Help:      "Duration of middleware execution in seconds.",
				Buckets:   prometheus.DefBuckets,
			},
			[]string{"middleware"},
		),
		retryAttempts: factory.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "retry_attempts_total",
				Help:      "Total number of retry attempts.",
			},
			[]string{"operation", "success"},
		),
	}
}

// RecordMessageReceived increments the received messages counter.
func (p *PrometheusCollector) RecordMessageReceived(channel, direction, msgType string) {
	p.messagesReceived.WithLabelValues(channel, direction, msgType).Inc()
}

// RecordMessageSent increments the sent messages counter.
func (p *PrometheusCollector) RecordMessageSent(channel, direction, msgType string) {
	p.messagesSent.WithLabelValues(channel, direction, msgType).Inc()
}

// RecordMessageDuration records the duration of message processing.
func (p *PrometheusCollector) RecordMessageDuration(channel string, duration time.Duration) {
	p.messageDuration.WithLabelValues(channel).Observe(duration.Seconds())
}

// RecordChannelStatus updates the channel status gauge.
func (p *PrometheusCollector) RecordChannelStatus(channel, channelType string, running bool) {
	value := float64(0)
	if running {
		value = 1
	}
	p.channelStatus.WithLabelValues(channel, channelType).Set(value)
}

// RecordChannelError increments the channel errors counter.
func (p *PrometheusCollector) RecordChannelError(channel, channelType, errorType string) {
	p.channelErrors.WithLabelValues(channel, channelType, errorType).Inc()
}

// RecordSessionActive updates the active sessions gauge.
func (p *PrometheusCollector) RecordSessionActive(count int) {
	p.sessionsActive.Set(float64(count))
}

// RecordSessionDuration records the duration of a session.
func (p *PrometheusCollector) RecordSessionDuration(duration time.Duration) {
	p.sessionDuration.Observe(duration.Seconds())
}

// RecordMiddlewareDuration records the duration of middleware execution.
func (p *PrometheusCollector) RecordMiddlewareDuration(middleware string, duration time.Duration) {
	p.middlewareDuration.WithLabelValues(middleware).Observe(duration.Seconds())
}

// RecordRetry records retry attempts.
func (p *PrometheusCollector) RecordRetry(operation string, attempts int, success bool) {
	successStr := "false"
	if success {
		successStr = "true"
	}
	p.retryAttempts.WithLabelValues(operation, successStr).Add(float64(attempts))
}
