package message

import (
	"encoding/json"
	"sync"
	"testing"
)

// BenchmarkNewMessage benchmarks message creation.
func BenchmarkNewMessage(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = NewMessage(
			"msg-1",
			"test-channel",
			DirectionOutbound,
			UserInfo{ID: "user-1", Name: "Test User"},
			"Hello World",
			MessageTypeText,
		)
	}
}

// BenchmarkMessage_SetMetadata benchmarks setting metadata.
func BenchmarkMessage_SetMetadata(b *testing.B) {
	msg := NewMessage(
		"msg-1",
		"test-channel",
		DirectionOutbound,
		UserInfo{ID: "user-1"},
		"Test",
		MessageTypeText,
	)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		msg.SetMetadata("key", "value")
	}
}

// BenchmarkMessage_GetMetadata benchmarks getting metadata.
func BenchmarkMessage_GetMetadata(b *testing.B) {
	msg := NewMessage(
		"msg-1",
		"test-channel",
		DirectionOutbound,
		UserInfo{ID: "user-1"},
		"Test",
		MessageTypeText,
	)
	msg.SetMetadata("key", "value")
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = msg.GetMetadata("key")
	}
}

// BenchmarkMessage_JSONMarshal benchmarks JSON marshaling.
func BenchmarkMessage_JSONMarshal(b *testing.B) {
	msg := NewMessage(
		"msg-1",
		"test-channel",
		DirectionOutbound,
		UserInfo{ID: "user-1", Name: "Test User", Platform: "test"},
		"Hello World, this is a test message with some content.",
		MessageTypeText,
	)
	msg.SetMetadata("extra", "data")
	msg.SetMetadata("count", 123)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = json.Marshal(msg)
	}
}

// BenchmarkMessage_JSONUnmarshal benchmarks JSON unmarshaling.
func BenchmarkMessage_JSONUnmarshal(b *testing.B) {
	data := []byte(`{
		"id": "msg-1",
		"channel_id": "test-channel",
		"direction": "outbound",
		"from": {"id": "user-1", "name": "Test User", "platform": "test"},
		"to": {"id": "user-2", "name": "Recipient"},
		"content": "Hello World, this is a test message with some content.",
		"type": "text",
		"metadata": {"extra": "data", "count": 123}
	}`)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var msg Message
		_ = json.Unmarshal(data, &msg)
	}
}

// BenchmarkMessage_JSONRoundTrip benchmarks marshal + unmarshal.
func BenchmarkMessage_JSONRoundTrip(b *testing.B) {
	msg := NewMessage(
		"msg-1",
		"test-channel",
		DirectionOutbound,
		UserInfo{ID: "user-1", Name: "Test User", Platform: "test"},
		"Hello World",
		MessageTypeText,
	)
	msg.SetMetadata("key1", "value1")
	msg.SetMetadata("key2", 123)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		data, _ := json.Marshal(msg)
		var newMsg Message
		_ = json.Unmarshal(data, &newMsg)
	}
}

// BenchmarkMessage_LargeMetadata benchmarks with large metadata.
func BenchmarkMessage_LargeMetadata(b *testing.B) {
	msg := NewMessage(
		"msg-1",
		"test-channel",
		DirectionOutbound,
		UserInfo{ID: "user-1"},
		"Test",
		MessageTypeText,
	)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		for j := 0; j < 100; j++ {
			msg.SetMetadata("key", make([]string, 10))
		}
	}
}

// BenchmarkMessage_ParallelSetMetadata benchmarks parallel metadata access.
// Note: Message.Metadata is not thread-safe by design.
// This benchmark demonstrates the need for external synchronization.
func BenchmarkMessage_ParallelSetMetadata(b *testing.B) {
	b.Run("with-mutex", func(b *testing.B) {
		msg := NewMessage(
			"msg-1",
			"test-channel",
			DirectionOutbound,
			UserInfo{ID: "user-1"},
			"Test",
			MessageTypeText,
		)
		var mu sync.Mutex

		b.ResetTimer()
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				mu.Lock()
				msg.SetMetadata("key", i)
				mu.Unlock()
				i++
			}
		})
	})
}

// BenchmarkUserInfo_Creation benchmarks UserInfo creation.
func BenchmarkUserInfo_Creation(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = UserInfo{
			ID:       "user-123",
			Name:     "Test User",
			Avatar:   "https://example.com/avatar.png",
			Platform: "test",
		}
	}
}

// BenchmarkMessagePool benchmarks message allocation with sync.Pool pattern.
func BenchmarkMessagePool(b *testing.B) {
	b.Run("direct-alloc", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = &Message{
				ID:        "msg-1",
				ChannelID: "test",
				Direction: DirectionOutbound,
				Content:   "Test message",
				Type:      MessageTypeText,
				Metadata:  make(map[string]interface{}),
			}
		}
	})

	b.Run("newmessage", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = NewMessage("msg-1", "test", DirectionOutbound, UserInfo{}, "Test message", MessageTypeText)
		}
	})
}

// BenchmarkMessageTypes benchmarks different message types.
func BenchmarkMessageTypes(b *testing.B) {
	types := []MessageType{
		MessageTypeText,
		MessageTypeImage,
		MessageTypeFile,
		MessageTypeAudio,
		MessageTypeVideo,
		MessageTypeMarkdown,
	}

	for _, mt := range types {
		b.Run(string(mt), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = NewMessage(
					"msg-1",
					"test-channel",
					DirectionOutbound,
					UserInfo{ID: "user-1"},
					"content",
					mt,
				)
			}
		})
	}
}
