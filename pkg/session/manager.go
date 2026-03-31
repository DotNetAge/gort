// Package session provides WebSocket session management for client connections.
// It handles the lifecycle of client connections including connection establishment,
// message handling, heartbeat monitoring, and graceful disconnection.
//
// Session Architecture:
//
//	┌─────────────┐     ┌─────────────┐     ┌─────────────┐
//	│   Client    │◀───▶│   Session   │◀───▶│   Manager   │
//	│  (WebSocket)│     │             │     │             │
//	└─────────────┘     └─────────────┘     └─────────────┘
//
// Each client connection is wrapped in a Session that tracks:
//   - Connection state and metadata
//   - Last activity timestamp
//   - Custom metadata for application use
//
// The Manager handles multiple sessions concurrently:
//   - Session registration and lookup
//   - Broadcast to all connected clients
//   - Graceful shutdown with timeout
//
// Basic Usage:
//
//	// Create session manager
//	manager := session.NewManager(session.Config{
//	    OnConnect: func(id string) {
//	        log.Printf("Client %s connected", id)
//	    },
//	    OnDisconnect: func(id string) {
//	        log.Printf("Client %s disconnected", id)
//	    },
//	    OnMessage: func(id string, msg *message.Message) {
//	        log.Printf("Message from %s: %s", id, msg.Content)
//	    },
//	})
//
//	// Add a new session
//	conn, _ := upgrader.Upgrade(w, r, nil)
//	if err := manager.AddSession("client-001", conn); err != nil {
//	    log.Fatal(err)
//	}
//
//	// Broadcast to all clients
//	msg := message.NewMessage("system", "Hello everyone!")
//	manager.Broadcast(ctx, msg)
//
//	// Graceful shutdown
//	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
//	defer cancel()
//	manager.Stop(ctx)
//
// Thread Safety:
//
// The Manager and Session types are safe for concurrent use.
// Multiple goroutines can safely call methods on the same Manager instance.
package session

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/DotNetAge/gort/pkg/message"
	"github.com/gorilla/websocket"
)

// Error definitions for session operations.
var (
	// ErrSessionNotFound is returned when attempting to access a session that doesn't exist.
	ErrSessionNotFound = errors.New("session not found")

	// ErrSessionExists is returned when attempting to add a session with an ID that already exists.
	ErrSessionExists = errors.New("session already exists")
)

// Default configuration values for heartbeat management.
const (
	// DefaultHeartbeatInterval is the default interval between heartbeat checks.
	DefaultHeartbeatInterval = 30 * time.Second

	// DefaultHeartbeatTimeout is the default timeout for heartbeat responses.
	DefaultHeartbeatTimeout = 90 * time.Second
)

// Session represents a single client connection.
// It wraps a WebSocket connection and tracks connection state and metadata.
//
// This type is safe for concurrent use.
type Session struct {
	// ID is the unique identifier for this session.
	ID string

	// Conn is the underlying WebSocket connection.
	// This is protected by mu for thread-safe access.
	Conn *websocket.Conn

	// Connected is the timestamp when the session was created.
	Connected time.Time

	// LastActive is the timestamp of the last activity on this session.
	LastActive time.Time

	// Metadata contains custom data associated with this session.
	Metadata map[string]interface{}

	// mu protects Conn and Metadata for thread-safe access.
	mu sync.Mutex
}

// NewSession creates a new Session with the given ID and connection.
//
// Parameters:
//   - id: Unique identifier for the session
//   - conn: The WebSocket connection
//
// Returns a new Session with Connected and LastActive set to the current time.
//
// Example:
//
//	conn, _ := upgrader.Upgrade(w, r, nil)
//	session := session.NewSession("client-001", conn)
func NewSession(id string, conn *websocket.Conn) *Session {
	now := time.Now()
	return &Session{
		ID:         id,
		Conn:       conn,
		Connected:  now,
		LastActive: now,
		Metadata:   make(map[string]interface{}),
	}
}

// Send sends data to the client through the WebSocket connection.
//
// Parameters:
//   - msg: The data to send (typically JSON-encoded message)
//
// Returns ErrSessionNotFound if the connection is nil or closed.
//
// Example:
//
//	data, _ := json.Marshal(message)
//	if err := session.Send(data); err != nil {
//	    log.Printf("Send failed: %v", err)
//	}
func (s *Session) Send(msg []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.Conn == nil {
		return ErrSessionNotFound
	}

	s.LastActive = time.Now()
	return s.Conn.WriteMessage(websocket.TextMessage, msg)
}

// Close closes the WebSocket connection and cleans up resources.
// This method is safe to call multiple times.
//
// Returns any error from closing the underlying connection.
func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.Conn == nil {
		return nil
	}

	err := s.Conn.Close()
	s.Conn = nil
	return err
}

// IsConnected safely checks if the session has an active connection.
// This method is thread-safe and can be called concurrently.
//
// Returns true if the connection is not nil.
func (s *Session) IsConnected() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Conn != nil
}

// SetMetadata sets a key-value pair in the session metadata.
// If Metadata is nil, it is initialized first.
//
// Parameters:
//   - key: The metadata key
//   - value: The metadata value
//
// Example:
//
//	session.SetMetadata("user_id", "user_123")
//	session.SetMetadata("permissions", []string{"read", "write"})
func (s *Session) SetMetadata(key string, value interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.Metadata == nil {
		s.Metadata = make(map[string]interface{})
	}
	s.Metadata[key] = value
}

// GetMetadata retrieves a metadata value by key.
//
// Parameters:
//   - key: The metadata key to retrieve
//
// Returns the value and true if found, nil and false otherwise.
//
// Example:
//
//	if userID, ok := session.GetMetadata("user_id"); ok {
//	    fmt.Printf("User: %v\n", userID)
//	}
func (s *Session) GetMetadata(key string) (interface{}, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.Metadata == nil {
		return nil, false
	}
	val, ok := s.Metadata[key]
	return val, ok
}

// Config contains configuration options for the session manager.
type Config struct {
	// OnMessage is called when a message is received from a client.
	// The session ID and parsed message are provided.
	OnMessage func(clientID string, msg *message.Message)

	// OnConnect is called when a new client connects.
	OnConnect func(clientID string)

	// OnDisconnect is called when a client disconnects.
	OnDisconnect func(clientID string)

	// HeartbeatInterval is the interval between heartbeat checks.
	// Defaults to DefaultHeartbeatInterval if zero.
	HeartbeatInterval time.Duration

	// HeartbeatTimeout is the timeout for heartbeat responses.
	// Defaults to DefaultHeartbeatTimeout if zero.
	HeartbeatTimeout time.Duration
}

// Manager handles multiple WebSocket sessions concurrently.
// It provides methods for session management, broadcasting, and lifecycle control.
//
// This type is safe for concurrent use.
type Manager struct {
	sessions sync.Map
	config   Config
	mu       sync.RWMutex
	running  bool
	cancel   context.CancelFunc
	stopped  bool
}

// NewManager creates a new Manager with the given configuration.
//
// Parameters:
//   - config: Configuration options for the manager
//
// Returns a new Manager with default values applied for zero fields.
//
// Example:
//
//	manager := session.NewManager(session.Config{
//	    OnConnect: func(id string) {
//	        log.Printf("Connected: %s", id)
//	    },
//	})
func NewManager(config Config) *Manager {
	if config.HeartbeatInterval == 0 {
		config.HeartbeatInterval = DefaultHeartbeatInterval
	}
	if config.HeartbeatTimeout == 0 {
		config.HeartbeatTimeout = DefaultHeartbeatTimeout
	}

	return &Manager{
		config:  config,
		running: true,
	}
}

// AddSession adds a new session to the manager.
//
// Parameters:
//   - id: Unique identifier for the session
//   - conn: The WebSocket connection
//
// Returns ErrSessionExists if a session with the same ID already exists.
//
// Example:
//
//	conn, _ := upgrader.Upgrade(w, r, nil)
//	if err := manager.AddSession("client-001", conn); err != nil {
//	    http.Error(w, err.Error(), http.StatusConflict)
//	    return
//	}
func (m *Manager) AddSession(id string, conn *websocket.Conn) error {
	session := NewSession(id, conn)

	_, loaded := m.sessions.LoadOrStore(id, session)
	if loaded {
		conn.Close()
		return ErrSessionExists
	}

	if m.config.OnConnect != nil {
		m.config.OnConnect(id)
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.mu.Lock()
	m.cancel = cancel
	m.mu.Unlock()

	go m.readLoop(ctx, session)

	return nil
}

// RemoveSession removes a session from the manager and closes its connection.
//
// Parameters:
//   - id: The session ID to remove
//
// This method is safe to call even if the session doesn't exist.
func (m *Manager) RemoveSession(id string) {
	m.mu.RLock()
	stopped := m.stopped
	m.mu.RUnlock()

	if stopped {
		return
	}

	val, ok := m.sessions.LoadAndDelete(id)
	if !ok {
		return
	}

	session := val.(*Session)
	session.Close()

	if m.config.OnDisconnect != nil {
		m.config.OnDisconnect(id)
	}
}

// GetSession retrieves a session by ID.
//
// Parameters:
//   - id: The session ID to look up
//
// Returns the session and true if found, nil and false otherwise.
//
// Example:
//
//	if session, ok := manager.GetSession("client-001"); ok {
//	    session.Send(data)
//	}
func (m *Manager) GetSession(id string) (*Session, bool) {
	val, ok := m.sessions.Load(id)
	if !ok {
		return nil, false
	}
	return val.(*Session), true
}

// Broadcast sends a message to all connected clients.
// Messages are sent concurrently to all sessions.
//
// Parameters:
//   - ctx: Context for cancellation
//   - msg: The message to broadcast
//
// Returns the first error encountered, or nil if all sends succeeded.
//
// Example:
//
//	msg := message.NewMessage("system", "Hello everyone!")
//	if err := manager.Broadcast(ctx, msg); err != nil {
//	    log.Printf("Broadcast error: %v", err)
//	}
func (m *Manager) Broadcast(ctx context.Context, msg *message.Message) error {
	data, err := msg.MarshalJSON()
	if err != nil {
		return err
	}

	var wg sync.WaitGroup
	var errMu sync.Mutex
	var firstErr error

	m.sessions.Range(func(key, value interface{}) bool {
		wg.Add(1)
		go func(s *Session) {
			defer wg.Done()
			if err := s.Send(data); err != nil {
				errMu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				errMu.Unlock()
			}
		}(value.(*Session))
		return true
	})

	wg.Wait()
	return firstErr
}

// SendTo sends a message to a specific client.
//
// Parameters:
//   - ctx: Context for cancellation
//   - sessionID: The target session ID
//   - msg: The message to send
//
// Returns ErrSessionNotFound if the session doesn't exist.
//
// Example:
//
//	msg := message.NewMessage("system", "Private message")
//	if err := manager.SendTo(ctx, "client-001", msg); err != nil {
//	    log.Printf("Send error: %v", err)
//	}
func (m *Manager) SendTo(ctx context.Context, sessionID string, msg *message.Message) error {
	session, ok := m.GetSession(sessionID)
	if !ok {
		return ErrSessionNotFound
	}

	data, err := msg.MarshalJSON()
	if err != nil {
		return err
	}

	return session.Send(data)
}

// GetClientCount returns the number of currently connected clients.
//
// Returns 0 if the manager has been stopped.
//
// Example:
//
//	fmt.Printf("Connected clients: %d\n", manager.GetClientCount())
func (m *Manager) GetClientCount() int {
	m.mu.RLock()
	if m.stopped {
		m.mu.RUnlock()
		return 0
	}
	m.mu.RUnlock()

	count := 0
	m.sessions.Range(func(key, value interface{}) bool {
		count++
		return true
	})
	return count
}

// GetSessionIDs returns a slice of all active session IDs.
//
// Returns a slice containing all session IDs.
//
// Example:
//
//	ids := manager.GetSessionIDs()
//	for _, id := range ids {
//	    fmt.Println(id)
//	}
func (m *Manager) GetSessionIDs() []string {
	ids := make([]string, 0)
	m.sessions.Range(func(key, value interface{}) bool {
		ids = append(ids, key.(string))
		return true
	})
	return ids
}

// Stop gracefully shuts down the manager and closes all sessions.
//
// Parameters:
//   - ctx: Context for timeout control
//
// Returns ctx.Err() if the shutdown times out.
//
// Example:
//
//	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
//	defer cancel()
//
//	if err := manager.Stop(ctx); err != nil {
//	    log.Printf("Shutdown error: %v", err)
//	}
func (m *Manager) Stop(ctx context.Context) error {
	m.mu.Lock()
	m.running = false
	m.stopped = true
	if m.cancel != nil {
		m.cancel()
	}
	m.mu.Unlock()

	var wg sync.WaitGroup
	m.sessions.Range(func(key, value interface{}) bool {
		wg.Add(1)
		go func(s *Session) {
			defer wg.Done()
			s.Close()
		}(value.(*Session))
		return true
	})

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// readLoop continuously reads messages from a session.
// It runs in a separate goroutine for each session.
func (m *Manager) readLoop(ctx context.Context, session *Session) {
	defer func() {
		m.mu.RLock()
		stopped := m.stopped
		m.mu.RUnlock()

		if !stopped {
			session.Close()
			m.RemoveSession(session.ID)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		default:
			// 使用 IsConnected 方法安全地检查连接状态
			if !session.IsConnected() {
				return
			}

			// 在读取消息前获取连接引用
			session.mu.Lock()
			conn := session.Conn
			session.mu.Unlock()

			if conn == nil {
				return
			}

			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}

			session.mu.Lock()
			session.LastActive = time.Now()
			session.mu.Unlock()

			if m.config.OnMessage != nil {
				var msg message.Message
				if err := msg.UnmarshalJSON(data); err != nil {
					continue
				}
				m.config.OnMessage(session.ID, &msg)
			}
		}
	}
}
