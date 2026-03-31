package middleware

import (
	"context"
	"errors"
	"testing"

	"github.com/example/gort/pkg/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockLogger struct {
	infoMessages  []string
	errorMessages []string
}

func (m *mockLogger) Info(msg string, fields ...interface{}) {
	m.infoMessages = append(m.infoMessages, msg)
}

func (m *mockLogger) Error(msg string, fields ...interface{}) {
	m.errorMessages = append(m.errorMessages, msg)
}

func TestNewChain(t *testing.T) {
	chain := NewChain()
	assert.NotNil(t, chain)
	assert.Empty(t, chain.middlewares)
}

func TestChain_Use(t *testing.T) {
	chain := NewChain()
	m := &testMiddleware{name: "test"}

	chain.Use(m)

	assert.Len(t, chain.middlewares, 1)
	assert.Equal(t, m, chain.middlewares[0])
}

func TestChain_Execute_EmptyChain(t *testing.T) {
	chain := NewChain()
	msg := message.NewMessage("msg_001", "wechat", message.DirectionInbound, message.UserInfo{}, "test", message.MessageTypeText)
	called := false

	handler := func(ctx context.Context, msg *message.Message) error {
		called = true
		return nil
	}

	err := chain.Execute(context.Background(), msg, handler)
	require.NoError(t, err)
	assert.True(t, called)
}

func TestChain_Execute_SingleMiddleware(t *testing.T) {
	chain := NewChain()
	m := &testMiddleware{name: "test", shouldCallNext: true}
	chain.Use(m)

	msg := message.NewMessage("msg_001", "wechat", message.DirectionInbound, message.UserInfo{}, "test", message.MessageTypeText)
	handlerCalled := false

	handler := func(ctx context.Context, msg *message.Message) error {
		handlerCalled = true
		return nil
	}

	err := chain.Execute(context.Background(), msg, handler)
	require.NoError(t, err)
	assert.True(t, handlerCalled)
	assert.True(t, m.called)
}

func TestChain_Execute_MultipleMiddlewares(t *testing.T) {
	chain := NewChain()
	m1 := &testMiddleware{name: "m1", shouldCallNext: true}
	m2 := &testMiddleware{name: "m2", shouldCallNext: true}
	m3 := &testMiddleware{name: "m3", shouldCallNext: true}

	chain.Use(m1)
	chain.Use(m2)
	chain.Use(m3)

	msg := message.NewMessage("msg_001", "wechat", message.DirectionInbound, message.UserInfo{}, "test", message.MessageTypeText)
	handlerCalled := false

	handler := func(ctx context.Context, msg *message.Message) error {
		handlerCalled = true
		return nil
	}

	err := chain.Execute(context.Background(), msg, handler)
	require.NoError(t, err)
	assert.True(t, handlerCalled)
	assert.True(t, m1.called)
	assert.True(t, m2.called)
	assert.True(t, m3.called)
}

func TestChain_Execute_MiddlewareReturnsError(t *testing.T) {
	chain := NewChain()
	expectedErr := errors.New("middleware error")
	m := &testMiddleware{name: "test", shouldCallNext: false, err: expectedErr}
	chain.Use(m)

	msg := message.NewMessage("msg_001", "wechat", message.DirectionInbound, message.UserInfo{}, "test", message.MessageTypeText)
	handlerCalled := false

	handler := func(ctx context.Context, msg *message.Message) error {
		handlerCalled = true
		return nil
	}

	err := chain.Execute(context.Background(), msg, handler)
	assert.Equal(t, expectedErr, err)
	assert.False(t, handlerCalled)
}

func TestLoggingMiddleware_Name(t *testing.T) {
	logger := &mockLogger{}
	m := NewLoggingMiddleware(logger)
	assert.Equal(t, "Logging", m.Name())
}

func TestLoggingMiddleware_Handle(t *testing.T) {
	logger := &mockLogger{}
	m := NewLoggingMiddleware(logger)

	msg := message.NewMessage("msg_001", "wechat", message.DirectionInbound, message.UserInfo{}, "test", message.MessageTypeText)
	handlerCalled := false

	handler := func(ctx context.Context, msg *message.Message) error {
		handlerCalled = true
		return nil
	}

	err := m.Handle(context.Background(), msg, handler)
	require.NoError(t, err)
	assert.True(t, handlerCalled)
	assert.Len(t, logger.infoMessages, 2)
}

func TestLoggingMiddleware_Handle_Error(t *testing.T) {
	logger := &mockLogger{}
	m := NewLoggingMiddleware(logger)

	msg := message.NewMessage("msg_001", "wechat", message.DirectionInbound, message.UserInfo{}, "test", message.MessageTypeText)
	expectedErr := errors.New("handler error")

	handler := func(ctx context.Context, msg *message.Message) error {
		return expectedErr
	}

	err := m.Handle(context.Background(), msg, handler)
	assert.Equal(t, expectedErr, err)
	assert.Len(t, logger.errorMessages, 1)
}

func TestTraceMiddleware_Name(t *testing.T) {
	m := NewTraceMiddleware(func() string { return "trace_001" })
	assert.Equal(t, "Trace", m.Name())
}

func TestTraceMiddleware_Handle_NoExistingTraceID(t *testing.T) {
	traceID := "trace_12345"
	m := NewTraceMiddleware(func() string { return traceID })

	msg := message.NewMessage("msg_001", "wechat", message.DirectionInbound, message.UserInfo{}, "test", message.MessageTypeText)
	handlerCalled := false

	handler := func(ctx context.Context, msg *message.Message) error {
		handlerCalled = true
		return nil
	}

	err := m.Handle(context.Background(), msg, handler)
	require.NoError(t, err)
	assert.True(t, handlerCalled)

	actualTraceID, ok := msg.GetMetadata("trace_id")
	assert.True(t, ok)
	assert.Equal(t, traceID, actualTraceID)
}

func TestTraceMiddleware_Handle_ExistingTraceID(t *testing.T) {
	m := NewTraceMiddleware(func() string { return "new_trace" })

	msg := message.NewMessage("msg_001", "wechat", message.DirectionInbound, message.UserInfo{}, "test", message.MessageTypeText)
	existingTraceID := "existing_trace"
	msg.SetMetadata("trace_id", existingTraceID)

	handler := func(ctx context.Context, msg *message.Message) error {
		return nil
	}

	err := m.Handle(context.Background(), msg, handler)
	require.NoError(t, err)

	actualTraceID, ok := msg.GetMetadata("trace_id")
	assert.True(t, ok)
	assert.Equal(t, existingTraceID, actualTraceID)
}

func TestAuthMiddleware_Name(t *testing.T) {
	m := NewAuthMiddleware(func(token string) bool { return true })
	assert.Equal(t, "Auth", m.Name())
}

func TestAuthMiddleware_Handle_ValidToken(t *testing.T) {
	m := NewAuthMiddleware(func(token string) bool {
		return token == "valid_token"
	})

	msg := message.NewMessage("msg_001", "wechat", message.DirectionInbound, message.UserInfo{}, "test", message.MessageTypeText)
	msg.SetMetadata("token", "valid_token")

	handlerCalled := false
	handler := func(ctx context.Context, msg *message.Message) error {
		handlerCalled = true
		return nil
	}

	err := m.Handle(context.Background(), msg, handler)
	require.NoError(t, err)
	assert.True(t, handlerCalled)
}

func TestAuthMiddleware_Handle_InvalidToken(t *testing.T) {
	m := NewAuthMiddleware(func(token string) bool {
		return token == "valid_token"
	})

	msg := message.NewMessage("msg_001", "wechat", message.DirectionInbound, message.UserInfo{}, "test", message.MessageTypeText)
	msg.SetMetadata("token", "invalid_token")

	handlerCalled := false
	handler := func(ctx context.Context, msg *message.Message) error {
		handlerCalled = true
		return nil
	}

	err := m.Handle(context.Background(), msg, handler)
	assert.Equal(t, ErrUnauthorized, err)
	assert.False(t, handlerCalled)
}

func TestAuthMiddleware_Handle_NoToken(t *testing.T) {
	m := NewAuthMiddleware(func(token string) bool { return true })

	msg := message.NewMessage("msg_001", "wechat", message.DirectionInbound, message.UserInfo{}, "test", message.MessageTypeText)

	handler := func(ctx context.Context, msg *message.Message) error {
		return nil
	}

	err := m.Handle(context.Background(), msg, handler)
	assert.Equal(t, ErrUnauthorized, err)
}

func TestAuthMiddleware_Handle_TokenNotString(t *testing.T) {
	m := NewAuthMiddleware(func(token string) bool { return true })

	msg := message.NewMessage("msg_001", "wechat", message.DirectionInbound, message.UserInfo{}, "test", message.MessageTypeText)
	msg.SetMetadata("token", 12345)

	handler := func(ctx context.Context, msg *message.Message) error {
		return nil
	}

	err := m.Handle(context.Background(), msg, handler)
	assert.Equal(t, ErrUnauthorized, err)
}

func TestChain_Integration(t *testing.T) {
	chain := NewChain()

	logger := &mockLogger{}
	chain.Use(NewLoggingMiddleware(logger))
	chain.Use(NewTraceMiddleware(func() string { return "trace_001" }))
	chain.Use(NewAuthMiddleware(func(token string) bool { return token == "secret" }))

	msg := message.NewMessage("msg_001", "wechat", message.DirectionInbound, message.UserInfo{}, "test", message.MessageTypeText)
	msg.SetMetadata("token", "secret")

	handlerCalled := false
	handler := func(ctx context.Context, msg *message.Message) error {
		handlerCalled = true
		return nil
	}

	err := chain.Execute(context.Background(), msg, handler)
	require.NoError(t, err)
	assert.True(t, handlerCalled)

	traceID, ok := msg.GetMetadata("trace_id")
	assert.True(t, ok)
	assert.Equal(t, "trace_001", traceID)
}

type testMiddleware struct {
	name           string
	called         bool
	shouldCallNext bool
	err            error
}

func (m *testMiddleware) Name() string {
	return m.name
}

func (m *testMiddleware) Handle(ctx context.Context, msg *message.Message, next Handler) error {
	m.called = true
	if m.err != nil {
		return m.err
	}
	if m.shouldCallNext {
		return next(ctx, msg)
	}
	return nil
}
