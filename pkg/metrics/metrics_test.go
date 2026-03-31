package metrics

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultCollector(t *testing.T) {
	collector := &DefaultCollector{}

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

func TestStatsCollector_New(t *testing.T) {
	collector := NewStatsCollector()
	require.NotNil(t, collector)

	stats := collector.GetStats()
	assert.Equal(t, int64(0), stats.MessagesReceived)
	assert.Equal(t, int64(0), stats.MessagesSent)
	assert.Equal(t, int64(0), stats.Errors)
	assert.Equal(t, int64(0), stats.ActiveSessions)
}

func TestStatsCollector_RecordMessageReceived(t *testing.T) {
	collector := NewStatsCollector()

	for i := 0; i < 5; i++ {
		collector.RecordMessageReceived("wechat", "inbound", "text")
	}

	stats := collector.GetStats()
	assert.Equal(t, int64(5), stats.MessagesReceived)
}

func TestStatsCollector_RecordMessageSent(t *testing.T) {
	collector := NewStatsCollector()

	for i := 0; i < 3; i++ {
		collector.RecordMessageSent("wechat", "outbound", "text")
	}

	stats := collector.GetStats()
	assert.Equal(t, int64(3), stats.MessagesSent)
}

func TestStatsCollector_RecordChannelError(t *testing.T) {
	collector := NewStatsCollector()

	collector.RecordChannelError("wechat", "wechat", "timeout")
	collector.RecordChannelError("wechat", "wechat", "rate_limit")

	stats := collector.GetStats()
	assert.Equal(t, int64(2), stats.Errors)
}

func TestStatsCollector_RecordSessionActive(t *testing.T) {
	collector := NewStatsCollector()

	collector.RecordSessionActive(10)
	stats := collector.GetStats()
	assert.Equal(t, int64(10), stats.ActiveSessions)

	collector.RecordSessionActive(5)
	stats = collector.GetStats()
	assert.Equal(t, int64(5), stats.ActiveSessions)
}

func TestStatsCollector_Concurrent(t *testing.T) {
	collector := NewStatsCollector()
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			collector.RecordMessageReceived("wechat", "inbound", "text")
		}()
		go func() {
			defer wg.Done()
			collector.RecordMessageSent("wechat", "outbound", "text")
		}()
		go func() {
			defer wg.Done()
			collector.RecordChannelError("wechat", "wechat", "error")
		}()
	}

	wg.Wait()

	stats := collector.GetStats()
	assert.Equal(t, int64(100), stats.MessagesReceived)
	assert.Equal(t, int64(100), stats.MessagesSent)
	assert.Equal(t, int64(100), stats.Errors)
}

func TestGlobalCollector_Default(t *testing.T) {
	original := GetGlobalCollector()
	defer SetGlobalCollector(original)

	collector := GetGlobalCollector()
	assert.NotNil(t, collector)

	_, ok := collector.(*DefaultCollector)
	assert.True(t, ok)
}

func TestGlobalCollector_Set(t *testing.T) {
	original := GetGlobalCollector()
	defer SetGlobalCollector(original)

	customCollector := NewStatsCollector()
	SetGlobalCollector(customCollector)

	retrieved := GetGlobalCollector()
	assert.Equal(t, customCollector, retrieved)
}

func TestGlobalFunctions(t *testing.T) {
	original := GetGlobalCollector()
	defer SetGlobalCollector(original)

	collector := NewStatsCollector()
	SetGlobalCollector(collector)

	RecordMessageReceived("wechat", "inbound", "text")
	RecordMessageSent("wechat", "outbound", "text")
	RecordMessageDuration("wechat", time.Millisecond*100)
	RecordChannelStatus("wechat", "wechat", true)
	RecordChannelError("wechat", "wechat", "timeout")
	RecordSessionActive(10)
	RecordSessionDuration(time.Second * 60)
	RecordMiddlewareDuration("auth", time.Millisecond*10)
	RecordRetry("send", 3, true)

	stats := collector.GetStats()
	assert.Equal(t, int64(1), stats.MessagesReceived)
	assert.Equal(t, int64(1), stats.MessagesSent)
	assert.Equal(t, int64(1), stats.Errors)
	assert.Equal(t, int64(10), stats.ActiveSessions)
}

func TestStatsCollector_AllMethods(t *testing.T) {
	collector := NewStatsCollector()

	collector.RecordMessageReceived("wechat", "inbound", "text")
	collector.RecordMessageSent("dingtalk", "outbound", "image")
	collector.RecordMessageDuration("feishu", time.Second)
	collector.RecordChannelStatus("telegram", "telegram", true)
	collector.RecordChannelError("wechat", "wechat", "auth_failed")
	collector.RecordSessionActive(5)
	collector.RecordSessionDuration(time.Minute)
	collector.RecordMiddlewareDuration("logging", time.Millisecond)
	collector.RecordRetry("send_message", 3, false)

	stats := collector.GetStats()
	assert.Equal(t, int64(1), stats.MessagesReceived)
	assert.Equal(t, int64(1), stats.MessagesSent)
	assert.Equal(t, int64(1), stats.Errors)
	assert.Equal(t, int64(5), stats.ActiveSessions)
}

func TestStatsCollector_GetStats_Copy(t *testing.T) {
	collector := NewStatsCollector()

	collector.RecordMessageReceived("wechat", "inbound", "text")
	stats1 := collector.GetStats()

	collector.RecordMessageReceived("wechat", "inbound", "text")
	stats2 := collector.GetStats()

	assert.Equal(t, int64(1), stats1.MessagesReceived)
	assert.Equal(t, int64(2), stats2.MessagesReceived)
}

func TestCollectorInterface(t *testing.T) {
	var _ Collector = (*DefaultCollector)(nil)
	var _ Collector = (*StatsCollector)(nil)
}
