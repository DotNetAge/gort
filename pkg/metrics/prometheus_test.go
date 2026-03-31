package metrics

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrometheusCollector_New(t *testing.T) {
	registry := prometheus.NewRegistry()
	collector := NewPrometheusCollectorWithRegistry(registry)
	require.NotNil(t, collector)
}

func TestPrometheusCollector_RecordMessageReceived(t *testing.T) {
	registry := prometheus.NewRegistry()
	collector := NewPrometheusCollectorWithRegistry(registry)

	collector.RecordMessageReceived("wechat", "inbound", "text")
	collector.RecordMessageReceived("wechat", "inbound", "text")
	collector.RecordMessageReceived("dingtalk", "outbound", "image")

	wechatCount := testutil.ToFloat64(collector.messagesReceived.WithLabelValues("wechat", "inbound", "text"))
	dingtalkCount := testutil.ToFloat64(collector.messagesReceived.WithLabelValues("dingtalk", "outbound", "image"))

	assert.Equal(t, float64(2), wechatCount)
	assert.Equal(t, float64(1), dingtalkCount)
}

func TestPrometheusCollector_RecordMessageSent(t *testing.T) {
	registry := prometheus.NewRegistry()
	collector := NewPrometheusCollectorWithRegistry(registry)

	collector.RecordMessageSent("wechat", "outbound", "text")
	collector.RecordMessageSent("wechat", "outbound", "text")

	count := testutil.ToFloat64(collector.messagesSent.WithLabelValues("wechat", "outbound", "text"))
	assert.Equal(t, float64(2), count)
}

func TestPrometheusCollector_RecordMessageDuration(t *testing.T) {
	registry := prometheus.NewRegistry()
	collector := NewPrometheusCollectorWithRegistry(registry)

	collector.RecordMessageDuration("wechat", 100*time.Millisecond)
	collector.RecordMessageDuration("wechat", 200*time.Millisecond)

	assert.NotNil(t, collector.messageDuration)
}

func TestPrometheusCollector_RecordChannelStatus(t *testing.T) {
	registry := prometheus.NewRegistry()
	collector := NewPrometheusCollectorWithRegistry(registry)

	collector.RecordChannelStatus("wechat", "wechat", true)
	collector.RecordChannelStatus("dingtalk", "dingtalk", false)

	statusWechat := testutil.ToFloat64(collector.channelStatus.WithLabelValues("wechat", "wechat"))
	statusDingtalk := testutil.ToFloat64(collector.channelStatus.WithLabelValues("dingtalk", "dingtalk"))

	assert.Equal(t, float64(1), statusWechat)
	assert.Equal(t, float64(0), statusDingtalk)
}

func TestPrometheusCollector_RecordChannelError(t *testing.T) {
	registry := prometheus.NewRegistry()
	collector := NewPrometheusCollectorWithRegistry(registry)

	collector.RecordChannelError("wechat", "wechat", "timeout")
	collector.RecordChannelError("wechat", "wechat", "timeout")
	collector.RecordChannelError("wechat", "wechat", "rate_limit")

	timeoutCount := testutil.ToFloat64(collector.channelErrors.WithLabelValues("wechat", "wechat", "timeout"))
	rateLimitCount := testutil.ToFloat64(collector.channelErrors.WithLabelValues("wechat", "wechat", "rate_limit"))

	assert.Equal(t, float64(2), timeoutCount)
	assert.Equal(t, float64(1), rateLimitCount)
}

func TestPrometheusCollector_RecordSessionActive(t *testing.T) {
	registry := prometheus.NewRegistry()
	collector := NewPrometheusCollectorWithRegistry(registry)

	collector.RecordSessionActive(10)
	count := testutil.ToFloat64(collector.sessionsActive)
	assert.Equal(t, float64(10), count)

	collector.RecordSessionActive(5)
	count = testutil.ToFloat64(collector.sessionsActive)
	assert.Equal(t, float64(5), count)
}

func TestPrometheusCollector_RecordSessionDuration(t *testing.T) {
	registry := prometheus.NewRegistry()
	collector := NewPrometheusCollectorWithRegistry(registry)

	collector.RecordSessionDuration(time.Minute)
	collector.RecordSessionDuration(2 * time.Minute)

	assert.NotNil(t, collector.sessionDuration)
}

func TestPrometheusCollector_RecordMiddlewareDuration(t *testing.T) {
	registry := prometheus.NewRegistry()
	collector := NewPrometheusCollectorWithRegistry(registry)

	collector.RecordMiddlewareDuration("auth", 10*time.Millisecond)
	collector.RecordMiddlewareDuration("auth", 20*time.Millisecond)
	collector.RecordMiddlewareDuration("logging", 5*time.Millisecond)

	assert.NotNil(t, collector.middlewareDuration)
}

func TestPrometheusCollector_RecordRetry(t *testing.T) {
	registry := prometheus.NewRegistry()
	collector := NewPrometheusCollectorWithRegistry(registry)

	collector.RecordRetry("send", 3, true)
	collector.RecordRetry("send", 5, false)
	collector.RecordRetry("receive", 2, true)

	sendTrue := testutil.ToFloat64(collector.retryAttempts.WithLabelValues("send", "true"))
	sendFalse := testutil.ToFloat64(collector.retryAttempts.WithLabelValues("send", "false"))
	receiveTrue := testutil.ToFloat64(collector.retryAttempts.WithLabelValues("receive", "true"))

	assert.Equal(t, float64(3), sendTrue)
	assert.Equal(t, float64(5), sendFalse)
	assert.Equal(t, float64(2), receiveTrue)
}

func TestPrometheusCollector_Interface(t *testing.T) {
	registry := prometheus.NewRegistry()
	collector := NewPrometheusCollectorWithRegistry(registry)

	var _ Collector = collector
}

func TestPrometheusCollector_AllMethods(t *testing.T) {
	registry := prometheus.NewRegistry()
	collector := NewPrometheusCollectorWithRegistry(registry)

	collector.RecordMessageReceived("wechat", "inbound", "text")
	collector.RecordMessageSent("wechat", "outbound", "text")
	collector.RecordMessageDuration("wechat", time.Millisecond*100)
	collector.RecordChannelStatus("wechat", "wechat", true)
	collector.RecordChannelError("wechat", "wechat", "timeout")
	collector.RecordSessionActive(10)
	collector.RecordSessionDuration(time.Second * 60)
	collector.RecordMiddlewareDuration("auth", time.Millisecond*10)
	collector.RecordRetry("send", 3, true)
}
