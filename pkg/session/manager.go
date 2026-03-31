package session

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/example/gort/pkg/message"
	"github.com/gorilla/websocket"
)

var (
	ErrSessionNotFound = errors.New("session not found")
	ErrSessionExists   = errors.New("session already exists")
)

const (
	DefaultHeartbeatInterval = 30 * time.Second
	DefaultHeartbeatTimeout  = 90 * time.Second
)

type Session struct {
	ID         string
	Conn       *websocket.Conn
	Connected  time.Time
	LastActive time.Time
	Metadata   map[string]interface{}
	mu         sync.Mutex
}

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

func (s *Session) Send(msg []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.Conn == nil {
		return ErrSessionNotFound
	}

	s.LastActive = time.Now()
	return s.Conn.WriteMessage(websocket.TextMessage, msg)
}

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

func (s *Session) SetMetadata(key string, value interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.Metadata == nil {
		s.Metadata = make(map[string]interface{})
	}
	s.Metadata[key] = value
}

func (s *Session) GetMetadata(key string) (interface{}, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.Metadata == nil {
		return nil, false
	}
	val, ok := s.Metadata[key]
	return val, ok
}

type Config struct {
	OnMessage         func(clientID string, msg *message.Message)
	OnConnect         func(clientID string)
	OnDisconnect      func(clientID string)
	HeartbeatInterval time.Duration
	HeartbeatTimeout  time.Duration
}

type Manager struct {
	sessions sync.Map
	config   Config
	mu       sync.RWMutex
	running  bool
	cancel   context.CancelFunc
	stopped  bool
}

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

func (m *Manager) GetSession(id string) (*Session, bool) {
	val, ok := m.sessions.Load(id)
	if !ok {
		return nil, false
	}
	return val.(*Session), true
}

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

func (m *Manager) GetSessionIDs() []string {
	ids := make([]string, 0)
	m.sessions.Range(func(key, value interface{}) bool {
		ids = append(ids, key.(string))
		return true
	})
	return ids
}

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
			if session.Conn == nil {
				return
			}
			_, data, err := session.Conn.ReadMessage()
			if err != nil {
				return
			}

			session.LastActive = time.Now()

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
