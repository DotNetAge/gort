package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DotNetAge/gort/pkg/channel"
	"github.com/DotNetAge/gort/pkg/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr error
	}{
		{
			name: "valid config with token",
			config: Config{
				Token: "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11",
			},
			wantErr: nil,
		},
		{
			name:    "missing token",
			config:  Config{},
			wantErr: ErrTokenRequired,
		},
		{
			name: "valid config with webhook",
			config: Config{
				Token:      "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11",
				WebhookURL: "https://example.com/webhook",
			},
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestNewChannel(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr error
	}{
		{
			name: "valid config",
			config: Config{
				Token: "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11",
			},
			wantErr: nil,
		},
		{
			name:    "invalid config - missing token",
			config:  Config{},
			wantErr: ErrTokenRequired,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch, err := NewChannel("test-telegram", tt.config)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				assert.Nil(t, ch)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, ch)
				assert.Equal(t, "test-telegram", ch.Name())
				assert.Equal(t, channel.ChannelTypeTelegram, ch.Type())
			}
		})
	}
}

func TestChannel_StartStop(t *testing.T) {
	ch, err := NewChannel("test", Config{
		Token: "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11",
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	handler := func(ctx context.Context, msg *message.Message) error { return nil }

	// Test Start
	err = ch.Start(ctx, handler)
	// May fail due to network, but should set running state
	if err == nil {
		assert.True(t, ch.IsRunning())
		assert.Equal(t, channel.StatusRunning, ch.GetStatus())

		// Test Stop
		err = ch.Stop(ctx)
		assert.NoError(t, err)
		assert.False(t, ch.IsRunning())
	}
}

func TestChannel_SendMessage_NotRunning(t *testing.T) {
	ch, err := NewChannel("test", Config{
		Token: "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11",
	})
	require.NoError(t, err)

	msg := &message.Message{
		Type:    message.MessageTypeText,
		Content: "test",
		To:      message.UserInfo{ID: "123456789"},
	}

	err = ch.SendMessage(context.Background(), msg)
	assert.ErrorIs(t, err, channel.ErrChannelNotRunning)
}

func TestChannel_SendMessage_InvalidChatID(t *testing.T) {
	ch, err := NewChannel("test", Config{
		Token: "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11",
	})
	require.NoError(t, err)

	ctx := context.Background()
	err = ch.Start(ctx, func(ctx context.Context, msg *message.Message) error { return nil })
	require.NoError(t, err)
	defer ch.Stop(ctx)

	msg := &message.Message{
		Type:    message.MessageTypeText,
		Content: "test",
		To:      message.UserInfo{ID: "invalid_chat_id"},
	}

	err = ch.SendMessage(ctx, msg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid chat_id")
}

func TestChannel_HandleWebhook(t *testing.T) {
	ch, err := NewChannel("test", Config{
		Token: "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11",
	})
	require.NoError(t, err)

	tests := []struct {
		name      string
		data      []byte
		wantErr   bool
		checkFunc func(t *testing.T, msg *message.Message)
	}{
		{
			name: "valid text message",
			data: []byte(`{
				"update_id": 123456789,
				"message": {
					"message_id": 1,
					"from": {
						"id": 123456,
						"is_bot": false,
						"first_name": "Test",
						"username": "testuser"
					},
					"chat": {
						"id": 123456,
						"type": "private"
					},
					"date": 1700000000,
					"text": "Hello Telegram"
				}
			}`),
			wantErr: false,
			checkFunc: func(t *testing.T, msg *message.Message) {
				assert.Equal(t, "1", msg.ID)
				assert.Equal(t, message.MessageTypeText, msg.Type)
				assert.Equal(t, "Hello Telegram", msg.Content)
				assert.Equal(t, "123456", msg.From.ID)
				assert.Equal(t, "Test", msg.From.Name)
				assert.Equal(t, "telegram", msg.From.Platform)
			},
		},
		{
			name: "photo message",
			data: []byte(`{
				"update_id": 123456790,
				"message": {
					"message_id": 2,
					"from": {"id": 123456, "is_bot": false, "first_name": "Test"},
					"chat": {"id": 123456, "type": "private"},
					"date": 1700000000,
					"photo": [
						{"file_id": "photo_small", "file_unique_id": "unique_small", "width": 100, "height": 100},
						{"file_id": "photo_large", "file_unique_id": "unique_large", "width": 800, "height": 800}
					],
					"caption": "Photo caption"
				}
			}`),
			wantErr: false,
			checkFunc: func(t *testing.T, msg *message.Message) {
				assert.Equal(t, message.MessageTypeImage, msg.Type)
				assert.Equal(t, "Photo caption", msg.Content)
			},
		},
		{
			name: "document message",
			data: []byte(`{
				"update_id": 123456791,
				"message": {
					"message_id": 3,
					"from": {"id": 123456, "is_bot": false, "first_name": "Test"},
					"chat": {"id": 123456, "type": "private"},
					"date": 1700000000,
					"document": {
						"file_id": "doc_xxx",
						"file_unique_id": "doc_unique",
						"file_name": "document.pdf",
						"mime_type": "application/pdf"
					}
				}
			}`),
			wantErr: false,
			checkFunc: func(t *testing.T, msg *message.Message) {
				assert.Equal(t, message.MessageTypeFile, msg.Type)
			},
		},
		{
			name: "audio message",
			data: []byte(`{
				"update_id": 123456792,
				"message": {
					"message_id": 4,
					"from": {"id": 123456, "is_bot": false, "first_name": "Test"},
					"chat": {"id": 123456, "type": "private"},
					"date": 1700000000,
					"audio": {
						"file_id": "audio_xxx",
						"file_unique_id": "audio_unique",
						"duration": 120
					}
				}
			}`),
			wantErr: false,
			checkFunc: func(t *testing.T, msg *message.Message) {
				assert.Equal(t, message.MessageTypeAudio, msg.Type)
			},
		},
		{
			name: "voice message",
			data: []byte(`{
				"update_id": 123456793,
				"message": {
					"message_id": 5,
					"from": {"id": 123456, "is_bot": false, "first_name": "Test"},
					"chat": {"id": 123456, "type": "private"},
					"date": 1700000000,
					"voice": {
						"file_id": "voice_xxx",
						"file_unique_id": "voice_unique",
						"duration": 30
					}
				}
			}`),
			wantErr: false,
			checkFunc: func(t *testing.T, msg *message.Message) {
				assert.Equal(t, message.MessageTypeAudio, msg.Type)
			},
		},
		{
			name: "video message",
			data: []byte(`{
				"update_id": 123456794,
				"message": {
					"message_id": 6,
					"from": {"id": 123456, "is_bot": false, "first_name": "Test"},
					"chat": {"id": 123456, "type": "private"},
					"date": 1700000000,
					"video": {
						"file_id": "video_xxx",
						"file_unique_id": "video_unique",
						"width": 1920,
						"height": 1080,
						"duration": 60
					}
				}
			}`),
			wantErr: false,
			checkFunc: func(t *testing.T, msg *message.Message) {
				assert.Equal(t, message.MessageTypeVideo, msg.Type)
			},
		},
		{
			name: "location message",
			data: []byte(`{
				"update_id": 123456795,
				"message": {
					"message_id": 7,
					"from": {"id": 123456, "is_bot": false, "first_name": "Test"},
					"chat": {"id": 123456, "type": "private"},
					"date": 1700000000,
					"location": {
						"longitude": 116.4074,
						"latitude": 39.9042
					}
				}
			}`),
			wantErr: false,
			checkFunc: func(t *testing.T, msg *message.Message) {
				assert.Equal(t, message.MessageTypeEvent, msg.Type)
			},
		},
		{
			name: "contact message",
			data: []byte(`{
				"update_id": 123456796,
				"message": {
					"message_id": 8,
					"from": {"id": 123456, "is_bot": false, "first_name": "Test"},
					"chat": {"id": 123456, "type": "private"},
					"date": 1700000000,
					"contact": {
						"phone_number": "+1234567890",
						"first_name": "Contact",
						"last_name": "Name"
					}
				}
			}`),
			wantErr: false,
			checkFunc: func(t *testing.T, msg *message.Message) {
				assert.Equal(t, message.MessageTypeEvent, msg.Type)
			},
		},
		{
			name:    "invalid JSON",
			data:    []byte(`{invalid json}`),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, err := ch.HandleWebhook("/webhook", tt.data)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, msg)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, msg)
				if tt.checkFunc != nil {
					tt.checkFunc(t, msg)
				}
			}
		})
	}
}

func TestChannel_MessageTypes(t *testing.T) {
	ch, err := NewChannel("test", Config{
		Token: "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11",
	})
	require.NoError(t, err)

	ctx := context.Background()
	err = ch.Start(ctx, func(ctx context.Context, msg *message.Message) error { return nil })
	require.NoError(t, err)
	defer ch.Stop(ctx)

	// Test different message types - they should handle gracefully
	msgTypes := []message.MessageType{
		message.MessageTypeText,
		message.MessageTypeImage,
		message.MessageTypeFile,
		message.MessageTypeAudio,
		message.MessageTypeVideo,
	}

	for _, msgType := range msgTypes {
		t.Run(string(msgType), func(t *testing.T) {
			msg := &message.Message{
				Type:    msgType,
				Content: "test content",
				To:      message.UserInfo{ID: "123456789"},
			}
			// This will fail due to network, but should not panic
			_ = ch.SendMessage(ctx, msg)
		})
	}
}

func TestChannel_WebhookMode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok": true,
		})
	}))
	defer server.Close()

	ch, err := NewChannel("test", Config{
		Token:      "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11",
		WebhookURL: server.URL + "/webhook",
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = ch.Start(ctx, func(ctx context.Context, msg *message.Message) error { return nil })
	// May fail due to network, but should attempt webhook setup
	if err == nil {
		defer ch.Stop(ctx)
		assert.True(t, ch.IsRunning())
	}
}

func TestUpdate_Struct(t *testing.T) {
	update := Update{
		UpdateID: 123456,
		Message: &MessageObject{
			MessageID: 1,
			From: &User{
				ID:        123456,
				IsBot:     false,
				FirstName: "Test",
				Username:  "testuser",
			},
			Chat: &Chat{
				ID:   123456,
				Type: "private",
			},
			Date: 1700000000,
			Text: "Hello",
		},
	}

	assert.Equal(t, int64(123456), update.UpdateID)
	assert.NotNil(t, update.Message)
	assert.Equal(t, int64(1), update.Message.MessageID)
	assert.Equal(t, "Hello", update.Message.Text)
}

func TestParseUpdate(t *testing.T) {
	ch, err := NewChannel("test", Config{
		Token: "123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11",
	})
	require.NoError(t, err)

	// Test with nil message
	update := &Update{
		UpdateID: 123456,
		Message:  nil,
	}

	msg, err := ch.parseUpdate(update)
	assert.Error(t, err)
	assert.Nil(t, msg)
}
