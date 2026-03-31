package middleware

import (
	"context"
	"testing"

	"github.com/DotNetAge/gort/pkg/message"
)

// mockMiddleware implements Middleware for benchmarking.
type mockMiddleware struct {
	name string
}

func (m *mockMiddleware) Name() string {
	return m.name
}

func (m *mockMiddleware) Handle(ctx context.Context, msg *message.Message, next Handler) error {
	return next(ctx, msg)
}

// benchLogger implements Logger for benchmarking.
type benchLogger struct{}

func (m *benchLogger) Info(msg string, fields ...interface{})  {}
func (m *benchLogger) Error(msg string, fields ...interface{}) {}

// BenchmarkChain_Creation benchmarks Chain creation.
func BenchmarkChain_Creation(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = NewChain()
	}
}

// BenchmarkChain_Use benchmarks adding middleware.
func BenchmarkChain_Use(b *testing.B) {
	chain := NewChain()
	m := &mockMiddleware{name: "test"}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		chain.Use(m)
	}
}

// BenchmarkChain_Execute_Empty benchmarks execution with no middleware.
func BenchmarkChain_Execute_Empty(b *testing.B) {
	chain := NewChain()
	ctx := context.Background()
	msg := message.NewMessage(
		"msg-1",
		"test",
		message.DirectionInbound,
		message.UserInfo{ID: "user-1"},
		"Hello",
		message.MessageTypeText,
	)
	handler := func(ctx context.Context, msg *message.Message) error {
		return nil
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = chain.Execute(ctx, msg, handler)
	}
}

// BenchmarkChain_Execute_Single benchmarks execution with one middleware.
func BenchmarkChain_Execute_Single(b *testing.B) {
	chain := NewChain()
	chain.Use(&mockMiddleware{name: "test"})

	ctx := context.Background()
	msg := message.NewMessage(
		"msg-1",
		"test",
		message.DirectionInbound,
		message.UserInfo{ID: "user-1"},
		"Hello",
		message.MessageTypeText,
	)
	handler := func(ctx context.Context, msg *message.Message) error {
		return nil
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = chain.Execute(ctx, msg, handler)
	}
}

// BenchmarkChain_Execute_Multiple benchmarks execution with multiple middleware.
func BenchmarkChain_Execute_Multiple(b *testing.B) {
	for _, count := range []int{1, 5, 10, 20} {
		b.Run(string(rune(count)), func(b *testing.B) {
			chain := NewChain()
			for i := 0; i < count; i++ {
				chain.Use(&mockMiddleware{name: "test"})
			}

			ctx := context.Background()
			msg := message.NewMessage(
				"msg-1",
				"test",
				message.DirectionInbound,
				message.UserInfo{ID: "user-1"},
				"Hello",
				message.MessageTypeText,
			)
			handler := func(ctx context.Context, msg *message.Message) error {
				return nil
			}

			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = chain.Execute(ctx, msg, handler)
			}
		})
	}
}

// BenchmarkLoggingMiddleware benchmarks logging middleware.
func BenchmarkLoggingMiddleware(b *testing.B) {
	logger := &benchLogger{}
	m := NewLoggingMiddleware(logger)

	ctx := context.Background()
	msg := message.NewMessage(
		"msg-1",
		"test",
		message.DirectionInbound,
		message.UserInfo{ID: "user-1"},
		"Hello",
		message.MessageTypeText,
	)
	next := func(ctx context.Context, msg *message.Message) error {
		return nil
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = m.Handle(ctx, msg, next)
	}
}

// BenchmarkMiddleware_Parallel benchmarks parallel middleware execution.
func BenchmarkMiddleware_Parallel(b *testing.B) {
	chain := NewChain()
	for i := 0; i < 5; i++ {
		chain.Use(&mockMiddleware{name: "test"})
	}

	handler := func(ctx context.Context, msg *message.Message) error {
		return nil
	}

	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		ctx := context.Background()
		msg := message.NewMessage(
			"msg-1",
			"test",
			message.DirectionInbound,
			message.UserInfo{ID: "user-1"},
			"Hello",
			message.MessageTypeText,
		)
		for pb.Next() {
			_ = chain.Execute(ctx, msg, handler)
		}
	})
}

// BenchmarkMiddleware_MessageThroughput benchmarks message throughput.
func BenchmarkMiddleware_MessageThroughput(b *testing.B) {
	chain := NewChain()
	chain.Use(&mockMiddleware{name: "auth"})
	chain.Use(&mockMiddleware{name: "logging"})
	chain.Use(&mockMiddleware{name: "metrics"})

	handler := func(ctx context.Context, msg *message.Message) error {
		return nil
	}

	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		msg := message.NewMessage(
			"msg-1",
			"test",
			message.DirectionInbound,
			message.UserInfo{ID: "user-1"},
			"Hello World",
			message.MessageTypeText,
		)
		_ = chain.Execute(ctx, msg, handler)
	}
}

// BenchmarkMiddleware_WithRealWorldScenario benchmarks realistic scenario.
func BenchmarkMiddleware_WithRealWorldScenario(b *testing.B) {
	chain := NewChain()
	logger := &benchLogger{}

	chain.Use(NewLoggingMiddleware(logger))
	chain.Use(&mockMiddleware{name: "auth"})
	chain.Use(&mockMiddleware{name: "rate-limit"})
	chain.Use(&mockMiddleware{name: "metrics"})

	handler := func(ctx context.Context, msg *message.Message) error {
		msg.SetMetadata("processed", true)
		return nil
	}

	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		msg := message.NewMessage(
			"msg-1",
			"test-channel",
			message.DirectionInbound,
			message.UserInfo{ID: "user-123", Name: "Test User", Platform: "test"},
			"This is a test message with some content that might be typical in real usage.",
			message.MessageTypeText,
		)
		msg.SetMetadata("source", "benchmark")
		_ = chain.Execute(ctx, msg, handler)
	}
}

// BenchmarkHandler benchmarks the handler function type.
func BenchmarkHandler(b *testing.B) {
	handler := func(ctx context.Context, msg *message.Message) error {
		return nil
	}

	ctx := context.Background()
	msg := message.NewMessage(
		"msg-1",
		"test",
		message.DirectionInbound,
		message.UserInfo{ID: "user-1"},
		"Hello",
		message.MessageTypeText,
	)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = handler(ctx, msg)
	}
}
