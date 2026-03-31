// Package gateway provides the core message routing and coordination layer for the gort system.
// It manages channels, sessions, and middleware to enable bidirectional message flow between
// clients and messaging platforms.
//
// Architecture Overview:
//
//	┌─────────────┐     ┌─────────────┐     ┌─────────────┐
//	│   Clients   │────▶│   Gateway   │────▶│  Channels   │
//	│  (WebSocket)│◀────│             │◀────│ (Platforms) │
//	└─────────────┘     └─────────────┘     └─────────────┘
//
// The Gateway acts as a central hub:
//   - Receives messages from external platforms via Channels
//   - Routes messages to connected clients via Sessions
//   - Receives messages from clients and routes to appropriate Channels
//   - Applies middleware for logging, authentication, rate limiting, etc.
//
// Basic Usage:
//
//	// Create session manager
//	sessionManager := session.NewManager(session.Config{
//	    OnMessage: func(clientID string, msg *message.Message) {
//	        log.Printf("Message from %s: %s", clientID, msg.Content)
//	    },
//	})
//
//	// Create gateway
//	gateway := gateway.New(sessionManager)
//
//	// Register channels
//	ch, _ := telegram.NewChannel("telegram-bot", config)
//	gateway.RegisterChannel(ch)
//
//	// Register handlers
//	gateway.RegisterChannelHandler(func(ctx context.Context, msg *message.Message) error {
//	    log.Printf("Channel message: %s", msg.Content)
//	    return nil
//	})
//
//	// Start the gateway
//	ctx := context.Background()
//	if err := gateway.Start(ctx); err != nil {
//	    log.Fatal(err)
//	}
//
// Thread Safety:
//
// The Gateway type is safe for concurrent use. All public methods can be called
// from multiple goroutines without additional synchronization.
package gateway

import (
	"context"
	"errors"
	"sync"

	"github.com/DotNetAge/gort/pkg/channel"
	"github.com/DotNetAge/gort/pkg/message"
	"github.com/DotNetAge/gort/pkg/middleware"
	"github.com/DotNetAge/gort/pkg/session"
)

// Error definitions for gateway operations.
var (
	// ErrChannelNotFound is returned when attempting to send a message to a channel that doesn't exist.
	ErrChannelNotFound = errors.New("channel not found")

	// ErrNotRunning is returned when attempting operations on a stopped gateway.
	ErrNotRunning = errors.New("gateway is not running")

	// ErrAlreadyRunning is returned when attempting to start an already running gateway.
	ErrAlreadyRunning = errors.New("gateway is already running")
)

// ClientHandler is the function type for handling messages from connected clients.
// It receives the context, client identifier, and the message.
//
// The handler should return nil on success, or an error if message processing fails.
// Errors are propagated back to the caller.
//
// Example:
//
//	handler := func(ctx context.Context, clientID string, msg *message.Message) error {
//	    log.Printf("Client %s sent: %s", clientID, msg.Content)
//	    // Process client message...
//	    return nil
//	}
type ClientHandler func(ctx context.Context, clientID string, msg *message.Message) error

// ChannelHandler is the function type for handling messages from channels.
// It receives the context and the message from the external platform.
//
// The handler should return nil on success, or an error if message processing fails.
// Errors are propagated back to the caller.
//
// Example:
//
//	handler := func(ctx context.Context, msg *message.Message) error {
//	    log.Printf("Received from %s: %s", msg.ChannelID, msg.Content)
//	    // Process channel message...
//	    return nil
//	}
type ChannelHandler func(ctx context.Context, msg *message.Message) error

// Gateway is the core message routing component that coordinates between channels,
// sessions, and middleware. It manages the flow of messages in both directions:
// from external platforms to clients, and from clients to external platforms.
//
// The Gateway maintains:
//   - A registry of all configured channels
//   - A session manager for connected clients
//   - A middleware chain for message processing
//
// This type is safe for concurrent use.
type Gateway struct {
	registry       *channel.Registry
	sessionManager *session.Manager
	middleware     *middleware.Chain

	clientHandler  ClientHandler
	channelHandler ChannelHandler

	running bool
	mu      sync.RWMutex
}

// New creates a new Gateway instance with the given session manager.
//
// Parameters:
//   - sessionManager: The session manager for handling client connections
//
// Returns a new Gateway initialized with an empty channel registry and middleware chain.
//
// Example:
//
//	sessionManager := session.NewManager(session.Config{})
//	gateway := gateway.New(sessionManager)
func New(sessionManager *session.Manager) *Gateway {
	return &Gateway{
		registry:       channel.NewRegistry(),
		sessionManager: sessionManager,
		middleware:     middleware.NewChain(),
	}
}

// RegisterChannel adds a channel to the gateway's registry.
// The channel can be started and stopped via the gateway's lifecycle methods.
//
// Parameters:
//   - ch: The channel to register
//
// Returns an error if a channel with the same name already exists.
//
// Example:
//
//	ch, _ := telegram.NewChannel("my-bot", config)
//	if err := gateway.RegisterChannel(ch); err != nil {
//	    log.Fatal(err)
//	}
func (g *Gateway) RegisterChannel(ch channel.Channel) error {
	return g.registry.Register(ch)
}

// GetChannel retrieves a channel by name from the registry.
//
// Parameters:
//   - name: The name of the channel to retrieve
//
// Returns the channel and true if found, or nil and false if not found.
//
// Example:
//
//	if ch, ok := gateway.GetChannel("my-bot"); ok {
//	    fmt.Println(ch.Type())
//	}
func (g *Gateway) GetChannel(name string) (channel.Channel, bool) {
	return g.registry.Get(name)
}

// RegisterClientHandler sets the handler for messages from connected clients.
// This handler is called for each message sent by a client before routing to channels.
//
// Parameters:
//   - handler: The function to handle client messages
//
// Example:
//
//	gateway.RegisterClientHandler(func(ctx context.Context, clientID string, msg *message.Message) error {
//	    log.Printf("From client %s: %s", clientID, msg.Content)
//	    return nil
//	})
func (g *Gateway) RegisterClientHandler(handler ClientHandler) {
	g.clientHandler = handler
}

// RegisterChannelHandler sets the handler for messages from external channels.
// This handler is called for each message received from a channel before broadcasting to clients.
//
// Parameters:
//   - handler: The function to handle channel messages
//
// Example:
//
//	gateway.RegisterChannelHandler(func(ctx context.Context, msg *message.Message) error {
//	    log.Printf("From channel %s: %s", msg.ChannelID, msg.Content)
//	    return nil
//	})
func (g *Gateway) RegisterChannelHandler(handler ChannelHandler) {
	g.channelHandler = handler
}

// Use adds a middleware to the gateway's processing chain.
// Middleware is applied to all messages passing through the gateway.
//
// Parameters:
//   - m: The middleware to add
//
// Example:
//
//	gateway.Use(middleware.NewLoggingMiddleware(logger))
//	gateway.Use(middleware.NewAuthMiddleware(validator))
func (g *Gateway) Use(m middleware.Middleware) {
	g.middleware.Use(m)
}

// Start initializes and starts all registered channels.
// This begins listening for messages from external platforms.
//
// Parameters:
//   - ctx: Context for cancellation and timeouts
//
// Returns ErrAlreadyRunning if the gateway is already running,
// or an error if any channel fails to start.
//
// Example:
//
//	ctx := context.Background()
//	if err := gateway.Start(ctx); err != nil {
//	    log.Fatal(err)
//	}
//	defer gateway.Stop(ctx)
func (g *Gateway) Start(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.running {
		return ErrAlreadyRunning
	}

	for _, ch := range g.registry.GetAll() {
		if err := ch.Start(ctx, g.handleChannelMessage); err != nil {
			g.stopChannels(ctx)
			return err
		}
	}

	g.running = true
	return nil
}

// Stop gracefully shuts down the gateway and all registered channels.
// This stops listening for messages and closes all channel connections.
//
// Parameters:
//   - ctx: Context for cancellation and timeouts
//
// Returns ErrNotRunning if the gateway is not running,
// or an error if the session manager fails to stop.
//
// Example:
//
//	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
//	defer cancel()
//
//	if err := gateway.Stop(ctx); err != nil {
//	    log.Printf("Stop error: %v", err)
//	}
func (g *Gateway) Stop(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if !g.running {
		return ErrNotRunning
	}

	g.stopChannels(ctx)

	if err := g.sessionManager.Stop(ctx); err != nil {
		return err
	}

	g.running = false
	return nil
}

// stopChannels stops all registered channels.
// This is an internal helper method that should be called with the lock held.
func (g *Gateway) stopChannels(ctx context.Context) {
	for _, ch := range g.registry.GetAll() {
		ch.Stop(ctx)
	}
}

// IsRunning returns true if the gateway is currently active.
// This can be used to check the gateway status before operations.
//
// Example:
//
//	if gateway.IsRunning() {
//	    fmt.Println("Gateway is active")
//	}
func (g *Gateway) IsRunning() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.running
}

// HandleChannelMessage processes a message received from a channel.
// This applies middleware and routes the message to registered handlers and clients.
//
// Parameters:
//   - ctx: Context for cancellation
//   - msg: The message to process
//
// Returns an error if middleware processing or message routing fails.
//
// Note: This method is typically called internally by channel implementations,
// but can be used for testing or custom channel implementations.
func (g *Gateway) HandleChannelMessage(ctx context.Context, msg *message.Message) error {
	return g.middleware.Execute(ctx, msg, g.processChannelMessage)
}

// HandleClientMessage processes a message received from a client.
// This applies middleware and routes the message to the appropriate channel.
//
// Parameters:
//   - ctx: Context for cancellation
//   - clientID: The identifier of the sending client
//   - msg: The message to process
//
// Returns an error if middleware processing or message routing fails.
func (g *Gateway) HandleClientMessage(ctx context.Context, clientID string, msg *message.Message) error {
	return g.middleware.Execute(ctx, msg, func(ctx context.Context, msg *message.Message) error {
		return g.processClientMessage(ctx, clientID, msg)
	})
}

// GetClientCount returns the number of currently connected clients.
// This delegates to the session manager.
//
// Example:
//
//	fmt.Printf("Connected clients: %d\n", gateway.GetClientCount())
func (g *Gateway) GetClientCount() int {
	return g.sessionManager.GetClientCount()
}

// Broadcast sends a message to all connected clients.
// This delegates to the session manager.
//
// Parameters:
//   - ctx: Context for cancellation
//   - msg: The message to broadcast
//
// Returns an error if the broadcast fails.
//
// Example:
//
//	msg := message.NewMessage("system", "Broadcast message")
//	if err := gateway.Broadcast(ctx, msg); err != nil {
//	    log.Printf("Broadcast error: %v", err)
//	}
func (g *Gateway) Broadcast(ctx context.Context, msg *message.Message) error {
	return g.sessionManager.Broadcast(ctx, msg)
}

// SendTo sends a message to a specific client.
// This delegates to the session manager.
//
// Parameters:
//   - ctx: Context for cancellation
//   - clientID: The identifier of the target client
//   - msg: The message to send
//
// Returns an error if the client is not found or the send fails.
//
// Example:
//
//	msg := message.NewMessage("system", "Private message")
//	if err := gateway.SendTo(ctx, "client-123", msg); err != nil {
//	    log.Printf("Send error: %v", err)
//	}
func (g *Gateway) SendTo(ctx context.Context, clientID string, msg *message.Message) error {
	return g.sessionManager.SendTo(ctx, clientID, msg)
}

// handleChannelMessage is the internal handler for channel messages.
// It sets the message direction and processes through middleware.
func (g *Gateway) handleChannelMessage(ctx context.Context, msg *message.Message) error {
	msg.Direction = message.DirectionInbound
	return g.middleware.Execute(ctx, msg, g.processChannelMessage)
}

// processChannelMessage processes an inbound channel message.
// It calls the registered channel handler and broadcasts to all clients.
func (g *Gateway) processChannelMessage(ctx context.Context, msg *message.Message) error {
	if g.channelHandler != nil {
		if err := g.channelHandler(ctx, msg); err != nil {
			return err
		}
	}

	return g.sessionManager.Broadcast(ctx, msg)
}

// processClientMessage processes an outbound client message.
// It calls the registered client handler and routes to the appropriate channel.
func (g *Gateway) processClientMessage(ctx context.Context, clientID string, msg *message.Message) error {
	msg.Direction = message.DirectionOutbound

	if g.clientHandler != nil {
		if err := g.clientHandler(ctx, clientID, msg); err != nil {
			return err
		}
	}

	ch, ok := g.registry.Get(msg.ChannelID)
	if !ok {
		return ErrChannelNotFound
	}

	return ch.SendMessage(ctx, msg)
}
