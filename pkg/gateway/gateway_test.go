package gateway

import (
	"context"
	"testing"

	"github.com/DotNetAge/gort/pkg/channel"
	"github.com/DotNetAge/gort/pkg/message"
	"github.com/DotNetAge/gort/pkg/middleware"
	"github.com/DotNetAge/gort/pkg/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	sm := session.NewManager(session.Config{})
	gw := New(sm)

	assert.NotNil(t, gw)
	assert.NotNil(t, gw.registry)
	assert.NotNil(t, gw.sessionManager)
	assert.NotNil(t, gw.middleware)
	assert.False(t, gw.IsRunning())
}

func TestGateway_RegisterChannel(t *testing.T) {
	sm := session.NewManager(session.Config{})
	gw := New(sm)
	ch := channel.NewMockChannel("wechat", channel.ChannelTypeWeChat)

	err := gw.RegisterChannel(ch)
	require.NoError(t, err)

	registered, ok := gw.GetChannel("wechat")
	assert.True(t, ok)
	assert.Equal(t, ch, registered)
}

func TestGateway_RegisterClientHandler(t *testing.T) {
	sm := session.NewManager(session.Config{})
	gw := New(sm)

	handler := func(ctx context.Context, clientID string, msg *message.Message) error {
		return nil
	}

	gw.RegisterClientHandler(handler)
	assert.NotNil(t, gw.clientHandler)
}

func TestGateway_RegisterChannelHandler(t *testing.T) {
	sm := session.NewManager(session.Config{})
	gw := New(sm)

	handler := func(ctx context.Context, msg *message.Message) error {
		return nil
	}

	gw.RegisterChannelHandler(handler)
	assert.NotNil(t, gw.channelHandler)
}

func TestGateway_Start(t *testing.T) {
	sm := session.NewManager(session.Config{})
	gw := New(sm)

	ch := channel.NewMockChannel("wechat", channel.ChannelTypeWeChat)
	err := gw.RegisterChannel(ch)
	require.NoError(t, err)

	err = gw.Start(context.Background())
	require.NoError(t, err)
	assert.True(t, gw.IsRunning())
	assert.True(t, ch.IsRunning())

	gw.Stop(context.Background())
}

func TestGateway_Start_AlreadyRunning(t *testing.T) {
	sm := session.NewManager(session.Config{})
	gw := New(sm)

	ch := channel.NewMockChannel("wechat", channel.ChannelTypeWeChat)
	err := gw.RegisterChannel(ch)
	require.NoError(t, err)

	err = gw.Start(context.Background())
	require.NoError(t, err)

	err = gw.Start(context.Background())
	assert.Equal(t, ErrAlreadyRunning, err)

	gw.Stop(context.Background())
}

func TestGateway_Stop(t *testing.T) {
	sm := session.NewManager(session.Config{})
	gw := New(sm)

	ch := channel.NewMockChannel("wechat", channel.ChannelTypeWeChat)
	err := gw.RegisterChannel(ch)
	require.NoError(t, err)

	err = gw.Start(context.Background())
	require.NoError(t, err)

	err = gw.Stop(context.Background())
	require.NoError(t, err)
	assert.False(t, gw.IsRunning())
	assert.False(t, ch.IsRunning())
}

func TestGateway_Stop_NotRunning(t *testing.T) {
	sm := session.NewManager(session.Config{})
	gw := New(sm)

	err := gw.Stop(context.Background())
	assert.Equal(t, ErrNotRunning, err)
}

func TestGateway_IsRunning(t *testing.T) {
	sm := session.NewManager(session.Config{})
	gw := New(sm)

	assert.False(t, gw.IsRunning())

	ch := channel.NewMockChannel("wechat", channel.ChannelTypeWeChat)
	gw.RegisterChannel(ch)
	gw.Start(context.Background())
	assert.True(t, gw.IsRunning())

	gw.Stop(context.Background())
	assert.False(t, gw.IsRunning())
}

func TestGateway_HandleChannelMessage(t *testing.T) {
	sm := session.NewManager(session.Config{})
	gw := New(sm)

	received := false
	gw.RegisterChannelHandler(func(ctx context.Context, msg *message.Message) error {
		received = true
		return nil
	})

	msg := message.NewMessage("msg_001", "wechat", message.DirectionInbound, message.UserInfo{}, "test", message.MessageTypeText)

	err := gw.HandleChannelMessage(context.Background(), msg)
	require.NoError(t, err)
	assert.True(t, received)
}

func TestGateway_HandleClientMessage(t *testing.T) {
	sm := session.NewManager(session.Config{})
	gw := New(sm)

	ch := channel.NewMockChannel("wechat", channel.ChannelTypeWeChat)
	gw.RegisterChannel(ch)

	msg := message.NewMessage("msg_001", "wechat", message.DirectionOutbound, message.UserInfo{}, "test", message.MessageTypeText)

	err := gw.HandleClientMessage(context.Background(), "client_001", msg)
	require.NoError(t, err)

	messages := ch.GetMessages()
	assert.Len(t, messages, 1)
}

func TestGateway_HandleClientMessage_ChannelNotFound(t *testing.T) {
	sm := session.NewManager(session.Config{})
	gw := New(sm)

	msg := message.NewMessage("msg_001", "nonexistent", message.DirectionOutbound, message.UserInfo{}, "test", message.MessageTypeText)

	err := gw.HandleClientMessage(context.Background(), "client_001", msg)
	assert.Equal(t, ErrChannelNotFound, err)
}

func TestGateway_GetClientCount(t *testing.T) {
	sm := session.NewManager(session.Config{})
	gw := New(sm)

	assert.Equal(t, 0, gw.GetClientCount())
}

func TestGateway_Broadcast(t *testing.T) {
	sm := session.NewManager(session.Config{})
	gw := New(sm)

	msg := message.NewMessage("msg_001", "wechat", message.DirectionInbound, message.UserInfo{}, "test", message.MessageTypeText)

	err := gw.Broadcast(context.Background(), msg)
	require.NoError(t, err)
}

func TestGateway_SendTo(t *testing.T) {
	sm := session.NewManager(session.Config{})
	gw := New(sm)

	msg := message.NewMessage("msg_001", "wechat", message.DirectionInbound, message.UserInfo{}, "test", message.MessageTypeText)

	err := gw.SendTo(context.Background(), "nonexistent", msg)
	assert.Equal(t, session.ErrSessionNotFound, err)
}

func TestGateway_Use(t *testing.T) {
	sm := session.NewManager(session.Config{})
	gw := New(sm)

	called := false
	mw := &testMiddleware{
		name: "test",
		handleFunc: func(ctx context.Context, msg *message.Message, next middleware.Handler) error {
			called = true
			return next(ctx, msg)
		},
	}

	gw.Use(mw)

	msg := message.NewMessage("msg_001", "wechat", message.DirectionInbound, message.UserInfo{}, "test", message.MessageTypeText)
	err := gw.HandleChannelMessage(context.Background(), msg)
	require.NoError(t, err)
	assert.True(t, called)
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
