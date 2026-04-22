package gateway

import (
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

type session struct {
	id        string
	clientID  string
	total     int
	parts     map[int][]byte
	createdAt time.Time
}

type SessionManager struct {
	mu           sync.RWMutex
	sessions     map[string]*session
	timeout      time.Duration
	done         chan struct{}
	once         sync.Once
	maxSessions  int
	currentCount int32
}

const defaultMaxSessions = 1000

func newSessionManager(timeout time.Duration) *SessionManager {
	sm := &SessionManager{
		sessions:     make(map[string]*session),
		timeout:      timeout,
		done:         make(chan struct{}),
		maxSessions:  defaultMaxSessions,
	}
	go sm.cleanupLoop()
	return sm
}

func (sm *SessionManager) create(clientID string, total int) (*session, error) {
	count := atomic.AddInt32(&sm.currentCount, 1)
	if int(count) > sm.maxSessions {
		atomic.AddInt32(&sm.currentCount, -1)
		return nil, fmt.Errorf("session limit reached (%d)", sm.maxSessions)
	}
	id := uuid.New().String()
	s := &session{
		id:        id,
		clientID:  clientID,
		total:     total,
		parts:     make(map[int][]byte, total),
		createdAt: time.Now(),
	}
	sm.mu.Lock()
	sm.sessions[id] = s
	sm.mu.Unlock()
	return s, nil
}

func (sm *SessionManager) get(id string) (*session, bool) {
	sm.mu.RLock()
	s, ok := sm.sessions[id]
	sm.mu.RUnlock()
	return s, ok
}

func (sm *SessionManager) addData(id string, index int, data []byte) error {
	sm.mu.RLock()
	s, ok := sm.sessions[id]
	sm.mu.RUnlock()
	if !ok {
		return ErrSessionNotFound
	}
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if _, exists := s.parts[index]; exists {
		return nil
	}
	s.parts[index] = data
	return nil
}

func (sm *SessionManager) assembleAndRemove(id string) (*session, bool) {
	sm.mu.Lock()
	s, ok := sm.sessions[id]
	if ok {
		delete(sm.sessions, id)
		atomic.AddInt32(&sm.currentCount, -1)
	}
	sm.mu.Unlock()
	return s, ok && len(s.parts) == s.total
}

func (sm *SessionManager) remove(id string) {
	sm.mu.Lock()
	if _, exists := sm.sessions[id]; exists {
		delete(sm.sessions, id)
		atomic.AddInt32(&sm.currentCount, -1)
	}
	sm.mu.Unlock()
}

func (sm *SessionManager) cleanupClient(clientID string) {
	var ids []string
	sm.mu.RLock()
	for id, s := range sm.sessions {
		if s.clientID == clientID {
			ids = append(ids, id)
		}
	}
	sm.mu.RUnlock()
	for _, id := range ids {
		sm.remove(id)
	}
}

func (sm *SessionManager) Close() { sm.once.Do(func() { close(sm.done) }) }

func (sm *SessionManager) cleanupLoop() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			now := time.Now()
			var expired []string
			sm.mu.RLock()
			for id, s := range sm.sessions {
				if now.Sub(s.createdAt) > sm.timeout {
					expired = append(expired, id)
				}
			}
			sm.mu.RUnlock()
			for _, id := range expired {
				sm.remove(id)
				slog.Warn("session expired", "id", id)
			}
		case <-sm.done:
			return
		}
	}
}
