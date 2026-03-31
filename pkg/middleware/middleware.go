// Package middleware provides message processing middleware for the gort system.
// It implements a chain-of-responsibility pattern for message handling, allowing
// cross-cutting concerns like logging, authentication, and tracing to be applied
// uniformly across all messages.
//
// Middleware Architecture:
//
//	Request ──▶ Middleware 1 ──▶ Middleware 2 ──▶ Middleware 3 ──▶ Handler
//	              │               │               │
//	              ▼               ▼               ▼
//	           Logging       Authentication    Tracing
//
// Each middleware can:
//   - Process the message before passing to the next handler
//   - Short-circuit the chain by returning an error
//   - Process the result after the next handler completes
//
// Basic Usage:
//
//	// Create middleware chain
//	chain := middleware.NewChain()
//
//	// Add middleware
//	chain.Use(middleware.NewLoggingMiddleware(logger))
//	chain.Use(middleware.NewAuthMiddleware(tokenValidator))
//	chain.Use(middleware.NewTraceMiddleware(generateID))
//
//	// Execute with final handler
//	err := chain.Execute(ctx, msg, func(ctx context.Context, msg *message.Message) error {
//	    // Process message...
//	    return nil
//	})
//
// Creating Custom Middleware:
//
//	type MyMiddleware struct{}
//
//	func (m *MyMiddleware) Name() string {
//	    return "MyMiddleware"
//	}
//
//	func (m *MyMiddleware) Handle(ctx context.Context, msg *message.Message, next Handler) error {
//	    // Pre-processing
//	    log.Println("Before handler")
//
//	    // Call next handler
//	    err := next(ctx, msg)
//
//	    // Post-processing
//	    log.Println("After handler")
//
//	    return err
//	}
//
// Thread Safety:
//
// Middleware implementations should be safe for concurrent use.
// The Chain type is safe for concurrent use.
package middleware

import (
	"context"
	"errors"

	"github.com/DotNetAge/gort/pkg/message"
)

// Error definitions for middleware operations.
var (
	// ErrUnauthorized is returned when authentication fails or no valid token is provided.
	ErrUnauthorized = errors.New("unauthorized")

	// ErrRateLimited is returned when a request exceeds rate limits.
	ErrRateLimited = errors.New("rate limited")
)

// Handler is the function type for message handling.
// It represents the final handler in the middleware chain.
//
// Parameters:
//   - ctx: Context for cancellation and values
//   - msg: The message to process
//
// Returns an error if message processing fails.
type Handler func(ctx context.Context, msg *message.Message) error

// Middleware is the interface for middleware implementations.
// Middleware wraps handlers to add cross-cutting functionality.
//
// Implementations must be safe for concurrent use.
type Middleware interface {
	// Name returns the unique name of this middleware.
	// This is used for logging and debugging purposes.
	Name() string

	// Handle processes the message and calls the next handler.
	//
	// Parameters:
	//   - ctx: Context for cancellation and values
	//   - msg: The message to process
	//   - next: The next handler in the chain
	//
	// Returns an error to stop processing, or nil to continue.
	Handle(ctx context.Context, msg *message.Message, next Handler) error
}

// Chain represents a chain of middlewares.
// It executes middleware in order, with each middleware deciding whether
// to continue to the next handler or return an error.
//
// This type is safe for concurrent use.
type Chain struct {
	middlewares []Middleware
}

// NewChain creates a new middleware chain.
//
// Returns an empty chain ready for middleware to be added.
//
// Example:
//
//	chain := middleware.NewChain()
//	chain.Use(middleware.NewLoggingMiddleware(logger))
func NewChain() *Chain {
	return &Chain{
		middlewares: make([]Middleware, 0),
	}
}

// Use adds a middleware to the chain.
// Middleware are executed in the order they are added.
// If m is nil, it is silently ignored.
//
// Parameters:
//   - m: The middleware to add
//
// Example:
//
//	chain.Use(middleware.NewLoggingMiddleware(logger))
//	chain.Use(middleware.NewAuthMiddleware(validator))
func (c *Chain) Use(m Middleware) {
	if m == nil {
		return
	}
	c.middlewares = append(c.middlewares, m)
}

// Execute executes the middleware chain with the given handler.
// The final handler is called after all middleware have processed the message.
//
// Parameters:
//   - ctx: Context for cancellation and values
//   - msg: The message to process
//   - final: The final handler to call after all middleware
//
// Returns an error if any middleware or the final handler fails.
//
// Example:
//
//	err := chain.Execute(ctx, msg, func(ctx context.Context, msg *message.Message) error {
//	    // Final message processing
//	    return processMessage(msg)
//	})
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
// It logs when a message starts processing and when it completes (success or failure).
type LoggingMiddleware struct {
	logger Logger
}

// Logger is the interface for logging.
// Implementations can use any logging library (logrus, zap, etc.).
type Logger interface {
	// Info logs an informational message.
	Info(msg string, fields ...interface{})

	// Error logs an error message.
	Error(msg string, fields ...interface{})
}

// NewLoggingMiddleware creates a new logging middleware.
//
// Parameters:
//   - logger: The logger to use for output
//
// Example:
//
//	logger := log.New(os.Stdout, "[GORT] ", log.LstdFlags)
//	middleware := middleware.NewLoggingMiddleware(&MyLogger{logger})
func NewLoggingMiddleware(logger Logger) *LoggingMiddleware {
	return &LoggingMiddleware{logger: logger}
}

// Name returns the middleware name.
func (m *LoggingMiddleware) Name() string {
	return "Logging"
}

// Handle logs the message processing.
// Logs at INFO level when processing starts and completes,
// and at ERROR level if processing fails.
//
// Parameters:
//   - ctx: Context for cancellation
//   - msg: The message being processed
//   - next: The next handler in the chain
//
// Returns any error from the next handler.
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

// TraceMiddleware adds trace ID to messages for distributed tracing.
// If a message doesn't have a trace_id metadata, it generates one.
type TraceMiddleware struct {
	generateID func() string
}

// NewTraceMiddleware creates a new trace middleware.
//
// Parameters:
//   - generateID: Function to generate unique trace IDs
//
// Returns a middleware that adds trace IDs to messages.
//
// Example:
//
//	middleware := middleware.NewTraceMiddleware(func() string {
//	    return uuid.New().String()
//	})
func NewTraceMiddleware(generateID func() string) *TraceMiddleware {
	return &TraceMiddleware{generateID: generateID}
}

// Name returns the middleware name.
func (m *TraceMiddleware) Name() string {
	return "Trace"
}

// Handle adds trace ID to the message if not present.
// The trace_id is stored in message metadata.
//
// Parameters:
//   - ctx: Context for cancellation
//   - msg: The message to process
//   - next: The next handler in the chain
//
// Returns any error from the next handler.
func (m *TraceMiddleware) Handle(ctx context.Context, msg *message.Message, next Handler) error {
	if _, ok := msg.GetMetadata("trace_id"); !ok {
		traceID := m.generateID()
		msg.SetMetadata("trace_id", traceID)
	}

	return next(ctx, msg)
}

// AuthMiddleware validates authentication tokens in messages.
// It checks for a "token" metadata field and validates it.
type AuthMiddleware struct {
	validateToken func(token string) bool
}

// NewAuthMiddleware creates a new auth middleware.
//
// Parameters:
//   - validateToken: Function to validate authentication tokens
//
// Returns a middleware that validates tokens.
//
// Example:
//
//	middleware := middleware.NewAuthMiddleware(func(token string) bool {
//	    return token == "valid-token"
//	})
func NewAuthMiddleware(validateToken func(token string) bool) *AuthMiddleware {
	return &AuthMiddleware{validateToken: validateToken}
}

// Name returns the middleware name.
func (m *AuthMiddleware) Name() string {
	return "Auth"
}

// Handle validates the token in the message metadata.
// Looks for "token" in message metadata and validates it.
// Returns ErrUnauthorized if token is missing or invalid.
//
// Parameters:
//   - ctx: Context for cancellation
//   - msg: The message to validate
//   - next: The next handler in the chain
//
// Returns ErrUnauthorized if authentication fails, or any error from the next handler.
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
