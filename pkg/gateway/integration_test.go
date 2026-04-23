package gateway

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestIntegration_FullLifecycle(t *testing.T) {
	env := setupWSEnv(t)
	defer env.cleanup()

	var handlerMessages []*Message
	var mu sync.Mutex
	handlerDone := make(chan struct{}, 1)

	env.gw.handler = func(msg *Message) {
		mu.Lock()
		handlerMessages = append(handlerMessages, msg)
		mu.Unlock()
		handlerDone <- struct{}{}
	}

	conn := dialRawWS(t, env.ts)
	defer conn.Close()
	wsSkipFirst(t, conn)

	sid := doBEGN(t, conn)
	doTEXT(t, conn, sid, "hello world")
	doCLSE(t, conn, sid)

	select {
	case <-handlerDone:
	case <-time.After(3 * time.Second):
		t.Fatal("handler not called in integration test")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(handlerMessages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(handlerMessages))
	}
	msg := handlerMessages[0]
	if string(msg.Data) != "hello world" {
		t.Errorf("expected 'hello world', got '%s'", string(msg.Data))
	}
	if msg.SessionID != sid {
		t.Errorf("session ID mismatch")
	}
}

func TestIntegration_MultipleClients_Broadcast(t *testing.T) {
	env := setupWSEnv(t)
	defer env.cleanup()

	conn1 := dialRawWS(t, env.ts)
	conn2 := dialRawWS(t, env.ts)
	defer conn1.Close()
	defer conn2.Close()

	wsSkipFirst(t, conn1)
	wsSkipFirst(t, conn2)

	env.gw.Broadcast("to everyone!")

	m1 := wsReadJSON(t, conn1)
	m2 := wsReadJSON(t, conn2)

	if string(m1.Data) != "to everyone!" || string(m2.Data) != "to everyone!" {
		t.Errorf("broadcast mismatch: [%s] [%s]", string(m1.Data), string(m2.Data))
	}
}

func TestIntegration_ClientSend_ServerReceives(t *testing.T) {
	env := setupWSEnv(t)
	defer env.cleanup()

	var received *Message
	doneCh := make(chan struct{}, 1)
	env.gw.handler = func(msg *Message) {
		received = msg
		doneCh <- struct{}{}
	}

	client := NewClient(WithClientAddr(env.addr))
	client.Connect()
	defer client.Close()

	client.Send(&Message{Data: []byte("integration test message")})

	select {
	case <-doneCh:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for server to receive client message")
	}

	if string(received.Data) != "integration test message" {
		t.Errorf("message mismatch: got '%s'", string(received.Data))
	}
	if received.Direction != DirectionOutbound {
		t.Errorf("expected outbound, got %s", received.Direction)
	}
	if received.ContentType != "text/plain" {
		t.Errorf("expected text/plain, got %s", received.ContentType)
	}
}

func TestIntegration_ClientSendJSON_ServerReceives(t *testing.T) {
	env := setupWSEnv(t)
	defer env.cleanup()

	var received *Message
	doneCh := make(chan struct{}, 1)
	env.gw.handler = func(msg *Message) {
		received = msg
		doneCh <- struct{}{}
	}

	client := NewClient(WithClientAddr(env.addr))
	client.Connect()
	defer client.Close()

	client.SendJSON(map[string]string{"type": "test", "value": "123"})

	select {
	case <-doneCh:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}

	var decoded map[string]string
	json.Unmarshal(received.Data, &decoded)
	if decoded["type"] != "test" || decoded["value"] != "123" {
		t.Errorf("JSON payload mismatch: %v", decoded)
	}
	if received.ContentType != "application/json" {
		t.Errorf("expected application/json, got %s", received.ContentType)
	}
}

func TestIntegration_ServerSendsFile_ClientReceives(t *testing.T) {
	env := setupWSEnv(t)
	defer env.cleanup()

	conn := dialRawWS(t, env.ts)
	defer conn.Close()
	cid := extractClientIDFromFirstMessage(t, conn)

	tmpFile := t.TempDir() + "/data.bin"
	content := []byte{0x01, 0x02, 0x03, 0x04, 0xFF}
	writeTestFile(t, tmpFile, content)

	err := env.gw.SendFile(cid, tmpFile)
	if err != nil {
		t.Fatalf("SendFile error: %v", err)
	}

	m := wsReadJSON(t, conn)
	if string(m.Data) != string(content) {
		t.Errorf("file content mismatch: expected %x, got %x", content, m.Data)
	}
	if m.ContentType != "application/octet-stream" {
		t.Errorf("expected application/octet-stream, got %s", m.ContentType)
	}
}

func TestIntegration_MultipleSessions_SameClient(t *testing.T) {
	env := setupWSEnv(t)
	defer env.cleanup()

	var messages []*Message
	var mu sync.Mutex
	countCh := make(chan struct{}, 2)

	env.gw.handler = func(msg *Message) {
		mu.Lock()
		messages = append(messages, msg)
		mu.Unlock()
		countCh <- struct{}{}
	}

	conn := dialRawWS(t, env.ts)
	defer conn.Close()
	wsSkipFirst(t, conn)

	sid1 := doBEGN(t, conn)
	doTEXT(t, conn, sid1, "msg-1")
	doCLSE(t, conn, sid1)

	sid2 := doBEGN(t, conn)
	doTEXT(t, conn, sid2, "msg-2")
	doCLSE(t, conn, sid2)

	for i := 0; i < 2; i++ {
		select {
		case <-countCh:
		case <-time.After(3 * time.Second):
			t.Fatalf("timeout waiting for message %d/2", i+1)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}
	if messages[0].SessionID == messages[1].SessionID {
		t.Error("different sessions should have different IDs")
	}
}

func TestIntegration_ClientDisconnect_ServerCleansUp(t *testing.T) {
	env := setupWSEnv(t)
	defer env.cleanup()

	conn := dialRawWS(t, env.ts)

	time.Sleep(100 * time.Millisecond)
	if env.gw.ClientCount() != 1 {
		t.Fatalf("expected 1 client before disconnect, got %d", env.gw.ClientCount())
	}

	conn.Close()
	time.Sleep(500 * time.Millisecond)

	if env.gw.ClientCount() != 0 {
		t.Fatalf("expected 0 clients after disconnect, got %d", env.gw.ClientCount())
	}
}

func TestIntegration_LargePayload(t *testing.T) {
	env := setupWSEnv(t)
	defer env.cleanup()

	var received *Message
	doneCh := make(chan struct{}, 1)
	env.gw.handler = func(msg *Message) {
		received = msg
		doneCh <- struct{}{}
	}

	client := NewClient(WithClientAddr(env.addr))
	client.Connect()
	defer client.Close()

	largeData := strings.Repeat("x", 100*1024)
	client.Send(&Message{Data: []byte(largeData)})

	select {
	case <-doneCh:
	case <-time.After(60 * time.Second):
		t.Fatal("timeout for large payload")
	}

	if len(received.Data) != len(largeData) {
		t.Errorf("size mismatch: expected %d, got %d", len(largeData), len(received.Data))
	}
}
