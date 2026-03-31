// Package channel provides the Channel interface and implementations for various
// messaging platforms. Each adapter implements the Channel interface to enable
// unified message handling across different platforms.
package channel

import (
	"context"
	"errors"
	"sync"

	"github.com/example/gort/pkg/message"
)

// Error definitions for channel operations.
var (
	ErrChannelAlreadyRunning  = errors.New("channel is already running")
	ErrChannelNotRunning      = errors.New("channel is not running")
	ErrChannelNotFound        = errors.New("channel not found")
	ErrChannelAlreadyExists   = errors.New("channel already exists")
	ErrInvalidConfiguration   = errors.New("invalid channel configuration")
	ErrAuthenticationFailed   = errors.New("authentication failed")
	ErrRateLimited            = errors.New("rate limited")
	ErrMessageTooLarge        = errors.New("message too large")
	ErrUnsupportedMessageType = errors.New("unsupported message type")
)

// Status represents the current status of a channel.
type Status string

const (
	StatusStopped Status = "stopped"
	StatusRunning Status = "running"
	StatusError   Status = "error"
)

// ChannelType represents the type of messaging platform.
type ChannelType string

const (
	ChannelTypeWeChat    ChannelType = "wechat"
	ChannelTypeDingTalk  ChannelType = "dingtalk"
	ChannelTypeFeishu    ChannelType = "feishu"
	ChannelTypeTelegram  ChannelType = "telegram"
	ChannelTypeMessenger ChannelType = "messenger"
	ChannelTypeWhatsApp  ChannelType = "whatsapp"
	ChannelTypeIMessage  ChannelType = "imessage"
	ChannelTypeWeCom     ChannelType = "wecom"
	ChannelTypeSlack     ChannelType = "slack"
	ChannelTypeDiscord   ChannelType = "discord"
)

// MessageHandler is the function type for handling incoming messages.
type MessageHandler func(ctx context.Context, msg *message.Message) error

// Channel is the core interface for all messaging platform adapters.
// Implementations must be safe for concurrent use.
//
// Each adapter must embed code comments documenting the official API source:
// - API Documentation URL
// - API Version
// - Authentication Method
type Channel interface {
	// Name returns the unique identifier for this channel instance.
	Name() string

	// Type returns the channel type (e.g., "wechat", "telegram").
	Type() ChannelType

	// Start initializes the channel and begins listening for incoming messages.
	// The handler function will be called for each incoming message.
	Start(ctx context.Context, handler MessageHandler) error

	// Stop gracefully shuts down the channel.
	Stop(ctx context.Context) error

	// IsRunning returns true if the channel is currently active.
	IsRunning() bool

	// SendMessage sends a message through this channel.
	SendMessage(ctx context.Context, msg *message.Message) error

	// GetStatus returns the current status of the channel.
	GetStatus() Status
}

// WebhookHandler is an optional interface for channels that support webhook-based
// message reception. Channels implementing this interface can receive messages
// via HTTP callbacks.
type WebhookHandler interface {
	// HandleWebhook processes an incoming webhook request.
	// path: the URL path of the webhook
	// data: the raw request body
	// Returns the parsed message or an error.
	HandleWebhook(path string, data []byte) (*message.Message, error)
}

// OAuthConfig contains OAuth authentication configuration.
type OAuthConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Scopes       []string
}

// RateLimitConfig contains rate limiting configuration.
type RateLimitConfig struct {
	RequestsPerSecond int
	BurstSize         int
}

// ChannelCapabilities describes what features a channel supports.
type ChannelCapabilities struct {
	TextMessages     bool
	MarkdownMessages bool
	ImageMessages    bool
	FileMessages     bool
	AudioMessages    bool
	VideoMessages    bool
	VoiceMessages    bool
	NewsMessages     bool
	LocationMessages bool
	TemplateMessages bool
	TemplateCard     bool
	ReadReceipts     bool
	TypingIndicators bool
	MessageEditing   bool
	MessageDeletion  bool
	ReactionMessages bool
	BlockKit         bool
	Interactive      bool
	Threads          bool
}

// BaseChannel provides common functionality for all channel implementations.
type BaseChannel struct {
	name        string
	channelType ChannelType
	status      Status
	handler     MessageHandler
	mu          sync.RWMutex
}

// NewBaseChannel creates a new BaseChannel with the given name and type.
func NewBaseChannel(name string, channelType ChannelType) *BaseChannel {
	return &BaseChannel{
		name:        name,
		channelType: channelType,
		status:      StatusStopped,
	}
}

// Name returns the channel name.
func (b *BaseChannel) Name() string {
	return b.name
}

// Type returns the channel type.
func (b *BaseChannel) Type() ChannelType {
	return b.channelType
}

// GetStatus returns the current status.
func (b *BaseChannel) GetStatus() Status {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.status
}

// IsRunning returns true if the channel is running.
func (b *BaseChannel) IsRunning() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.status == StatusRunning
}

// SetStatus updates the channel status.
func (b *BaseChannel) SetStatus(status Status) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.status = status
}

// SetHandler sets the message handler.
func (b *BaseChannel) SetHandler(handler MessageHandler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handler = handler
}

// GetHandler returns the current message handler.
func (b *BaseChannel) GetHandler() MessageHandler {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.handler
}

// HandleMessage calls the registered handler with the given message.
func (b *BaseChannel) HandleMessage(ctx context.Context, msg *message.Message) error {
	b.mu.RLock()
	handler := b.handler
	b.mu.RUnlock()

	if handler == nil {
		return ErrChannelNotRunning
	}
	return handler(ctx, msg)
}

// Registry manages a collection of channels.
type Registry struct {
	channels map[string]Channel
	mu       sync.RWMutex
}

// NewRegistry creates a new empty registry.
func NewRegistry() *Registry {
	return &Registry{
		channels: make(map[string]Channel),
	}
}

// Register adds a channel to the registry.
func (r *Registry) Register(channel Channel) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := channel.Name()
	if _, exists := r.channels[name]; exists {
		return ErrChannelAlreadyExists
	}

	r.channels[name] = channel
	return nil
}

// Unregister removes a channel from the registry.
func (r *Registry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.channels, name)
}

// Get retrieves a channel by name.
func (r *Registry) Get(name string) (Channel, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	channel, ok := r.channels[name]
	return channel, ok
}

// GetAll returns all registered channels.
func (r *Registry) GetAll() []Channel {
	r.mu.RLock()
	defer r.mu.RUnlock()

	channels := make([]Channel, 0, len(r.channels))
	for _, c := range r.channels {
		channels = append(channels, c)
	}
	return channels
}

// Count returns the number of registered channels.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.channels)
}

// StartAll starts all registered channels.
func (r *Registry) StartAll(ctx context.Context, handler MessageHandler) error {
	r.mu.RLock()
	channels := make([]Channel, 0, len(r.channels))
	for _, c := range r.channels {
		channels = append(channels, c)
	}
	r.mu.RUnlock()

	for _, c := range channels {
		if err := c.Start(ctx, handler); err != nil {
			return err
		}
	}
	return nil
}

// StopAll stops all registered channels.
func (r *Registry) StopAll(ctx context.Context) error {
	r.mu.RLock()
	channels := make([]Channel, 0, len(r.channels))
	for _, c := range r.channels {
		channels = append(channels, c)
	}
	r.mu.RUnlock()

	var lastErr error
	for _, c := range channels {
		if err := c.Stop(ctx); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

// MockChannel is a mock implementation of the Channel interface for testing.
type MockChannel struct {
	*BaseChannel
	messages  []*message.Message
	sendError error
	started   bool
	mu        sync.Mutex
}

// NewMockChannel creates a new MockChannel for testing.
func NewMockChannel(name string, channelType ChannelType) *MockChannel {
	return &MockChannel{
		BaseChannel: NewBaseChannel(name, channelType),
		messages:    make([]*message.Message, 0),
	}
}

// Start starts the mock channel.
func (m *MockChannel) Start(ctx context.Context, handler MessageHandler) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return ErrChannelAlreadyRunning
	}

	m.started = true
	m.SetHandler(handler)
	m.SetStatus(StatusRunning)
	return nil
}

// Stop stops the mock channel.
func (m *MockChannel) Stop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.started {
		return ErrChannelNotRunning
	}

	m.started = false
	m.SetStatus(StatusStopped)
	m.SetHandler(nil)
	return nil
}

// IsRunning returns true if the channel is running.
func (m *MockChannel) IsRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.started
}

// SendMessage records the message for testing verification.
func (m *MockChannel) SendMessage(ctx context.Context, msg *message.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.sendError != nil {
		return m.sendError
	}

	m.messages = append(m.messages, msg)
	return nil
}

// GetMessages returns all sent messages.
func (m *MockChannel) GetMessages() []*message.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]*message.Message{}, m.messages...)
}

// SetSendError sets an error to be returned by SendMessage.
func (m *MockChannel) SetSendError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sendError = err
}

// SimulateMessage simulates receiving a message.
func (m *MockChannel) SimulateMessage(ctx context.Context, msg *message.Message) error {
	m.mu.Lock()
	started := m.started
	m.mu.Unlock()

	if !started {
		return ErrChannelNotRunning
	}

	return m.HandleMessage(ctx, msg)
}
