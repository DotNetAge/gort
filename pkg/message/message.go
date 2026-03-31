// Package message provides the standard message format used throughout the gort system.
// It defines the core data structures for message passing between different components.
package message

import (
	"encoding/json"
	"time"
)

// Direction represents the direction of a message in the system.
// It indicates whether a message is coming from an external platform (inbound)
// or going to an external platform (outbound).
type Direction string

const (
	// DirectionInbound indicates a message from external platform to system.
	// These messages originate from channels like WeChat, DingTalk, or Feishu.
	DirectionInbound Direction = "inbound"
	// DirectionOutbound indicates a message from system to external platform.
	// These messages are sent from clients to external channels.
	DirectionOutbound Direction = "outbound"
)

// MessageType represents the type of a message content.
// It determines how the message content should be interpreted and processed.
type MessageType string

const (
	// MessageTypeText represents a plain text message.
	MessageTypeText MessageType = "text"
	// MessageTypeImage represents an image message with a URL to the image.
	MessageTypeImage MessageType = "image"
	// MessageTypeFile represents a file attachment message.
	MessageTypeFile MessageType = "file"
	// MessageTypeAudio represents an audio/voice message.
	MessageTypeAudio MessageType = "audio"
	// MessageTypeVideo represents a video message.
	MessageTypeVideo MessageType = "video"
	// MessageTypeVoice represents a voice message.
	MessageTypeVoice MessageType = "voice"
	// MessageTypeMarkdown represents a markdown formatted message.
	MessageTypeMarkdown MessageType = "markdown"
	// MessageTypeNews represents a news/article message.
	MessageTypeNews MessageType = "news"
	// MessageTypeTemplateCard represents a template card message.
	MessageTypeTemplateCard MessageType = "template_card"
	// MessageTypeEvent represents a system event message (e.g., user join/leave).
	MessageTypeEvent MessageType = "event"
)

// UserInfo represents information about a user in the system.
// It contains identifying information and display details.
type UserInfo struct {
	// ID is the platform-specific user identifier.
	ID string `json:"id"`
	// Name is the display name of the user.
	Name string `json:"name"`
	// Avatar is the URL to the user's avatar image.
	Avatar string `json:"avatar"`
	// Platform indicates which platform the user belongs to (e.g., "wechat", "dingtalk").
	Platform string `json:"platform"`
}

// Message represents the standard message format used throughout the system.
// It provides a unified structure for messages from different platforms,
// enabling consistent processing across all components.
type Message struct {
	// ID is the unique identifier for this message.
	// It is used for message tracking, deduplication, and logging.
	ID string `json:"id"`
	// ChannelID identifies the source or destination channel.
	// For inbound messages, this is the source channel.
	// For outbound messages, this is the destination channel.
	ChannelID string `json:"channel_id"`
	// Direction indicates whether the message is inbound or outbound.
	Direction Direction `json:"direction"`
	// From contains information about the message sender.
	From UserInfo `json:"from"`
	// To contains information about the message recipient.
	// This is primarily used for group chat scenarios.
	To UserInfo `json:"to"`
	// Content is the actual message content.
	// For text messages, this is the text itself.
	// For media messages, this is typically a URL.
	Content string `json:"content"`
	// Type indicates the type of message content.
	Type MessageType `json:"type"`
	// Metadata contains platform-specific and custom data.
	// Use this for fields that don't fit into the standard structure.
	Metadata map[string]interface{} `json:"metadata"`
	// Timestamp is when the message was created or received.
	Timestamp time.Time `json:"timestamp"`
}

// NewMessage creates a new Message with the given parameters.
// It initializes the Metadata map and sets the Timestamp to the current UTC time.
//
// Example:
//
//	msg := message.NewMessage(
//	    "msg_001",
//	    "wechat",
//	    message.DirectionInbound,
//	    message.UserInfo{ID: "user_001", Name: "Alice"},
//	    "Hello, World!",
//	    message.MessageTypeText,
//	)
func NewMessage(id, channelID string, direction Direction, from UserInfo, content string, msgType MessageType) *Message {
	return &Message{
		ID:        id,
		ChannelID: channelID,
		Direction: direction,
		From:      from,
		Content:   content,
		Type:      msgType,
		Metadata:  make(map[string]interface{}),
		Timestamp: time.Now().UTC(),
	}
}

// SetMetadata sets a key-value pair in the message metadata.
// If Metadata is nil, it is initialized first.
func (m *Message) SetMetadata(key string, value interface{}) {
	if m.Metadata == nil {
		m.Metadata = make(map[string]interface{})
	}
	m.Metadata[key] = value
}

// GetMetadata retrieves a metadata value by key.
// Returns the value and true if the key exists, nil and false otherwise.
func (m *Message) GetMetadata(key string) (interface{}, bool) {
	if m.Metadata == nil {
		return nil, false
	}
	val, ok := m.Metadata[key]
	return val, ok
}

// IsInbound returns true if the message direction is inbound.
func (m *Message) IsInbound() bool {
	return m.Direction == DirectionInbound
}

// IsOutbound returns true if the message direction is outbound.
func (m *Message) IsOutbound() bool {
	return m.Direction == DirectionOutbound
}

// Validate validates the message fields.
// It checks that required fields are present and valid.
// Returns nil if the message is valid, or an appropriate error otherwise.
func (m *Message) Validate() error {
	if m.ID == "" {
		return ErrEmptyID
	}
	if m.ChannelID == "" {
		return ErrEmptyChannelID
	}
	if !isValidMessageType(m.Type) {
		return ErrInvalidMessageType
	}
	if m.Content == "" && m.Type != MessageTypeEvent {
		return ErrEmptyContent
	}
	return nil
}

// isValidMessageType checks if the message type is valid.
func isValidMessageType(t MessageType) bool {
	switch t {
	case MessageTypeText, MessageTypeImage, MessageTypeFile,
		MessageTypeAudio, MessageTypeVideo, MessageTypeEvent:
		return true
	default:
		return false
	}
}

// MarshalJSON implements json.Marshaler.
// It serializes the message to JSON format.
func (m *Message) MarshalJSON() ([]byte, error) {
	type Alias Message
	return json.Marshal((*Alias)(m))
}

// UnmarshalJSON implements json.Unmarshaler.
// It deserializes JSON data into the message.
func (m *Message) UnmarshalJSON(data []byte) error {
	type Alias Message
	return json.Unmarshal(data, (*Alias)(m))
}
