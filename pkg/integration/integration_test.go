package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/example/gort/pkg/channel"
	"github.com/example/gort/pkg/gateway"
	"github.com/example/gort/pkg/message"
	"github.com/example/gort/pkg/metrics"
	"github.com/example/gort/pkg/middleware"
	"github.com/example/gort/pkg/session"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func TestGateway_Integration_StartStop(t *testing.T) {
	sm := session.NewManager(session.Config{})
	gw := gateway.New(sm)

	ch1 := channel.NewMockChannel("wechat", channel.ChannelTypeWeChat)
	ch2 := channel.NewMockChannel("dingtalk", channel.ChannelTypeDingTalk)

	require.NoError(t, gw.RegisterChannel(ch1))
	require.NoError(t, gw.RegisterChannel(ch2))

	ctx := context.Background()
	require.NoError(t, gw.Start(ctx))
	assert.True(t, gw.IsRunning())
	assert.True(t, ch1.IsRunning())
	assert.True(t, ch2.IsRunning())

	require.NoError(t, gw.Stop(ctx))
	assert.False(t, gw.IsRunning())
	assert.False(t, ch1.IsRunning())
	assert.False(t, ch2.IsRunning())
}

func TestGateway_Integration_MessageFlow(t *testing.T) {
	sm := session.NewManager(session.Config{})
	gw := gateway.New(sm)

	var receivedMessages []*message.Message
	var mu sync.Mutex

	gw.RegisterChannelHandler(func(ctx context.Context, msg *message.Message) error {
		mu.Lock()
		receivedMessages = append(receivedMessages, msg)
		mu.Unlock()
		return nil
	})

	ch := channel.NewMockChannel("wechat", channel.ChannelTypeWeChat)
	require.NoError(t, gw.RegisterChannel(ch))

	ctx := context.Background()
	require.NoError(t, gw.Start(ctx))
	defer gw.Stop(ctx)

	msg := message.NewMessage("msg_001", "wechat", message.DirectionInbound, message.UserInfo{}, "hello", message.MessageTypeText)
	require.NoError(t, ch.SimulateMessage(ctx, msg))

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	assert.Len(t, receivedMessages, 1)
	assert.Equal(t, "hello", receivedMessages[0].Content)
	mu.Unlock()
}

func TestGateway_Integration_Middleware(t *testing.T) {
	sm := session.NewManager(session.Config{})
	gw := gateway.New(sm)

	var middlewareCalls []string
	var mu sync.Mutex

	authMiddleware := &testMiddleware{
		name: "auth",
		handleFunc: func(ctx context.Context, msg *message.Message, next middleware.Handler) error {
			mu.Lock()
			middlewareCalls = append(middlewareCalls, "auth")
			mu.Unlock()
			return next(ctx, msg)
		},
	}

	loggingMiddleware := &testMiddleware{
		name: "logging",
		handleFunc: func(ctx context.Context, msg *message.Message, next middleware.Handler) error {
			mu.Lock()
			middlewareCalls = append(middlewareCalls, "logging")
			mu.Unlock()
			return next(ctx, msg)
		},
	}

	gw.Use(authMiddleware)
	gw.Use(loggingMiddleware)

	var received bool
	gw.RegisterChannelHandler(func(ctx context.Context, msg *message.Message) error {
		mu.Lock()
		received = true
		mu.Unlock()
		return nil
	})

	ch := channel.NewMockChannel("wechat", channel.ChannelTypeWeChat)
	require.NoError(t, gw.RegisterChannel(ch))

	ctx := context.Background()
	require.NoError(t, gw.Start(ctx))
	defer gw.Stop(ctx)

	msg := message.NewMessage("msg_001", "wechat", message.DirectionInbound, message.UserInfo{}, "test", message.MessageTypeText)
	require.NoError(t, ch.SimulateMessage(ctx, msg))

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	assert.True(t, received)
	assert.Equal(t, []string{"auth", "logging"}, middlewareCalls)
	mu.Unlock()
}

func TestGateway_Integration_Metrics(t *testing.T) {
	collector := metrics.NewStatsCollector()
	metrics.SetGlobalCollector(collector)
	defer metrics.SetGlobalCollector(&metrics.DefaultCollector{})

	sm := session.NewManager(session.Config{})
	gw := gateway.New(sm)

	ch := channel.NewMockChannel("wechat", channel.ChannelTypeWeChat)
	require.NoError(t, gw.RegisterChannel(ch))

	ctx := context.Background()
	require.NoError(t, gw.Start(ctx))
	defer gw.Stop(ctx)

	metrics.RecordMessageReceived("wechat", "inbound", "text")
	metrics.RecordMessageSent("wechat", "outbound", "text")

	stats := collector.GetStats()
	assert.Equal(t, int64(1), stats.MessagesReceived)
	assert.Equal(t, int64(1), stats.MessagesSent)
}

func TestGateway_Integration_WebSocket(t *testing.T) {
	var wsConn *websocket.Conn
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		wsConn, err = upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
	}))
	defer server.Close()

	sm := session.NewManager(session.Config{})
	gw := gateway.New(sm)

	ch := channel.NewMockChannel("wechat", channel.ChannelTypeWeChat)
	require.NoError(t, gw.RegisterChannel(ch))

	ctx := context.Background()
	require.NoError(t, gw.Start(ctx))
	defer gw.Stop(ctx)

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	clientConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer clientConn.Close()

	time.Sleep(50 * time.Millisecond)

	if wsConn != nil {
		msg := message.NewMessage("msg_001", "wechat", message.DirectionInbound, message.UserInfo{}, "test", message.MessageTypeText)
		data, err := json.Marshal(msg)
		require.NoError(t, err)
		require.NoError(t, wsConn.WriteMessage(websocket.TextMessage, data))
	}
}

func TestGateway_Integration_MultipleChannels(t *testing.T) {
	sm := session.NewManager(session.Config{})
	gw := gateway.New(sm)

	channels := []struct {
		name string
		typ  channel.ChannelType
	}{
		{"wechat", channel.ChannelTypeWeChat},
		{"dingtalk", channel.ChannelTypeDingTalk},
		{"feishu", channel.ChannelTypeFeishu},
		{"telegram", channel.ChannelTypeTelegram},
	}

	for _, c := range channels {
		ch := channel.NewMockChannel(c.name, c.typ)
		require.NoError(t, gw.RegisterChannel(ch))
	}

	ctx := context.Background()
	require.NoError(t, gw.Start(ctx))
	defer gw.Stop(ctx)

	for _, c := range channels {
		ch, ok := gw.GetChannel(c.name)
		assert.True(t, ok)
		assert.True(t, ch.IsRunning())
	}
}

func TestGateway_Integration_ErrorHandling(t *testing.T) {
	sm := session.NewManager(session.Config{})
	gw := gateway.New(sm)

	var errorCount int
	var mu sync.Mutex

	errorMiddleware := &testMiddleware{
		name: "error_handler",
		handleFunc: func(ctx context.Context, msg *message.Message, next middleware.Handler) error {
			err := next(ctx, msg)
			if err != nil {
				mu.Lock()
				errorCount++
				mu.Unlock()
			}
			return err
		},
	}

	gw.Use(errorMiddleware)

	gw.RegisterChannelHandler(func(ctx context.Context, msg *message.Message) error {
		return assert.AnError
	})

	ch := channel.NewMockChannel("wechat", channel.ChannelTypeWeChat)
	require.NoError(t, gw.RegisterChannel(ch))

	ctx := context.Background()
	require.NoError(t, gw.Start(ctx))
	defer gw.Stop(ctx)

	msg := message.NewMessage("msg_001", "wechat", message.DirectionInbound, message.UserInfo{}, "test", message.MessageTypeText)
	_ = ch.SimulateMessage(ctx, msg)

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	assert.Equal(t, 1, errorCount)
	mu.Unlock()
}

type testMiddleware struct {
	name       string
	handleFunc func(ctx context.Context, msg *message.Message, next middleware.Handler) error
}

func (m *testMiddleware) Name() string {
	return m.name
}

func (m *testMiddleware) Handle(ctx context.Context, msg *message.Message, next middleware.Handler) error {
	if m.handleFunc != nil {
		return m.handleFunc(ctx, msg, next)
	}
	return next(ctx, msg)
}
