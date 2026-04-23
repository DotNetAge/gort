package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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
	h := func(msg *Message) {}
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

func TestStartWS_Connect(t *testing.T) {
	env := setupTestServer(t)
	defer env.cleanup()

	conn := dialTestWS(t, env.ts)
	defer conn.Close()

	time.Sleep(100 * time.Millisecond)
	if env.gw.ClientCount() != 1 {
		t.Errorf("expected 1 client, got %d", env.gw.ClientCount())
	}
}

func TestBEGN_Command(t *testing.T) {
	env := setupTestServer(t)
	defer env.cleanup()

	conn := dialTestWS(t, env.ts)
	defer conn.Close()
	wsSkipFirst(t, conn)

	wsSend(conn, "BEGN|test-session-1|||")
	resp := wsReadText(t, conn)
	parts := strings.Split(resp, "|")
	if parts[0] != string(CmdBegn) || parts[2] != string(CmdOK) {
		t.Errorf("unexpected response: %s", resp)
	}
	if parts[1] == "" {
		t.Error("session ID should not be empty")
	}
}

func TestBEGN_DefaultTotalOne(t *testing.T) {
	env := setupTestServer(t)
	defer env.cleanup()

	conn := dialTestWS(t, env.ts)
	defer conn.Close()
	wsSkipFirst(t, conn)

	wsSend(conn, "BEGN|test-session-2|||")
	resp := wsReadText(t, conn)
	parts := strings.Split(resp, "|")
	if parts[0] != string(CmdBegn) || parts[2] != string(CmdOK) {
		t.Errorf("unexpected response: %s", resp)
	}
}

func TestFRAME_Command(t *testing.T) {
	env := setupTestServer(t)
	defer env.cleanup()

	conn := dialTestWS(t, env.ts)
	defer conn.Close()
	wsSkipFirst(t, conn)

	wsSend(conn, "BEGN|test-session-1|||")
	resp := wsReadText(t, conn)
	parts := strings.Split(resp, "|")
	sessionID := parts[1]

	wsSend(conn, fmt.Sprintf("FRAME|%s|%d|%d|%s", sessionID, 1, 3, "hello world"))
}

func TestFRAME_UnknownSession(t *testing.T) {
	env := setupTestServer(t)
	defer env.cleanup()

	conn := dialTestWS(t, env.ts)
	defer conn.Close()
	wsSkipFirst(t, conn)

	wsSend(conn, "FRAME|nonexistent-id|0|1|data")
	time.Sleep(100 * time.Millisecond)
}

func TestCLSE_CompleteTriggersHandler(t *testing.T) {
	env := setupTestServer(t)
	defer env.cleanup()

	var receivedMsg *Message
	var mu sync.Mutex
	handlerDone := make(chan struct{}, 1)

	env.gw.handler = func(msg *Message) {
		mu.Lock()
		receivedMsg = msg
		mu.Unlock()
		handlerDone <- struct{}{}
	}

	conn := dialTestWS(t, env.ts)
	defer conn.Close()
	wsSkipFirst(t, conn)

	wsSend(conn, "BEGN|test-session|||")
	resp := wsReadText(t, conn)
	parts := strings.Split(resp, "|")
	sessionID := parts[1]

	for i := 0; i < 3; i++ {
		wsSend(conn, fmt.Sprintf("FRAME|%s|%d|%d|%s", sessionID, i, 3, fmt.Sprintf("part-%d", i)))
	}

	wsSend(conn, fmt.Sprintf("CLSE|%s|||", sessionID))

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

func TestCLSE_IncompleteDoesNotTriggerHandler(t *testing.T) {
	env := setupTestServer(t)
	defer env.cleanup()

	handlerCalled := false
	env.gw.handler = func(msg *Message) {
		handlerCalled = true
	}

	conn := dialTestWS(t, env.ts)
	defer conn.Close()
	wsSkipFirst(t, conn)

	wsSend(conn, "BEGN|test-session|||")
	resp := wsReadText(t, conn)
	parts := strings.Split(resp, "|")
	sessionID := parts[1]

	wsSend(conn, fmt.Sprintf("FRAME|%s|%d|%d|%s", sessionID, 0, 3, "only-one-part"))
	time.Sleep(100 * time.Millisecond)
	wsSend(conn, fmt.Sprintf("CLSE|%s|||", sessionID))
	time.Sleep(300 * time.Millisecond)

	if handlerCalled {
		t.Error("handler should NOT be called for incomplete session")
	}
}

func TestUnknownCommand_Ignored(t *testing.T) {
	env := setupTestServer(t)
	defer env.cleanup()

	conn := dialTestWS(t, env.ts)
	defer conn.Close()

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
	defer conn.Close()
	cid := extractClientIDFromFirstMessage(t, conn)

	env.gw.Send(cid, "hello from server")

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
	defer conn1.Close()
	defer conn2.Close()

	extractClientIDFromFirstMessage(t, conn1)
	extractClientIDFromFirstMessage(t, conn2)

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
	defer conn.Close()
	clientID := extractClientIDFromFirstMessage(t, conn)

	if err := env.gw.SendFile(clientID, filePath); err != nil {
		t.Fatalf("SendFile error: %v", err)
	}

	m := wsReadJSON(t, conn)
	if string(m.Data) != "file content here" {
		t.Errorf("expected file content, got %s", string(m.Data))
	}
	if m.ContentType != "application/octet-stream" {
		t.Errorf("expected content type application/octet-stream, got %s", m.ContentType)
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
	defer conn.Close()
	clientID := extractClientIDFromFirstMessage(t, conn)

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
	defer conn1.Close()
	defer conn2.Close()

	extractClientIDFromFirstMessage(t, conn1)
	extractClientIDFromFirstMessage(t, conn2)

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
	extractClientIDFromFirstMessage(t, conn)

	wsSend(conn, "BEGN|test-session|||")
	resp := wsReadText(t, conn)
	sessionID := strings.Split(resp, "|")[1]
	wsSend(conn, fmt.Sprintf("TEXT|%s|%s", sessionID, "partial"))

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

	sess, err := sm.create("client-1")
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if sess.id == "" {
		t.Error("session id should not be empty")
	}
	if sess.clientID != "client-1" {
		t.Errorf("expected client-1, got %s", sess.clientID)
	}

	got, ok := sm.get(sess.id)
	if !ok {
		t.Fatal("should find created session")
	}
	if got.id != sess.id {
		t.Error("id mismatch")
	}
}

func TestSessionManager_AddFrame(t *testing.T) {
	sm := newSessionManager(30 * time.Second)
	defer sm.Close()

	sess, _ := sm.create("c1")
	err := sm.addFrame(sess.id, 0, 2, []byte("first"))
	if err != nil {
		t.Fatalf("addFrame failed: %v", err)
	}
	err = sm.addFrame(sess.id, 1, 2, []byte("second"))
	if err != nil {
		t.Fatalf("addFrame failed: %v", err)
	}

	err = sm.addFrame("nope", 0, 1, []byte("x"))
	if err != ErrSessionNotFound {
		t.Errorf("expected ErrSessionNotFound, got %v", err)
	}
}

func TestSessionManager_DuplicateFrameIndexIgnored(t *testing.T) {
	sm := newSessionManager(30 * time.Second)
	defer sm.Close()

	sess, _ := sm.create("c1")
	sm.addFrame(sess.id, 0, 2, []byte("original"))
	sm.addFrame(sess.id, 0, 2, []byte("duplicate"))

	result, complete := sm.assembleAndRemove(sess.id)
	if complete {
		t.Error("should NOT be complete (1 of 2 parts after duplicate)")
	}
	if result == nil {
		t.Fatal("session should be returned even when incomplete")
	}
	if len(result.pendingFrames) != 1 {
		t.Errorf("expected 1 frame, got %d", len(result.pendingFrames))
	}
}

func TestSessionManager_AssembleAndRemove(t *testing.T) {
	sm := newSessionManager(30 * time.Second)
	defer sm.Close()

	sess, _ := sm.create("c1")
	sm.addFrame(sess.id, 0, 2, []byte("part-0"))
	sm.addFrame(sess.id, 1, 2, []byte("part-1"))

	result, complete := sm.assembleAndRemove(sess.id)
	if !complete {
		t.Fatal("should be complete (2 of 2 parts)")
	}
	if result == nil {
		t.Fatal("session should be returned")
	}
	if len(result.messages) != 1 {
		t.Errorf("expected 1 assembled message, got %d", len(result.messages))
	}
	if string(result.messages[0].Data) != "part-0part-1" {
		t.Errorf("expected assembled data 'part-0part-1', got %s", string(result.messages[0].Data))
	}

	_, ok := sm.get(sess.id)
	if ok {
		t.Error("session should be removed after assembleAndRemove")
	}
}

func TestSessionManager_IncompleteFrameNotComplete(t *testing.T) {
	sm := newSessionManager(30 * time.Second)
	defer sm.Close()

	sess, _ := sm.create("c1")
	sm.addFrame(sess.id, 0, 3, []byte("part-0"))

	_, complete := sm.assembleAndRemove(sess.id)
	if complete {
		t.Error("should NOT be complete (1 of 3 parts)")
	}
}

func TestSessionManager_CleanupClient(t *testing.T) {
	sm := newSessionManager(30 * time.Second)
	defer sm.Close()

	sess1, _ := sm.create("client-A")
	sess2, _ := sm.create("client-B")
	sess3, _ := sm.create("client-A")

	sm.cleanupClient("client-A")

	_, ok := sm.get(sess1.id)
	if ok {
		t.Error("sess1 should be removed")
	}
	_, ok = sm.get(sess2.id)
	if !ok {
		t.Error("sess2 should remain")
	}
	_, ok = sm.get(sess3.id)
	if ok {
		t.Error("sess3 should be removed")
	}
}

func TestDispatchCommand_EmptyInput(t *testing.T) {
	g := New()
	g.dispatchCommand(&client{id: "test"}, "")
}

func TestHandleBEGN_TotalClamped(t *testing.T) {
	env := setupTestServer(t)
	defer env.cleanup()

	conn := dialTestWS(t, env.ts)
	defer conn.Close()
	wsSkipFirst(t, conn)

	wsSend(conn, "BEGN|test-session-3|||")
	resp := wsReadText(t, conn)
	parts := strings.Split(resp, "|")
	if parts[0] != string(CmdBegn) || parts[2] != string(CmdOK) {
		t.Errorf("should handle large total: %s", resp)
	}
}

func TestHandleFRAME_TooFewParts(t *testing.T) {
	env := setupTestServer(t)
	defer env.cleanup()

	conn := dialTestWS(t, env.ts)
	defer conn.Close()

	wsSend(conn, "FRAME|id|0|")
	time.Sleep(100 * time.Millisecond)
}

func TestHandleCLSE_MissingSessionID(t *testing.T) {
	env := setupTestServer(t)
	defer env.cleanup()

	conn := dialTestWS(t, env.ts)
	defer conn.Close()

	wsSend(conn, "CLSE||||")
	time.Sleep(100 * time.Millisecond)
}

func TestMultipleBEGN_SameClient(t *testing.T) {
	env := setupTestServer(t)
	defer env.cleanup()

	conn := dialTestWS(t, env.ts)
	defer conn.Close()
	wsSkipFirst(t, conn)

	wsSend(conn, "BEGN|session-1|||")
	resp1 := wsReadText(t, conn)
	wsSend(conn, "BEGN|session-2|||")
	resp2 := wsReadText(t, conn)

	sid1 := strings.Split(resp1, "|")[1]
	sid2 := strings.Split(resp2, "|")[1]

	if sid1 == sid2 {
		t.Error("sessions should have different IDs")
	}
}

func TestTEXT_SingleMessage(t *testing.T) {
	env := setupTestServer(t)
	defer env.cleanup()

	var received *Message
	done := make(chan struct{}, 1)
	env.gw.handler = func(msg *Message) {
		received = msg
		done <- struct{}{}
	}

	conn := dialTestWS(t, env.ts)
	defer conn.Close()
	wsSkipFirst(t, conn)

	wsSend(conn, "TEXT||hello world")
	resp := wsReadText(t, conn)
	if !strings.HasPrefix(resp, string(CmdOK)) {
		t.Errorf("expected OK response, got: %s", resp)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler not called")
	}

	if string(received.Data) != "hello world" {
		t.Errorf("expected 'hello world', got: %s", string(received.Data))
	}
	if received.ContentType != "text/plain" {
		t.Errorf("expected text/plain, got: %s", received.ContentType)
	}
}

func TestJSON_SingleMessage(t *testing.T) {
	env := setupTestServer(t)
	defer env.cleanup()

	var received *Message
	done := make(chan struct{}, 1)
	env.gw.handler = func(msg *Message) {
		received = msg
		done <- struct{}{}
	}

	conn := dialTestWS(t, env.ts)
	defer conn.Close()
	wsSkipFirst(t, conn)

	payload := `{"key":"value"}`
	wsSend(conn, "JSON||"+payload)
	resp := wsReadText(t, conn)
	if !strings.HasPrefix(resp, string(CmdOK)) {
		t.Errorf("expected OK response, got: %s", resp)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler not called")
	}

	if received.ContentType != "application/json" {
		t.Errorf("expected application/json, got: %s", received.ContentType)
	}
}

func TestFILE_SingleMessage(t *testing.T) {
	env := setupTestServer(t)
	defer env.cleanup()

	var received *Message
	done := make(chan struct{}, 1)
	env.gw.handler = func(msg *Message) {
		received = msg
		done <- struct{}{}
	}

	conn := dialTestWS(t, env.ts)
	defer conn.Close()
	wsSkipFirst(t, conn)

	wsSend(conn, "FILE||\x01\x02\x03\xff")
	resp := wsReadText(t, conn)
	if !strings.HasPrefix(resp, string(CmdOK)) {
		t.Errorf("expected OK response, got: %s", resp)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler not called")
	}

	if received.ContentType != "application/octet-stream" {
		t.Errorf("expected application/octet-stream, got: %s", received.ContentType)
	}
}
