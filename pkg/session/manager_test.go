package session

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/example/gort/pkg/message"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func createTestWebSocketServer(t *testing.T, handler func(*websocket.Conn)) (*httptest.Server, string) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		if handler != nil {
			handler(conn)
		} else {
			for {
				_, _, err := conn.ReadMessage()
				if err != nil {
					return
				}
			}
		}
	}))

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	return server, wsURL
}

func TestNewSession(t *testing.T) {
	conn := &websocket.Conn{}
	session := NewSession("session_001", conn)

	assert.Equal(t, "session_001", session.ID)
	assert.Equal(t, conn, session.Conn)
	assert.NotZero(t, session.Connected)
	assert.NotZero(t, session.LastActive)
	assert.NotNil(t, session.Metadata)
}

func TestSession_Send_NilConn(t *testing.T) {
	session := &Session{ID: "session_001"}

	err := session.Send([]byte("test"))
	assert.Equal(t, ErrSessionNotFound, err)
}

func TestSession_Close(t *testing.T) {
	server, wsURL := createTestWebSocketServer(t, nil)
	defer server.Close()

	wsConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)

	session := NewSession("session_001", wsConn)
	err = session.Close()
	require.NoError(t, err)
	assert.Nil(t, session.Conn)
}

func TestSession_Metadata(t *testing.T) {
	session := NewSession("session_001", nil)

	session.SetMetadata("key1", "value1")
	val, ok := session.GetMetadata("key1")
	assert.True(t, ok)
	assert.Equal(t, "value1", val)

	val, ok = session.GetMetadata("nonexistent")
	assert.False(t, ok)
}

func TestNewManager(t *testing.T) {
	mgr := NewManager(Config{})

	assert.NotNil(t, mgr)
	assert.Equal(t, 30*time.Second, mgr.config.HeartbeatInterval)
	assert.Equal(t, 90*time.Second, mgr.config.HeartbeatTimeout)
}

func TestNewManager_CustomConfig(t *testing.T) {
	mgr := NewManager(Config{
		HeartbeatInterval: 10 * time.Second,
		HeartbeatTimeout:  30 * time.Second,
	})

	assert.Equal(t, 10*time.Second, mgr.config.HeartbeatInterval)
	assert.Equal(t, 30*time.Second, mgr.config.HeartbeatTimeout)
}

func TestManager_AddSession(t *testing.T) {
	server, wsURL := createTestWebSocketServer(t, nil)
	defer server.Close()

	wsConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)

	connectCalled := false
	mgr := NewManager(Config{
		OnConnect: func(clientID string) {
			connectCalled = true
		},
	})

	err = mgr.AddSession("session_001", wsConn)
	require.NoError(t, err)
	assert.True(t, connectCalled)
	assert.Equal(t, 1, mgr.GetClientCount())

	mgr.Stop(context.Background())
}

func TestManager_AddSession_Duplicate(t *testing.T) {
	server, wsURL := createTestWebSocketServer(t, nil)
	defer server.Close()

	wsConn1, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)

	mgr := NewManager(Config{})

	err = mgr.AddSession("session_001", wsConn1)
	require.NoError(t, err)

	wsConn2, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)

	err = mgr.AddSession("session_001", wsConn2)
	assert.Equal(t, ErrSessionExists, err)

	mgr.Stop(context.Background())
}

func TestManager_RemoveSession(t *testing.T) {
	server, wsURL := createTestWebSocketServer(t, nil)
	defer server.Close()

	wsConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)

	disconnectCalled := false
	mgr := NewManager(Config{
		OnDisconnect: func(clientID string) {
			disconnectCalled = true
		},
	})

	err = mgr.AddSession("session_001", wsConn)
	require.NoError(t, err)

	mgr.RemoveSession("session_001")
	assert.True(t, disconnectCalled)
	assert.Equal(t, 0, mgr.GetClientCount())
}

func TestManager_GetSession(t *testing.T) {
	server, wsURL := createTestWebSocketServer(t, nil)
	defer server.Close()

	wsConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)

	mgr := NewManager(Config{})

	err = mgr.AddSession("session_001", wsConn)
	require.NoError(t, err)

	session, ok := mgr.GetSession("session_001")
	assert.True(t, ok)
	assert.Equal(t, "session_001", session.ID)

	_, ok = mgr.GetSession("nonexistent")
	assert.False(t, ok)

	mgr.Stop(context.Background())
}

func TestManager_GetClientCount(t *testing.T) {
	mgr := NewManager(Config{})
	assert.Equal(t, 0, mgr.GetClientCount())

	server, wsURL := createTestWebSocketServer(t, nil)
	defer server.Close()

	for i := 0; i < 3; i++ {
		wsConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		require.NoError(t, err)
		err = mgr.AddSession(string(rune('a'+i)), wsConn)
		require.NoError(t, err)
	}

	assert.Equal(t, 3, mgr.GetClientCount())

	mgr.Stop(context.Background())
}

func TestManager_GetSessionIDs(t *testing.T) {
	server, wsURL := createTestWebSocketServer(t, nil)
	defer server.Close()

	mgr := NewManager(Config{})

	for i := 0; i < 3; i++ {
		wsConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		require.NoError(t, err)
		err = mgr.AddSession(string(rune('a'+i)), wsConn)
		require.NoError(t, err)
	}

	ids := mgr.GetSessionIDs()
	assert.Len(t, ids, 3)

	mgr.Stop(context.Background())
}

func TestManager_SendTo_SessionNotFound(t *testing.T) {
	mgr := NewManager(Config{})

	msg := message.NewMessage("msg_001", "wechat", message.DirectionInbound, message.UserInfo{}, "test", message.MessageTypeText)
	err := mgr.SendTo(context.Background(), "nonexistent", msg)
	assert.Equal(t, ErrSessionNotFound, err)
}

func TestManager_Stop(t *testing.T) {
	server, wsURL := createTestWebSocketServer(t, nil)
	defer server.Close()

	mgr := NewManager(Config{})

	for i := 0; i < 3; i++ {
		wsConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		require.NoError(t, err)
		err = mgr.AddSession(string(rune('a'+i)), wsConn)
		require.NoError(t, err)
	}

	err := mgr.Stop(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, mgr.GetClientCount())
}

func TestManager_OnMessage(t *testing.T) {
	var receivedMsg *message.Message
	var receivedClientID string
	var mu sync.Mutex
	done := make(chan struct{})

	server, wsURL := createTestWebSocketServer(t, func(conn *websocket.Conn) {
		msg := message.NewMessage("msg_001", "wechat", message.DirectionInbound, message.UserInfo{}, "hello", message.MessageTypeText)
		data, _ := json.Marshal(msg)
		conn.WriteMessage(websocket.TextMessage, data)
		<-done
	})
	defer server.Close()

	mgr := NewManager(Config{
		OnMessage: func(clientID string, msg *message.Message) {
			mu.Lock()
			defer mu.Unlock()
			receivedClientID = clientID
			receivedMsg = msg
		},
	})

	wsConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	err = mgr.AddSession("session_001", wsConn)
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)
	close(done)

	mu.Lock()
	assert.Equal(t, "session_001", receivedClientID)
	assert.NotNil(t, receivedMsg)
	assert.Equal(t, "hello", receivedMsg.Content)
	mu.Unlock()

	mgr.Stop(context.Background())
}

func TestManager_SendTo(t *testing.T) {
	received := make([]byte, 0)
	var mu sync.Mutex

	server, wsURL := createTestWebSocketServer(t, func(conn *websocket.Conn) {
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			mu.Lock()
			received = data
			mu.Unlock()
			return
		}
	})
	defer server.Close()

	mgr := NewManager(Config{})

	wsConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	err = mgr.AddSession("session_001", wsConn)
	require.NoError(t, err)

	msg := message.NewMessage("msg_001", "wechat", message.DirectionInbound, message.UserInfo{}, "test", message.MessageTypeText)
	err = mgr.SendTo(context.Background(), "session_001", msg)
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	assert.NotNil(t, received)
	mu.Unlock()

	mgr.Stop(context.Background())
}

func TestManager_Broadcast(t *testing.T) {
	var mu sync.Mutex
	received := make(map[string]bool)

	server, wsURL := createTestWebSocketServer(t, func(conn *websocket.Conn) {
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			mu.Lock()
			received[string(data)] = true
			mu.Unlock()
			return
		}
	})
	defer server.Close()

	mgr := NewManager(Config{})

	for i := 0; i < 3; i++ {
		wsConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		require.NoError(t, err)
		err = mgr.AddSession(string(rune('a'+i)), wsConn)
		require.NoError(t, err)
	}

	msg := message.NewMessage("msg_001", "wechat", message.DirectionInbound, message.UserInfo{}, "test", message.MessageTypeText)
	err := mgr.Broadcast(context.Background(), msg)
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	assert.GreaterOrEqual(t, len(received), 1)
	mu.Unlock()

	mgr.Stop(context.Background())
}
