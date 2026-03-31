package gateway

import (
	"context"
	"testing"
	"time"

	"github.com/example/gort/pkg/channel"
	"github.com/example/gort/pkg/message"
	"github.com/example/gort/pkg/session"
)

// mockChannel implements channel.Channel for benchmarking.
type mockChannel struct {
	name    string
	running bool
}

func (m *mockChannel) Name() string                       { return m.name }
func (m *mockChannel) Type() channel.ChannelType          { return "mock" }
func (m *mockChannel) Start(ctx context.Context, handler channel.MessageHandler) error {
	m.running = true
	return nil
}
func (m *mockChannel) Stop(ctx context.Context) error {
	m.running = false
	return nil
}
func (m *mockChannel) IsRunning() bool                    { return m.running }
func (m *mockChannel) SendMessage(ctx context.Context, msg *message.Message) error {
	return nil
}
func (m *mockChannel) GetStatus() channel.Status {
	if m.running {
		return channel.StatusRunning
	}
	return channel.StatusStopped
}

func newTestSessionManager() *session.Manager {
	return session.NewManager(session.Config{
		HeartbeatInterval: 30 * time.Second,
		HeartbeatTimeout:  60 * time.Second,
	})
}

// BenchmarkGateway_Creation benchmarks Gateway creation.
func BenchmarkGateway_Creation(b *testing.B) {
	sm := newTestSessionManager()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = New(sm)
	}
}

// BenchmarkGateway_RegisterChannel benchmarks channel registration.
func BenchmarkGateway_RegisterChannel(b *testing.B) {
	sm := newTestSessionManager()
	gw := New(sm)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		ch := &mockChannel{name: "test"}
		_ = gw.RegisterChannel(ch)
	}
}

// BenchmarkGateway_GetChannel benchmarks channel retrieval.
func BenchmarkGateway_GetChannel(b *testing.B) {
	sm := newTestSessionManager()
	gw := New(sm)
	gw.RegisterChannel(&mockChannel{name: "test"})

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = gw.GetChannel("test")
	}
}

// BenchmarkGateway_RegisterHandler benchmarks handler registration.
func BenchmarkGateway_RegisterHandler(b *testing.B) {
	sm := newTestSessionManager()
	gw := New(sm)
	handler := func(ctx context.Context, clientID string, msg *message.Message) error {
		return nil
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		gw.RegisterClientHandler(handler)
	}
}

// BenchmarkGateway_MessageRouting benchmarks message routing.
func BenchmarkGateway_MessageRouting(b *testing.B) {
	sm := newTestSessionManager()
	gw := New(sm)
	gw.RegisterChannel(&mockChannel{name: "test"})

	msg := message.NewMessage(
		"msg-1",
		"test",
		message.DirectionOutbound,
		message.UserInfo{ID: "user-1"},
		"Hello World",
		message.MessageTypeText,
	)

	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		ch, ok := gw.GetChannel("test")
		if ok {
			_ = ch.SendMessage(ctx, msg)
		}
	}
}

// BenchmarkGateway_ParallelGetChannel benchmarks parallel channel access.
func BenchmarkGateway_ParallelGetChannel(b *testing.B) {
	sm := newTestSessionManager()
	gw := New(sm)
	for i := 0; i < 10; i++ {
		gw.RegisterChannel(&mockChannel{name: string(rune('a' + i))})
	}

	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			_, _ = gw.GetChannel(string(rune('a' + i%10)))
			i++
		}
	})
}

// BenchmarkGateway_MultiChannel benchmarks with multiple channels.
func BenchmarkGateway_MultiChannel(b *testing.B) {
	sm := newTestSessionManager()
	gw := New(sm)

	for i := 0; i < 100; i++ {
		gw.RegisterChannel(&mockChannel{name: string(rune(i))})
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = gw.GetChannel(string(rune(i % 100)))
	}
}

// BenchmarkGateway_ContextHandling benchmarks context handling.
func BenchmarkGateway_ContextHandling(b *testing.B) {
	_ = newTestSessionManager()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		_ = ctx
		cancel()
	}
}
