package gateway

import (
	"encoding/json"
	"os"
	"testing"
	"time"
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

func TestClient_ConnectAndReceiveConnected(t *testing.T) {
	env := setupTestServerW(t)
	defer env.cleanup()

	client := NewClient(WithClientAddr(testHostPortW(env.ts)))
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
	env := setupTestServerW(t)
	defer env.cleanup()

	client := NewClient(WithClientAddr(testHostPortW(env.ts)))
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

func TestClient_Send_TextRoundTrip(t *testing.T) {
	env := setupTestServerW(t)
	defer env.cleanup()

	var receivedMsg *Message
	handlerDone := make(chan struct{}, 1)
	env.gw.handler = func(msg *Message) {
		receivedMsg = msg
		handlerDone <- struct{}{}
	}

	client := NewClient(WithClientAddr(testHostPortW(env.ts)))
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect error: %v", err)
	}
	defer client.Close()

	time.Sleep(100 * time.Millisecond)
	if !client.IsConnected() {
		t.Fatal("client should be connected after Connect()")
	}

	err := client.Send(&Message{Data: []byte("hello from client")})
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
	env := setupTestServerW(t)
	defer env.cleanup()

	var receivedMsg *Message
	handlerDone := make(chan struct{}, 1)
	env.gw.handler = func(msg *Message) {
		receivedMsg = msg
		handlerDone <- struct{}{}
	}

	client := NewClient(WithClientAddr(testHostPortW(env.ts)))
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
	if receivedMsg.ContentType != "application/json" {
		t.Errorf("expected content type application/json, got %s", receivedMsg.ContentType)
	}
}

func TestClient_SendFile_RoundTrip(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "testfile-*.txt")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	testData := "file data content here"
	if _, err := tmpFile.WriteString(testData); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}
	tmpFile.Close()

	env := setupTestServerW(t)
	defer env.cleanup()

	var receivedMsg *Message
	handlerDone := make(chan struct{}, 1)
	env.gw.handler = func(msg *Message) {
		receivedMsg = msg
		handlerDone <- struct{}{}
	}

	client := NewClient(WithClientAddr(testHostPortW(env.ts)))
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect error: %v", err)
	}
	defer client.Close()

	time.Sleep(100 * time.Millisecond)
	if !client.IsConnected() {
		t.Fatal("client should be connected")
	}

	if err := client.SendFile(tmpFile.Name()); err != nil {
		t.Fatalf("SendFile error: %v", err)
	}

	select {
	case <-handlerDone:
	case <-time.After(5 * time.Second):
		t.Fatal("handler was not called for SendFile")
	}

	if string(receivedMsg.Data) != testData {
		t.Errorf("expected file data, got %s", string(receivedMsg.Data))
	}
	if receivedMsg.ContentType != "application/octet-stream" {
		t.Errorf("expected content type application/octet-stream, got %s", receivedMsg.ContentType)
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

func TestClient_BeginAndEndSession(t *testing.T) {
	env := setupTestServerW(t)
	defer env.cleanup()

	client := NewClient(WithClientAddr(testHostPortW(env.ts)))
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect error: %v", err)
	}
	defer client.Close()

	time.Sleep(100 * time.Millisecond)

	sessionID, err := client.BeginSession()
	if err != nil {
		t.Fatalf("BeginSession error: %v", err)
	}
	if sessionID == "" {
		t.Error("session ID should not be empty")
	}
	if client.SessionID() != sessionID {
		t.Errorf("client session ID mismatch")
	}

	if err := client.EndSession(); err != nil {
		t.Fatalf("EndSession error: %v", err)
	}
	if client.SessionID() != "" {
		t.Error("session ID should be cleared after EndSession")
	}
}
