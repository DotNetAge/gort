package middleware

import (
	"context"
	"errors"

	"github.com/example/gort/pkg/message"
)

var (
	// ErrUnauthorized indicates that the request is unauthorized.
	ErrUnauthorized = errors.New("unauthorized")
	// ErrRateLimited indicates that the request has been rate limited.
	ErrRateLimited = errors.New("rate limited")
)

// Handler is the function type for message handling.
type Handler func(ctx context.Context, msg *message.Message) error

// Middleware is the interface for middleware implementations.
type Middleware interface {
	Name() string
	Handle(ctx context.Context, msg *message.Message, next Handler) error
}

// Chain represents a chain of middlewares.
type Chain struct {
	middlewares []Middleware
}

// NewChain creates a new middleware chain.
func NewChain() *Chain {
	return &Chain{
		middlewares: make([]Middleware, 0),
	}
}

// Use adds a middleware to the chain.
// If m is nil, it is silently ignored.
func (c *Chain) Use(m Middleware) {
	if m == nil {
		return
	}
	c.middlewares = append(c.middlewares, m)
}

// Execute executes the middleware chain with the given handler.
func (c *Chain) Execute(ctx context.Context, msg *message.Message, final Handler) error {
	if len(c.middlewares) == 0 {
		return final(ctx, msg)
	}

	handler := final
	for i := len(c.middlewares) - 1; i >= 0; i-- {
		m := c.middlewares[i]
		currentHandler := handler
		handler = func(ctx context.Context, msg *message.Message) error {
			return m.Handle(ctx, msg, currentHandler)
		}
	}

	return handler(ctx, msg)
}

// LoggingMiddleware logs message processing.
type LoggingMiddleware struct {
	logger Logger
}

// Logger is the interface for logging.
type Logger interface {
	Info(msg string, fields ...interface{})
	Error(msg string, fields ...interface{})
}

// NewLoggingMiddleware creates a new logging middleware.
func NewLoggingMiddleware(logger Logger) *LoggingMiddleware {
	return &LoggingMiddleware{logger: logger}
}

// Name returns the middleware name.
func (m *LoggingMiddleware) Name() string {
	return "Logging"
}

// Handle logs the message processing.
func (m *LoggingMiddleware) Handle(ctx context.Context, msg *message.Message, next Handler) error {
	m.logger.Info("processing message", "id", msg.ID, "channel", msg.ChannelID, "direction", msg.Direction)

	err := next(ctx, msg)

	if err != nil {
		m.logger.Error("message processing failed", "id", msg.ID, "error", err)
	} else {
		m.logger.Info("message processed", "id", msg.ID)
	}

	return err
}

// TraceMiddleware adds trace ID to messages.
type TraceMiddleware struct {
	generateID func() string
}

// NewTraceMiddleware creates a new trace middleware.
func NewTraceMiddleware(generateID func() string) *TraceMiddleware {
	return &TraceMiddleware{generateID: generateID}
}

// Name returns the middleware name.
func (m *TraceMiddleware) Name() string {
	return "Trace"
}

// Handle adds trace ID to the message.
func (m *TraceMiddleware) Handle(ctx context.Context, msg *message.Message, next Handler) error {
	if _, ok := msg.GetMetadata("trace_id"); !ok {
		traceID := m.generateID()
		msg.SetMetadata("trace_id", traceID)
	}

	return next(ctx, msg)
}

// AuthMiddleware validates authentication tokens.
type AuthMiddleware struct {
	validateToken func(token string) bool
}

// NewAuthMiddleware creates a new auth middleware.
func NewAuthMiddleware(validateToken func(token string) bool) *AuthMiddleware {
	return &AuthMiddleware{validateToken: validateToken}
}

// Name returns the middleware name.
func (m *AuthMiddleware) Name() string {
	return "Auth"
}

// Handle validates the token in the message metadata.
func (m *AuthMiddleware) Handle(ctx context.Context, msg *message.Message, next Handler) error {
	token, ok := msg.GetMetadata("token")
	if !ok {
		return ErrUnauthorized
	}

	tokenStr, ok := token.(string)
	if !ok {
		return ErrUnauthorized
	}

	if !m.validateToken(tokenStr) {
		return ErrUnauthorized
	}

	return next(ctx, msg)
}
