package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestNewServer_Defaults(t *testing.T) {
	s := New()
	if s.addr != ":8081" {
		t.Errorf("expected default addr :8081, got %s", s.addr)
	}
	if s.path != "/ws" {
		t.Errorf("expected default path /ws, got %s", s.path)
	}
	if s.sessionTimeout != 30*time.Minute {
		t.Errorf("expected default timeout 30m, got %v", s.sessionTimeout)
	}
	if s.handler != nil {
		t.Error("expected nil handler by default")
	}
}

func TestNewServer_WithOptions(t *testing.T) {
	h := func(g *Server, msg *Message) {}
	s := New(WithPort(9999), WithPath("/api/ws"), WithHandler(h), WithSessionTimeout(5*time.Minute))
	if s.addr != ":9999" {
		t.Errorf("expected addr :9999, got %s", s.addr)
	}
	if s.path != "/api/ws" {
		t.Errorf("expected path /api/ws, got %s", s.path)
	}
	if s.sessionTimeout != 5*time.Minute {
		t.Errorf("expected timeout 5m, got %v", s.sessionTimeout)
	}
	if s.handler == nil {
		t.Error("expected handler to be set")
	}
}

func TestNewServer_WithAddrTakesPrecedence(t *testing.T) {
	s := New(WithPort(9999), WithAddr("0.0.0.0:7777"))
	if s.addr != "0.0.0.0:7777" {
		t.Errorf("WithAddr should take precedence (applied last), got %s", s.addr)
	}
}

func TestStartAndShutdown(t *testing.T) {
	g := New(WithPort(0))

	errCh := make(chan error, 1)
	go func() {
		errCh <- g.Start()
	}()

	time.Sleep(50 * time.Millisecond)
	if !g.IsRunning() {
		t.Fatal("server should be running")
	}

	if err := g.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown error: %v", err)
	}

	select {
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			t.Fatalf("unexpected start error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("start did not return after shutdown")
	}

	if g.IsRunning() {
		t.Error("server should not be running after shutdown")
	}
}

func TestStart_AlreadyRunning(t *testing.T) {
	g := New(WithPort(0))
	go g.Start()
	time.Sleep(50 * time.Millisecond)

	err := g.Start()
	if err != ErrAlreadyRunning {
		t.Errorf("expected ErrAlreadyRunning, got %v", err)
	}

	g.Shutdown(context.Background())
}

func TestShutdown_NotRunning(t *testing.T) {
	g := New()
	err := g.Shutdown(context.Background())
	if err != ErrNotRunning {
		t.Errorf("expected ErrNotRunning, got %v", err)
	}
}

type testEnv struct {
	gw      *Server
	ts      *httptest.Server
	cleanup func()
}

func setupTestServer(t *testing.T) *testEnv {
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

	env := &testEnv{gw: gw, ts: ts}
	env.cleanup = func() {
		gw.Shutdown(context.Background())
		ts.Close()
	}
	t.Cleanup(env.cleanup)
	return env
}

func dialTestWS(t *testing.T, ts *httptest.Server) *websocket.Conn {
	u := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	header := http.Header{"Origin": {ts.URL}}
	conn, _, err := websocket.DefaultDialer.Dial(u, header)
	if err != nil {
		t.Fatalf("dial error: %v", err)
	}
	return conn
}

func wsSend(conn *websocket.Conn, text string) {
	conn.WriteMessage(websocket.TextMessage, []byte(text))
}

func wsReadText(t *testing.T, conn *websocket.Conn) string {
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	typ, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read error: %v", err)
	}
	if typ != websocket.TextMessage {
		t.Fatalf("expected TextMessage, got type %d", typ)
	}
	return string(msg)
}

func wsReadJSON(t *testing.T, conn *websocket.Conn) Message {
	raw := wsReadText(t, conn)
	var m Message
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("json unmarshal error: %v", err)
	}
	return m
}

func extractClientIDFromConn(t *testing.T, conn *websocket.Conn) string {
	m := wsReadJSON(t, conn)
	if m.ClientID == "" {
		t.Fatal("clientID should not be empty in connected message")
	}
	return m.ClientID
}

func doSessionStart(t *testing.T, conn *websocket.Conn, total int) string {
	wsSend(conn, fmt.Sprintf("SESSION_START|||%d||", total))
	resp := wsReadText(t, conn)
	parts := strings.Split(resp, "|")
	if len(parts) < 3 || parts[2] != "OK" {
		t.Fatalf("SESSION_START failed: %s", resp)
	}
	return parts[1]
}

func doData(t *testing.T, conn *websocket.Conn, sessionID string, index, total int, data string) {
	wsSend(conn, fmt.Sprintf("DATA|%s|%d|%d|%s", sessionID, index, total, data))
	resp := wsReadText(t, conn)
	if !strings.HasSuffix(resp, "OK||") {
		t.Fatalf("DATA ACK failed: %s", resp)
	}
}

func doSessionEnd(t *testing.T, conn *websocket.Conn, sessionID string) {
	wsSend(conn, fmt.Sprintf("SESSION_END|%s|||", sessionID))
	resp := wsReadText(t, conn)
	if !strings.Contains(resp, "OK") {
		t.Fatalf("SESSION_END failed: %s", resp)
	}
}

func TestHandleWS_ConnectAndReceiveConnectedMsg(t *testing.T) {
	env := setupTestServer(t)
	defer env.cleanup()

	conn := dialTestWS(t, env.ts)
	defer conn.Close()

	m := wsReadJSON(t, conn)
	if string(m.Data) != "connected" {
		t.Errorf("expected 'connected', got %s", string(m.Data))
	}
	if m.Direction != DirectionInbound {
		t.Errorf("expected inbound, got %s", m.Direction)
	}
	if m.ClientID == "" {
		t.Error("clientID should not be empty")
	}
}

func TestSessionStart_Command(t *testing.T) {
	env := setupTestServer(t)
	defer env.cleanup()

	conn := dialTestWS(t, env.ts)
	extractClientIDFromConn(t, conn)

	wsSend(conn, "SESSION_START|||3||")
	resp := wsReadText(t, conn)
	parts := strings.Split(resp, "|")
	if parts[0] != "SESSION_START" || parts[2] != "OK" {
		t.Errorf("unexpected response: %s", resp)
	}
	if parts[1] == "" {
		t.Error("session ID should not be empty")
	}
}

func TestSessionStart_DefaultTotalOne(t *testing.T) {
	env := setupTestServer(t)
	defer env.cleanup()

	conn := dialTestWS(t, env.ts)
	extractClientIDFromConn(t, conn)

	wsSend(conn, "SESSION_START||||")
	resp := wsReadText(t, conn)
	parts := strings.Split(resp, "|")
	if parts[0] != "SESSION_START" || parts[2] != "OK" {
		t.Errorf("unexpected response: %s", resp)
	}
}

func TestData_Command(t *testing.T) {
	env := setupTestServer(t)
	defer env.cleanup()

	conn := dialTestWS(t, env.ts)
	extractClientIDFromConn(t, conn)
	sessionID := doSessionStart(t, conn, 3)

	wsSend(conn, fmt.Sprintf("DATA|%s|1|3|hello world", sessionID))
	resp := wsReadText(t, conn)
	parts := strings.Split(resp, "|")
	if parts[0] != "DATA" || parts[2] != "1" || parts[3] != "OK" {
		t.Errorf("unexpected DATA response: %s", resp)
	}
}

func TestData_UnknownSession(t *testing.T) {
	env := setupTestServer(t)
	defer env.cleanup()

	conn := dialTestWS(t, env.ts)
	extractClientIDFromConn(t, conn)

	wsSend(conn, "DATA|nonexistent-id|0|1|data")

	conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	typ, _, err := conn.ReadMessage()
	if err == nil && typ == websocket.TextMessage {
		t.Error("unknown session should not get a text ACK response")
	}
}

func TestSessionEnd_CompleteTriggersHandler(t *testing.T) {
	env := setupTestServer(t)
	defer env.cleanup()

	var receivedMsg *Message
	var mu sync.Mutex
	handlerDone := make(chan struct{}, 1)

	env.gw.handler = func(g *Server, msg *Message) {
		mu.Lock()
		receivedMsg = msg
		mu.Unlock()
		handlerDone <- struct{}{}
	}

	conn := dialTestWS(t, env.ts)
	extractClientIDFromConn(t, conn)
	sessionID := doSessionStart(t, conn, 3)

	for i := 0; i < 3; i++ {
		doData(t, conn, sessionID, i, 3, fmt.Sprintf("part-%d", i))
	}

	doSessionEnd(t, conn, sessionID)

	select {
	case <-handlerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("handler was not called")
	}

	mu.Lock()
	defer mu.Unlock()
	if receivedMsg == nil {
		t.Fatal("message should not be nil")
	}
	if receivedMsg.SessionID != sessionID {
		t.Errorf("session ID mismatch: %s vs %s", receivedMsg.SessionID, sessionID)
	}
	if receivedMsg.Direction != DirectionOutbound {
		t.Errorf("expected outbound, got %s", receivedMsg.Direction)
	}
	text := string(receivedMsg.Data)
	if !strings.Contains(text, "part-0") || !strings.Contains(text, "part-1") || !strings.Contains(text, "part-2") {
		t.Errorf("assembled data missing parts, got: %s", text)
	}
}

func TestSessionEnd_IncompleteDoesNotTriggerHandler(t *testing.T) {
	env := setupTestServer(t)
	defer env.cleanup()

	handlerCalled := false
	env.gw.handler = func(g *Server, msg *Message) {
		handlerCalled = true
	}

	conn := dialTestWS(t, env.ts)
	extractClientIDFromConn(t, conn)
	sessionID := doSessionStart(t, conn, 3)

	doData(t, conn, sessionID, 0, 3, "only-one-part")

	wsSend(conn, fmt.Sprintf("SESSION_END|%s|||", sessionID))

	time.Sleep(300 * time.Millisecond)
	if handlerCalled {
		t.Error("handler should NOT be called for incomplete session")
	}
}

func TestUnknownCommand_Ignored(t *testing.T) {
	env := setupTestServer(t)
	defer env.cleanup()

	conn := dialTestWS(t, env.ts)
	extractClientIDFromConn(t, conn)

	wsSend(conn, "UNKNOWN_CMD|param|param|data")
	time.Sleep(100 * time.Millisecond)

	conn.SetWriteDeadline(time.Now().Add(time.Millisecond * 100))
	if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
		t.Error("connection should still be open after unknown command")
	}
}

func TestSend_ToSpecificClient(t *testing.T) {
	env := setupTestServer(t)
	defer env.cleanup()

	conn := dialTestWS(t, env.ts)
	clientID := extractClientIDFromConn(t, conn)

	env.gw.Send(clientID, "hello from server")

	m := wsReadJSON(t, conn)
	if string(m.Data) != "hello from server" {
		t.Errorf("expected 'hello from server', got %s", string(m.Data))
	}
	if m.Direction != DirectionInbound {
		t.Errorf("expected inbound, got %s", m.Direction)
	}
}

func TestSend_Broadcast(t *testing.T) {
	env := setupTestServer(t)
	defer env.cleanup()

	conn1 := dialTestWS(t, env.ts)
	conn2 := dialTestWS(t, env.ts)
	extractClientIDFromConn(t, conn1)
	extractClientIDFromConn(t, conn2)

	env.gw.Broadcast("broadcast msg")

	m1 := wsReadJSON(t, conn1)
	m2 := wsReadJSON(t, conn2)
	if string(m1.Data) != "broadcast msg" || string(m2.Data) != "broadcast msg" {
		t.Errorf("both should receive broadcast: [%s] [%s]", string(m1.Data), string(m2.Data))
	}
}

func TestSend_UnknownClient_SilentDrop(t *testing.T) {
	env := setupTestServer(t)
	defer env.cleanup()

	dialTestWS(t, env.ts)
	env.gw.Send("nonexistent-client-id", "test")
	time.Sleep(100 * time.Millisecond)
}

func TestSendFile(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := tmpDir + "/test.txt"
	os.WriteFile(filePath, []byte("file content here"), 0644)

	env := setupTestServer(t)
	defer env.cleanup()

	conn := dialTestWS(t, env.ts)
	clientID := extractClientIDFromConn(t, conn)

	if err := env.gw.SendFile(clientID, filePath); err != nil {
		t.Fatalf("SendFile error: %v", err)
	}

	m := wsReadJSON(t, conn)
	if string(m.Data) != "file content here" {
		t.Errorf("expected file content, got %s", string(m.Data))
	}
	if m.ContentType != "file" {
		t.Errorf("expected 'file', got %s", m.ContentType)
	}
}

func TestSendFile_NotFound(t *testing.T) {
	g := New()
	err := g.SendFile("any", "/nonexistent/file.txt")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestSendJSON(t *testing.T) {
	env := setupTestServer(t)
	defer env.cleanup()

	conn := dialTestWS(t, env.ts)
	clientID := extractClientIDFromConn(t, conn)

	payload := map[string]string{"status": "ok", "code": "200"}
	if err := env.gw.SendJSON(clientID, payload); err != nil {
		t.Fatalf("SendJSON error: %v", err)
	}

	m := wsReadJSON(t, conn)
	if m.ContentType != "application/json" {
		t.Errorf("expected 'application/json', got %s", m.ContentType)
	}

	var decoded map[string]string
	if err := json.Unmarshal(m.Data, &decoded); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if decoded["status"] != "ok" || decoded["code"] != "200" {
		t.Errorf("unexpected payload: %v", decoded)
	}
}

func TestBroadcastJSON(t *testing.T) {
	env := setupTestServer(t)
	defer env.cleanup()

	conn1 := dialTestWS(t, env.ts)
	conn2 := dialTestWS(t, env.ts)
	extractClientIDFromConn(t, conn1)
	extractClientIDFromConn(t, conn2)

	env.gw.BroadcastJSON(map[string]int{"count": 42})

	m1 := wsReadJSON(t, conn1)
	var decoded map[string]int
	json.Unmarshal(m1.Data, &decoded)
	if decoded["count"] != 42 {
		t.Errorf("expected count=42, got %v", decoded)
	}
}

func TestClientCount(t *testing.T) {
	env := setupTestServer(t)
	defer env.cleanup()

	if env.gw.ClientCount() != 0 {
		t.Errorf("expected 0 clients initially, got %d", env.gw.ClientCount())
	}

	dialTestWS(t, env.ts)
	time.Sleep(100 * time.Millisecond)
	if env.gw.ClientCount() != 1 {
		t.Errorf("expected 1 client, got %d", env.gw.ClientCount())
	}

	dialTestWS(t, env.ts)
	time.Sleep(100 * time.Millisecond)
	if env.gw.ClientCount() != 2 {
		t.Errorf("expected 2 clients, got %d", env.gw.ClientCount())
	}
}

func TestClientDisconnect_CleansUp(t *testing.T) {
	env := setupTestServer(t)
	defer env.cleanup()

	conn := dialTestWS(t, env.ts)
	extractClientIDFromConn(t, conn)
	sessionID := doSessionStart(t, conn, 2)
	doData(t, conn, sessionID, 0, 2, "partial")

	conn.Close()
	time.Sleep(300 * time.Millisecond)

	if env.gw.ClientCount() != 0 {
		t.Errorf("expected 0 after disconnect, got %d", env.gw.ClientCount())
	}
}

func TestMessage_Text(t *testing.T) {
	m := &Message{Data: []byte("hello")}
	if m.Text() != "hello" {
		t.Errorf("expected 'hello', got '%s'", m.Text())
	}
}

func TestMessage_Text_NilData(t *testing.T) {
	m := &Message{Data: nil}
	if m.Text() != "" {
		t.Errorf("expected '' for nil data, got '%s'", m.Text())
	}
}

func TestSessionManager_CreateAndGet(t *testing.T) {
	sm := newSessionManager(30 * time.Second)
	defer sm.Close()

	sess, _ := sm.create("client-1", 3)
	if sess.id == "" {
		t.Error("session id should not be empty")
	}
	if sess.clientID != "client-1" {
		t.Errorf("expected client-1, got %s", sess.clientID)
	}
	if sess.total != 3 {
		t.Errorf("expected total 3, got %d", sess.total)
	}

	got, ok := sm.get(sess.id)
	if !ok {
		t.Fatal("should find created session")
	}
	if got.id != sess.id {
		t.Error("id mismatch")
	}
}

func TestSessionManager_AddData(t *testing.T) {
	sm := newSessionManager(30 * time.Second)
	defer sm.Close()

	sess, _ := sm.create("c1", 2)
	sm.addData(sess.id, 0, []byte("first"))
	sm.addData(sess.id, 1, []byte("second"))

	if err := sm.addData("nope", 0, []byte("x")); err != ErrSessionNotFound {
		t.Errorf("expected ErrSessionNotFound, got %v", err)
	}
}

func TestSessionManager_DuplicateIndexIgnored(t *testing.T) {
	sm := newSessionManager(30 * time.Second)
	defer sm.Close()

	sess, _ := sm.create("c1", 2)
	sm.addData(sess.id, 0, []byte("original"))
	sm.addData(sess.id, 0, []byte("duplicate"))

	result, complete := sm.assembleAndRemove(sess.id)
	if complete {
		t.Error("should NOT be complete (1 of 2 parts after duplicate)")
	}
	if result == nil {
		t.Fatal("session should be returned even when incomplete")
	}
	if len(result.parts) != 1 {
		t.Errorf("expected 1 part, got %d", len(result.parts))
	}
}

func TestSessionManager_AssembleAndRemove(t *testing.T) {
	sm := newSessionManager(30 * time.Second)
	defer sm.Close()

	sess, _ := sm.create("c1", 2)
	sm.addData(sess.id, 0, []byte("a"))
	sm.addData(sess.id, 1, []byte("b"))

	result, complete := sm.assembleAndRemove(sess.id)
	if !complete {
		t.Fatal("should be complete")
	}
	assembled := string(result.parts[0]) + string(result.parts[1])
	if assembled != "ab" {
		t.Errorf("expected 'ab', got '%s'", assembled)
	}
	if _, ok := sm.get(sess.id); ok {
		t.Error("session should be removed")
	}
}

func TestSessionManager_IncompleteAssembly(t *testing.T) {
	sm := newSessionManager(30 * time.Second)
	defer sm.Close()

	sess, _ := sm.create("c1", 2)
	sm.addData(sess.id, 0, []byte("a"))

	_, complete := sm.assembleAndRemove(sess.id)
	if complete {
		t.Error("should NOT be complete")
	}
}

func TestSessionManager_CleanupClient(t *testing.T) {
	sm := newSessionManager(30 * time.Second)
	defer sm.Close()

	s1, _ := sm.create("client-a", 1)
	s2, _ := sm.create("client-b", 1)
	s3, _ := sm.create("client-a", 1)

	sm.cleanupClient("client-a")

	if _, ok := sm.get(s1.id); ok {
		t.Error("s1 should be cleaned")
	}
	if _, ok := sm.get(s3.id); ok {
		t.Error("s3 should be cleaned")
	}
	if _, ok := sm.get(s2.id); !ok {
		t.Error("s2 should still exist")
	}
}

func TestDispatchCommand_EmptyInput(t *testing.T) {
	g := New()
	g.dispatchCommand(&client{id: "test"}, "")
}

func TestHandleSessionStart_TotalClamped(t *testing.T) {
	env := setupTestServer(t)
	defer env.cleanup()

	conn := dialTestWS(t, env.ts)
	extractClientIDFromConn(t, conn)

	wsSend(conn, "SESSION_START|||99999||")
	resp := wsReadText(t, conn)
	parts := strings.Split(resp, "|")
	if parts[0] != "SESSION_START" || parts[2] != "OK" {
		t.Errorf("should handle large total: %s", resp)
	}
}

func TestHandleData_TooFewParts(t *testing.T) {
	env := setupTestServer(t)
	defer env.cleanup()

	conn := dialTestWS(t, env.ts)
	extractClientIDFromConn(t, conn)
	wsSend(conn, "DATA|id|0|")
	time.Sleep(100 * time.Millisecond)
}

func TestHandleSessionEnd_MissingSessionID(t *testing.T) {
	env := setupTestServer(t)
	defer env.cleanup()

	conn := dialTestWS(t, env.ts)
	extractClientIDFromConn(t, conn)
	wsSend(conn, "SESSION_END||||")
	time.Sleep(100 * time.Millisecond)
}

func TestMultipleSessions_SameClient(t *testing.T) {
	env := setupTestServer(t)
	defer env.cleanup()

	conn := dialTestWS(t, env.ts)
	extractClientIDFromConn(t, conn)

	sid1 := doSessionStart(t, conn, 1)
	sid2 := doSessionStart(t, conn, 1)

	if sid1 == sid2 {
		t.Error("sessions should have different IDs")
	}
}
