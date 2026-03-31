package message

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMessage(t *testing.T) {
	tests := []struct {
		name      string
		id        string
		channelID string
		direction Direction
		from      UserInfo
		content   string
		msgType   MessageType
	}{
		{
			name:      "create inbound text message",
			id:        "msg_001",
			channelID: "wechat",
			direction: DirectionInbound,
			from: UserInfo{
				ID:       "user_001",
				Name:     "Test User",
				Platform: "wechat",
			},
			content: "Hello",
			msgType: MessageTypeText,
		},
		{
			name:      "create outbound text message",
			id:        "msg_002",
			channelID: "dingtalk",
			direction: DirectionOutbound,
			from: UserInfo{
				ID:       "system",
				Name:     "AI Assistant",
				Platform: "gort",
			},
			content: "Hi there",
			msgType: MessageTypeText,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := NewMessage(tt.id, tt.channelID, tt.direction, tt.from, tt.content, tt.msgType)

			assert.Equal(t, tt.id, msg.ID)
			assert.Equal(t, tt.channelID, msg.ChannelID)
			assert.Equal(t, tt.direction, msg.Direction)
			assert.Equal(t, tt.from, msg.From)
			assert.Equal(t, tt.content, msg.Content)
			assert.Equal(t, tt.msgType, msg.Type)
			assert.NotNil(t, msg.Metadata)
			assert.NotZero(t, msg.Timestamp)
			assert.True(t, msg.Timestamp.Location() == time.UTC)
		})
	}
}

func TestMessage_SetMetadata(t *testing.T) {
	msg := NewMessage("msg_001", "wechat", DirectionInbound, UserInfo{}, "test", MessageTypeText)

	msg.SetMetadata("key1", "value1")
	msg.SetMetadata("key2", 123)

	assert.Equal(t, "value1", msg.Metadata["key1"])
	assert.Equal(t, 123, msg.Metadata["key2"])
}

func TestMessage_SetMetadata_NilMap(t *testing.T) {
	msg := &Message{}
	assert.Nil(t, msg.Metadata)

	msg.SetMetadata("key", "value")

	assert.NotNil(t, msg.Metadata)
	assert.Equal(t, "value", msg.Metadata["key"])
}

func TestMessage_GetMetadata(t *testing.T) {
	msg := NewMessage("msg_001", "wechat", DirectionInbound, UserInfo{}, "test", MessageTypeText)
	msg.SetMetadata("key1", "value1")

	val, ok := msg.GetMetadata("key1")
	assert.True(t, ok)
	assert.Equal(t, "value1", val)

	val, ok = msg.GetMetadata("nonexistent")
	assert.False(t, ok)
	assert.Nil(t, val)
}

func TestMessage_GetMetadata_NilMap(t *testing.T) {
	msg := &Message{}

	val, ok := msg.GetMetadata("key")
	assert.False(t, ok)
	assert.Nil(t, val)
}

func TestMessage_IsInbound(t *testing.T) {
	inboundMsg := NewMessage("msg_001", "wechat", DirectionInbound, UserInfo{}, "test", MessageTypeText)
	assert.True(t, inboundMsg.IsInbound())
	assert.False(t, inboundMsg.IsOutbound())

	outboundMsg := NewMessage("msg_002", "wechat", DirectionOutbound, UserInfo{}, "test", MessageTypeText)
	assert.False(t, outboundMsg.IsInbound())
	assert.True(t, outboundMsg.IsOutbound())
}

func TestMessage_IsOutbound(t *testing.T) {
	outboundMsg := NewMessage("msg_001", "wechat", DirectionOutbound, UserInfo{}, "test", MessageTypeText)
	assert.True(t, outboundMsg.IsOutbound())
	assert.False(t, outboundMsg.IsInbound())
}

func TestMessage_Validate(t *testing.T) {
	tests := []struct {
		name    string
		msg     *Message
		wantErr error
	}{
		{
			name: "valid message",
			msg: &Message{
				ID:        "msg_001",
				ChannelID: "wechat",
				Content:   "Hello",
				Type:      MessageTypeText,
			},
			wantErr: nil,
		},
		{
			name: "empty ID",
			msg: &Message{
				ChannelID: "wechat",
				Content:   "Hello",
				Type:      MessageTypeText,
			},
			wantErr: ErrEmptyID,
		},
		{
			name: "empty channel ID",
			msg: &Message{
				ID:      "msg_001",
				Content: "Hello",
				Type:    MessageTypeText,
			},
			wantErr: ErrEmptyChannelID,
		},
		{
			name: "empty content for text message",
			msg: &Message{
				ID:        "msg_001",
				ChannelID: "wechat",
				Content:   "",
				Type:      MessageTypeText,
			},
			wantErr: ErrEmptyContent,
		},
		{
			name: "empty content for event message is allowed",
			msg: &Message{
				ID:        "msg_001",
				ChannelID: "wechat",
				Content:   "",
				Type:      MessageTypeEvent,
			},
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.msg.Validate()
			assert.Equal(t, tt.wantErr, err)
		})
	}
}

func TestMessage_JSONSerialization(t *testing.T) {
	original := &Message{
		ID:        "msg_001",
		ChannelID: "wechat",
		Direction: DirectionInbound,
		From: UserInfo{
			ID:       "user_001",
			Name:     "Test User",
			Avatar:   "https://example.com/avatar.png",
			Platform: "wechat",
		},
		To: UserInfo{
			ID:       "bot_001",
			Name:     "AI Bot",
			Platform: "gort",
		},
		Content:   "Hello, World!",
		Type:      MessageTypeText,
		Metadata:  map[string]interface{}{"msg_id": "12345", "create_time": 1234567890},
		Timestamp: time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded Message
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, original.ID, decoded.ID)
	assert.Equal(t, original.ChannelID, decoded.ChannelID)
	assert.Equal(t, original.Direction, decoded.Direction)
	assert.Equal(t, original.From, decoded.From)
	assert.Equal(t, original.To, decoded.To)
	assert.Equal(t, original.Content, decoded.Content)
	assert.Equal(t, original.Type, decoded.Type)
	assert.Equal(t, original.Metadata["msg_id"], decoded.Metadata["msg_id"])
	assert.Equal(t, original.Timestamp.UTC(), decoded.Timestamp.UTC())
}

func TestUserInfo_JSONSerialization(t *testing.T) {
	original := UserInfo{
		ID:       "user_001",
		Name:     "Test User",
		Avatar:   "https://example.com/avatar.png",
		Platform: "wechat",
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded UserInfo
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, original, decoded)
}

func TestDirection_Constants(t *testing.T) {
	assert.Equal(t, Direction("inbound"), DirectionInbound)
	assert.Equal(t, Direction("outbound"), DirectionOutbound)
}

func TestMessageType_Constants(t *testing.T) {
	assert.Equal(t, MessageType("text"), MessageTypeText)
	assert.Equal(t, MessageType("image"), MessageTypeImage)
	assert.Equal(t, MessageType("file"), MessageTypeFile)
	assert.Equal(t, MessageType("audio"), MessageTypeAudio)
	assert.Equal(t, MessageType("video"), MessageTypeVideo)
	assert.Equal(t, MessageType("event"), MessageTypeEvent)
}
