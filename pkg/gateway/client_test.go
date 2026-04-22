package gateway

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestNewClient_Defaults(t *testing.T) {
	c := NewClient()
	if c.addr != "localhost:8081" {
		t.Errorf("expected default addr localhost:8081, got %s", c.addr)
	}
	if c.path != "/ws" {
		t.Errorf("expected default path /ws, got %s", c.path)
	}
}

func TestNewClient_WithOptions(t *testing.T) {
	c := NewClient(WithClientPort(9090), WithClientPath("/api/ws"))
	if c.addr != "localhost:9090" {
		t.Errorf("expected addr localhost:9090, got %s", c.addr)
	}
	if c.path != "/api/ws" {
		t.Errorf("expected path /api/ws, got %s", c.path)
	}
}

func TestNewClient_WithClientAddrTakesPrecedence(t *testing.T) {
	c := NewClient(WithClientPort(9999), WithClientAddr("192.168.1.1:7777"))
	if c.addr != "192.168.1.1:7777" {
		t.Errorf("WithClientAddr should take precedence, got %s", c.addr)
	}
}

func TestClient_ConnectAndReceiveConnected(t *testing.T) {
	env := setupTestServer(t)

	client := NewClient(WithClientAddr(testHostPort(env.ts)))
	err := client.Connect()
	if err != nil {
		t.Fatalf("connect error: %v", err)
	}
	defer client.Close()

	time.Sleep(100 * time.Millisecond)
	if !client.IsConnected() {
		t.Error("client should be connected")
	}
}

func TestClient_Close(t *testing.T) {
	env := setupTestServer(t)

	client := NewClient(WithClientAddr(testHostPort(env.ts)))
	client.Connect()

	if err := client.Close(); err != nil {
		t.Fatalf("close error: %v", err)
	}

	if client.IsConnected() {
		t.Error("client should not be connected after close")
	}
}

func TestClient_Close_NotConnected(t *testing.T) {
	client := NewClient()
	if err := client.Close(); err != nil {
		t.Errorf("close of unconnected client should not error, got %v", err)
	}
}

func TestClient_OnMessageCallback(t *testing.T) {
	env := setupTestServer(t)

	var received *Message
	doneCh := make(chan struct{}, 1)
	var clientID string

	client := NewClient(WithClientAddr(testHostPort(env.ts)))
	client.OnMessage(func(msg *Message) {
		if msg.ClientID != "" && clientID == "" {
			clientID = msg.ClientID
		}
		if string(msg.Data) == "connected" {
			return
		}
		received = msg
		doneCh <- struct{}{}
	})

	if err := client.Connect(); err != nil {
		t.Fatalf("connect error: %v", err)
	}
	defer client.Close()

	time.Sleep(200 * time.Millisecond)

	env.gw.Send(clientID, "test callback")

	select {
	case <-doneCh:
	case <-time.After(2 * time.Second):
		t.Fatal("OnMessage callback was not called")
	}

	if received == nil {
		t.Fatal("message should be received")
	}
	if string(received.Data) != "test callback" {
		t.Errorf("expected 'test callback', got %s", string(received.Data))
	}
}

func TestClient_Send_TextRoundTrip(t *testing.T) {
	env := setupTestServer(t)

	var receivedMsg *Message
	handlerDone := make(chan struct{}, 1)
	env.gw.handler = func(g *Server, msg *Message) {
		receivedMsg = msg
		handlerDone <- struct{}{}
	}

	client := NewClient(WithClientAddr(testHostPort(env.ts)))
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect error: %v", err)
	}
	defer client.Close()

	time.Sleep(100 * time.Millisecond)
	if !client.IsConnected() {
		t.Fatal("client should be connected after Connect()")
	}

	err := client.Send("hello from client")
	if err != nil {
		t.Fatalf("Send error: %v", err)
	}

	select {
	case <-handlerDone:
	case <-time.After(5 * time.Second):
		t.Fatal("handler was not called for Send")
	}

	if string(receivedMsg.Data) != "hello from client" {
		t.Errorf("expected 'hello from client', got %s", string(receivedMsg.Data))
	}
	if receivedMsg.Direction != DirectionOutbound {
		t.Errorf("expected outbound, got %s", receivedMsg.Direction)
	}
}

func TestClient_SendJSON_RoundTrip(t *testing.T) {
	env := setupTestServer(t)

	var receivedMsg *Message
	handlerDone := make(chan struct{}, 1)
	env.gw.handler = func(g *Server, msg *Message) {
		receivedMsg = msg
		handlerDone <- struct{}{}
	}

	client := NewClient(WithClientAddr(testHostPort(env.ts)))
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect error: %v", err)
	}
	defer client.Close()

	time.Sleep(100 * time.Millisecond)
	if !client.IsConnected() {
		t.Fatal("client should be connected")
	}

	payload := map[string]interface{}{"action": "ping", "count": 42}
	if err := client.SendJSON(payload); err != nil {
		t.Fatalf("SendJSON error: %v", err)
	}

	select {
	case <-handlerDone:
	case <-time.After(5 * time.Second):
		t.Fatal("handler was not called for SendJSON")
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(receivedMsg.Data, &decoded); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if decoded["action"] != "ping" || decoded["count"].(float64) != 42 {
		t.Errorf("unexpected payload: %v", decoded)
	}
}

func TestClient_SendFile_RoundTrip(t *testing.T) {
	env := setupTestServer(t)

	var receivedMsg *Message
	handlerDone := make(chan struct{}, 1)
	env.gw.handler = func(g *Server, msg *Message) {
		receivedMsg = msg
		handlerDone <- struct{}{}
	}

	client := NewClient(WithClientAddr(testHostPort(env.ts)))
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect error: %v", err)
	}
	defer client.Close()

	time.Sleep(100 * time.Millisecond)
	if !client.IsConnected() {
		t.Fatal("client should be connected")
	}

	reader := strings.NewReader("file data content here")
	if err := client.SendFile(reader); err != nil {
		t.Fatalf("SendFile error: %v", err)
	}

	select {
	case <-handlerDone:
	case <-time.After(5 * time.Second):
		t.Fatal("handler was not called for SendFile")
	}

	if string(receivedMsg.Data) != "file data content here" {
		t.Errorf("expected file data, got %s", string(receivedMsg.Data))
	}
}

func TestClient_ConnectToInvalidAddress(t *testing.T) {
	client := NewClient(WithClientAddr("localhost:1"))
	err := client.Connect()
	if err == nil {
		t.Error("expected error connecting to invalid address")
	}
}

func TestClient_WriteCommand_NotConnected(t *testing.T) {
	client := NewClient()
	err := client.writeCommand("test")
	if err == nil {
		t.Error("expected error writing to disconnected client")
	}
}

func receiveClientID(t *testing.T, env *testEnv) string {
	conn := dialTestWS(t, env.ts)
	m := wsReadJSON(t, conn)
	return m.ClientID
}

type wsTestEnv struct {
	gw      *Server
	ts      *httptest.Server
	addr    string
	cleanup func()
}

func setupWSEnv(t *testing.T) *wsTestEnv {
	mux := http.NewServeMux()
	var gw *Server
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		gw.handleWS(w, r)
	})

	ts := httptest.NewServer(mux)
	addr := testHostPort(ts)
	gw = New(WithAddr(addr))

	go gw.Start()
	time.Sleep(50 * time.Millisecond)

	env := &wsTestEnv{gw: gw, ts: ts, addr: addr}
	env.cleanup = func() {
		gw.Shutdown(context.Background())
		ts.Close()
	}
	t.Cleanup(env.cleanup)
	return env
}

func dialRawWS(t *testing.T, ts *httptest.Server) *websocket.Conn {
	u := "ws://" + testHostPort(ts) + "/ws"
	header := http.Header{"Origin": {"http://" + testHostPort(ts)}}
	conn, _, err := websocket.DefaultDialer.Dial(u, header)
	if err != nil {
		t.Fatalf("dial error: %v", err)
	}
	return conn
}

func testHostPort(ts *httptest.Server) string {
	_, port, _ := net.SplitHostPort(ts.Listener.Addr().String())
	return "127.0.0.1:" + port
}
