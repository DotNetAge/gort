// Package message provides the standard message format used throughout the gort system.
// It defines the core data structures for message passing between different components,
// enabling unified message handling across various messaging platforms.
//
// Message Structure:
//
//	┌─────────────────────────────────────────┐
//	│              Message                    │
//	├─────────────────────────────────────────┤
//	│ ID          - Unique identifier         │
//	│ ChannelID   - Source/destination        │
//	│ Direction   - Inbound or Outbound       │
//	│ From        - Sender information        │
//	│ To          - Recipient information     │
//	│ Content     - Message content           │
//	│ Type        - Content type              │
//	│ Metadata    - Platform-specific data    │
//	│ Timestamp   - Creation time             │
//	└─────────────────────────────────────────┘
//
// Basic Usage:
//
//	// Create a new message
//	msg := message.NewMessage(
//	    "msg_001",
//	    "wechat-channel",
//	    message.DirectionInbound,
//	    message.UserInfo{ID: "user_001", Name: "Alice"},
//	    "Hello, World!",
//	    message.MessageTypeText,
//	)
//
//	// Add metadata
//	msg.SetMetadata("priority", "high")
//	msg.SetMetadata("source_ip", "192.168.1.1")
//
//	// Validate the message
//	if err := msg.Validate(); err != nil {
//	    log.Fatal(err)
//	}
//
//	// Check direction
//	if msg.IsInbound() {
//	    fmt.Println("Processing incoming message")
//	}
//
// Message Types:
//
// The package supports various message types for different content:
//   - Text: Plain text messages
//   - Image: Image URLs or base64 encoded images
//   - File: File attachments
//   - Audio: Audio/voice messages
//   - Video: Video messages
//   - Markdown: Markdown formatted text
//   - Event: System events (user join/leave, etc.)
//
// Thread Safety:
//
// Message values are not safe for concurrent modification. If you need to share
// a message across goroutines, create a copy or ensure proper synchronization.
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
	// Content field contains the text itself.
	MessageTypeText MessageType = "text"

	// MessageTypeImage represents an image message with a URL to the image.
	// Content field typically contains the image URL.
	// Additional metadata may include width, height, etc.
	MessageTypeImage MessageType = "image"

	// MessageTypeFile represents a file attachment message.
	// Content field contains the file URL or identifier.
	// Metadata includes file_name, file_size, etc.
	MessageTypeFile MessageType = "file"

	// MessageTypeAudio represents an audio/voice message.
	// Content field contains the audio URL.
	MessageTypeAudio MessageType = "audio"

	// MessageTypeVideo represents a video message.
	// Content field contains the video URL.
	// Metadata may include duration, thumbnail, etc.
	MessageTypeVideo MessageType = "video"

	// MessageTypeVoice represents a voice message.
	// Similar to audio but specifically for voice recordings.
	MessageTypeVoice MessageType = "voice"

	// MessageTypeMarkdown represents a markdown formatted message.
	// Content field contains markdown text.
	MessageTypeMarkdown MessageType = "markdown"

	// MessageTypeNews represents a news/article message.
	// Used for rich content with title, description, image, and URL.
	MessageTypeNews MessageType = "news"

	// MessageTypeTemplateCard represents a template card message.
	// Used for structured messages with interactive elements.
	MessageTypeTemplateCard MessageType = "template_card"

	// MessageTypeEvent represents a system event message (e.g., user join/leave).
	// These are not user-generated messages but system notifications.
	MessageTypeEvent MessageType = "event"
)

// UserInfo represents information about a user in the system.
// It contains identifying information and display details.
type UserInfo struct {
	// ID is the platform-specific user identifier.
	// This is unique within the platform but may not be globally unique.
	ID string `json:"id"`

	// Name is the display name of the user.
	// This is shown in UI and logs.
	Name string `json:"name"`

	// Avatar is the URL to the user's avatar image.
	// May be empty if the user has no avatar.
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
	// Common keys: "file_name", "file_size", "duration", "mime_type"
	Metadata map[string]interface{} `json:"metadata"`

	// Timestamp is when the message was created or received.
	Timestamp time.Time `json:"timestamp"`
}

// NewMessage creates a new Message with the given parameters.
// It initializes the Metadata map and sets the Timestamp to the current UTC time.
//
// Parameters:
//   - id: Unique identifier for the message
//   - channelID: The channel identifier (source or destination)
//   - direction: Message direction (inbound or outbound)
//   - from: Sender information
//   - content: The message content
//   - msgType: The type of message
//
// Returns a pointer to the newly created Message.
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
//
// Parameters:
//   - key: The metadata key
//   - value: The metadata value (can be any type)
//
// Example:
//
//	msg.SetMetadata("priority", "high")
//	msg.SetMetadata("retry_count", 3)
func (m *Message) SetMetadata(key string, value interface{}) {
	if m.Metadata == nil {
		m.Metadata = make(map[string]interface{})
	}
	m.Metadata[key] = value
}

// GetMetadata retrieves a metadata value by key.
// Returns the value and true if the key exists, nil and false otherwise.
//
// Parameters:
//   - key: The metadata key to retrieve
//
// Returns the value and a boolean indicating if the key was found.
//
// Example:
//
//	if priority, ok := msg.GetMetadata("priority"); ok {
//	    fmt.Printf("Priority: %v\n", priority)
//	}
func (m *Message) GetMetadata(key string) (interface{}, bool) {
	if m.Metadata == nil {
		return nil, false
	}
	val, ok := m.Metadata[key]
	return val, ok
}

// IsInbound returns true if the message direction is inbound.
// This is a convenience method for checking Direction == DirectionInbound.
//
// Example:
//
//	if msg.IsInbound() {
//	    // Process incoming message
//	}
func (m *Message) IsInbound() bool {
	return m.Direction == DirectionInbound
}

// IsOutbound returns true if the message direction is outbound.
// This is a convenience method for checking Direction == DirectionOutbound.
//
// Example:
//
//	if msg.IsOutbound() {
//	    // Process outgoing message
//	}
func (m *Message) IsOutbound() bool {
	return m.Direction == DirectionOutbound
}

// Validate validates the message fields.
// It checks that required fields are present and valid.
// Returns nil if the message is valid, or an appropriate error otherwise.
//
// Validation rules:
//   - ID cannot be empty
//   - ChannelID cannot be empty
//   - Type must be a valid MessageType
//   - Content cannot be empty (except for event messages)
//
// Example:
//
//	if err := msg.Validate(); err != nil {
//	    log.Printf("Invalid message: %v", err)
//	    return
//	}
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
// This is an internal helper function used by Validate.
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
//
// This method ensures proper JSON encoding of all fields,
// including the time.Time Timestamp field.
//
// Example:
//
//	data, err := json.Marshal(msg)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Println(string(data))
func (m *Message) MarshalJSON() ([]byte, error) {
	type Alias Message
	return json.Marshal((*Alias)(m))
}

// UnmarshalJSON implements json.Unmarshaler.
// It deserializes JSON data into the message.
//
// This method ensures proper JSON decoding of all fields,
// including the time.Time Timestamp field.
//
// Example:
//
//	var msg message.Message
//	if err := json.Unmarshal(data, &msg); err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Println(msg.Content)
func (m *Message) UnmarshalJSON(data []byte) error {
	type Alias Message
	return json.Unmarshal(data, (*Alias)(m))
}
