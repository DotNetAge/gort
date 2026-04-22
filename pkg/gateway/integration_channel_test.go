package gateway

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/DotNetAge/gort/pkg/channel"
)

type mockIntegrationChannel struct {
	*channel.BaseChannel
	name        string
	channelType string
	messages    []*channel.Message
	mu          sync.Mutex
}

func newMockIntegrationChannel(name, chType string) *mockIntegrationChannel {
	base := &channel.BaseChannel{}
	return &mockIntegrationChannel{
		BaseChannel: base,
		name:        name,
		channelType: chType,
		messages:    make([]*channel.Message, 0),
	}
}

func (m *mockIntegrationChannel) Name() string { return m.name }
func (m *mockIntegrationChannel) Type() channel.ChannelType {
	return channel.ChannelType(m.channelType)
}
func (m *mockIntegrationChannel) IsRunning() bool {
	return m.BaseChannel.GetStatus() == channel.StatusRunning
}
func (m *mockIntegrationChannel) GetStatus() channel.Status { return m.BaseChannel.GetStatus() }

func (m *mockIntegrationChannel) Start(ctx context.Context, handler channel.MessageHandler) error {
	if handler != nil {
		m.SetHandler(handler)
	}
	m.SetStatus(channel.StatusRunning)
	return nil
}

func (m *mockIntegrationChannel) Stop(ctx context.Context) error {
	m.SetStatus(channel.StatusStopped)
	return nil
}

func (m *mockIntegrationChannel) SendMessage(ctx context.Context, msg *channel.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *msg
	m.messages = append(m.messages, &cp)
	return nil
}

func (m *mockIntegrationChannel) GetMessages() []*channel.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]*channel.Message, len(m.messages))
	copy(result, m.messages)
	return result
}

func (m *mockIntegrationChannel) SimulateInboundMessage(content string) {
	sender := m.GetGatewaySender()
	if sender != nil {
		sender.Broadcast(content)
	}
}

func TestRegisterChannel_AutoWiresGatewaySender(t *testing.T) {
	gw := New()

	ch := newMockIntegrationChannel("test-ch", "test")
	gw.RegisterChannel(ch)

	sender := ch.GetGatewaySender()
	if sender == nil {
		t.Fatal("GatewaySender should be set after RegisterChannel")
	}
	if sender != gw {
		t.Error("GatewaySender should be the Server instance")
	}
}

func TestRegisterChannel_MultipleChannels(t *testing.T) {
	gw := New(
		WithChannels(
			newMockIntegrationChannel("ch1", "type1"),
			newMockIntegrationChannel("ch2", "type2"),
			newMockIntegrationChannel("ch3", "type3"),
		),
	)

	channels := gw.Channels()
	if len(channels) != 3 {
		t.Errorf("expected 3 channels, got %d", len(channels))
	}

	for _, name := range []string{"ch1", "ch2", "ch3"} {
		ch := channels[name]
		if _, ok := ch.(*mockIntegrationChannel); !ok {
			t.Errorf("channel %s wrong type", name)
			continue
		}
		mockCh := ch.(*mockIntegrationChannel)
		if mockCh.GetGatewaySender() == nil {
			t.Errorf("channel %s missing GatewaySender", name)
		}
	}
}

func TestChannel_SendToGateway_Broadcast(t *testing.T) {
	env := setupTestServer(t)
	defer env.cleanup()

	ch := newMockIntegrationChannel("broadcaster", "test")
	env.gw.RegisterChannel(ch)

	conn := dialTestWS(t, env.ts)
	extractClientIDFromConn(t, conn)

	ch.SimulateInboundMessage("hello from channel")

	msgJSON := wsReadText(t, conn)
	var m Message
	json.Unmarshal([]byte(msgJSON), &m)
	if string(m.Data) != "hello from channel" {
		t.Errorf("expected 'hello from channel', got %s", string(m.Data))
	}
	if m.Direction != DirectionInbound {
		t.Errorf("expected inbound, got %s", m.Direction)
	}
}

func TestChannel_SendToGateway_SpecificClient(t *testing.T) {
	env := setupTestServer(t)
	defer env.cleanup()

	conn := dialTestWS(t, env.ts)
	clientID := extractClientIDFromConn(t, conn)

	ch := newMockIntegrationChannel("direct-sender", "test")
	env.gw.RegisterChannel(ch)

	ch.SendToGatewayClient(clientID, "targeted message")

	msgJSON := wsReadText(t, conn)
	var m Message
	json.Unmarshal([]byte(msgJSON), &m)
	if string(m.Data) != "targeted message" {
		t.Errorf("expected 'targeted message', got %s", string(m.Data))
	}
}

func TestStartAllChannels_Lifecycle(t *testing.T) {
	gw := New()
	ch1 := newMockIntegrationChannel("ch-start-1", "type1")
	ch2 := newMockIntegrationChannel("ch-start-2", "type2")

	gw.RegisterChannel(ch1)
	gw.RegisterChannel(ch2)

	gw.StartAllChannels(context.Background(), func(g *Server, msg *Message) {})

	if !ch1.IsRunning() {
		t.Error("ch1 should be running after StartAllChannels")
	}
	if !ch2.IsRunning() {
		t.Error("ch2 should be running after StartAllChannels")
	}

	gw.StopAllChannels(context.Background())

	time.Sleep(50 * time.Millisecond)
	if ch1.IsRunning() {
		t.Error("ch1 should be stopped after StopAllChannels")
	}
	if ch2.IsRunning() {
		t.Error("ch2 should be stopped after StopAllChannels")
	}
}

func TestStartAllChannels_WrapsHandler(t *testing.T) {
	env := setupTestServer(t)
	defer env.cleanup()

	var receivedMsg *Message
	handlerDone := make(chan struct{}, 1)

	ch := newMockIntegrationChannel("handler-test", "test")
	env.gw.RegisterChannel(ch)

	wrappedHandler := func(cctx context.Context, cmsg *channel.Message) error {
		gwMsg := FromChannelMessage(cmsg)
		gwMsg.ChannelID = ch.Name()
		env.gw.handler(env.gw, gwMsg)
		return nil
	}

	ch.SetHandler(wrappedHandler)
	ch.Start(context.Background(), nil)

	inboundMsg := &channel.Message{
		ID:        "ext-msg-001",
		Type:      channel.MessageTypeText,
		Direction: channel.DirectionInbound,
		Content:   "external platform message",
		From:      channel.UserInfo{ID: "user-123", Name: "Test User"},
		Timestamp: time.Now().Format(time.RFC3339),
	}

	env.gw.handler = func(g *Server, msg *Message) {
		receivedMsg = msg
		handlerDone <- struct{}{}
	}

	err := ch.HandleMessage(context.Background(), inboundMsg)
	if err != nil {
		t.Fatalf("HandleMessage error: %v", err)
	}

	select {
	case <-handlerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("handler was not called from channel message")
	}

	if receivedMsg == nil {
		t.Fatal("message should have been received")
	}
	if receivedMsg.ChannelID != "handler-test" {
		t.Errorf("expected ChannelID=handler-test, got %s", receivedMsg.ChannelID)
	}
	if string(receivedMsg.Data) != "external platform message" {
		t.Errorf("expected 'external platform message', got %s", string(receivedMsg.Data))
	}
	if receivedMsg.Direction != DirectionInbound {
		t.Errorf("expected inbound direction, got %s", receivedMsg.Direction)
	}
}

func TestStopAllChannels_NotStarted(t *testing.T) {
	gw := New()
	ch := newMockIntegrationChannel("never-started", "test")
	gw.RegisterChannel(ch)

	err := gw.StopAllChannels(context.Background())
	if err != nil {
		t.Errorf("StopAllChannels on non-running channels should not error, got %v", err)
	}
}

func TestSendToChannel_OutboundRouting(t *testing.T) {
	gw := New()
	ch := newMockIntegrationChannel("outbound-ch", "test")
	gw.RegisterChannel(ch)

	ctx := context.Background()
	msg := &Message{
		ID:        "gw-out-001",
		Data:      []byte("reply to external user"),
		Direction: DirectionOutbound,
		Timestamp: time.Now(),
	}

	err := gw.SendToChannel(ctx, "outbound-ch", msg)
	if err != nil {
		t.Fatalf("SendToChannel error: %v", err)
	}

	received := ch.GetMessages()
	if len(received) != 1 {
		t.Fatalf("expected 1 message in channel, got %d", len(received))
	}
	chMsg := received[0]
	if chMsg.Content != "reply to external user" {
		t.Errorf("expected content='reply to external user', got '%s'", chMsg.Content)
	}
	if chMsg.Direction != channel.DirectionOutbound {
		t.Errorf("expected outbound direction in channel msg, got %s", chMsg.Direction)
	}
}

func TestSendToChannel_UnknownChannel(t *testing.T) {
	gw := New()
	ctx := context.Background()
	msg := &Message{Data: []byte("test")}

	err := gw.SendToChannel(ctx, "nonexistent", msg)
	if err == nil {
		t.Error("expected error for unknown channel")
	}
}

func TestToChannelMessage_Conversion(t *testing.T) {
	now := time.Now()
	gwMsg := &Message{
		ID:          "gw-123",
		SessionID:   "sess-456",
		ClientID:    "client-789",
		Direction:   DirectionOutbound,
		Data:        []byte("binary payload"),
		ContentType: "file",
		Timestamp:   now,
	}

	chMsg := ToChannelMessage(gwMsg)

	if chMsg.ID != "gw-123" {
		t.Errorf("ID mismatch")
	}
	if chMsg.Content != "binary payload" {
		t.Errorf("Content mismatch: %s", chMsg.Content)
	}
	if chMsg.Type != channel.MessageTypeText {
		t.Errorf("Type mismatch: expected text, got %s", chMsg.Type)
	}
	if chMsg.Direction != channel.DirectionOutbound {
		t.Errorf("Direction mismatch")
	}

	metaSession := chMsg.Metadata["session_id"].(string)
	if metaSession != "sess-456" {
		t.Errorf("session_id metadata mismatch: %s", metaSession)
	}
}

func TestFromChannelMessage_Conversion(t *testing.T) {
	ts := time.Now().Format(time.RFC3339)
	chMsg := &channel.Message{
		ID:        "ch-001",
		Type:      channel.MessageTypeImage,
		Direction: channel.DirectionInbound,
		From:      channel.UserInfo{ID: "u1", Name: "Alice"},
		Content:   "image description",
		Data:      []byte{0x89, 0x50, 0x4E},
		Timestamp: ts,
		Metadata: map[string]interface{}{
			"custom_key": "custom_value",
		},
	}

	gwMsg := FromChannelMessage(chMsg)

	if gwMsg.ID != "ch-001" {
		t.Errorf("ID mismatch")
	}
	if gwMsg.ContentType != "image" {
		t.Errorf("ContentType mismatch: expected image, got %s", gwMsg.ContentType)
	}
	if gwMsg.Direction != DirectionInbound {
		t.Errorf("Direction mismatch")
	}
	expectedData := []byte{0x89, 0x50, 0x4E}
	if string(gwMsg.Data) != string(expectedData) {
		t.Errorf("Data mismatch: expected %x, got %x", expectedData, gwMsg.Data)
	}
}

func TestFromChannelMessage_WithBinaryData(t *testing.T) {
	binaryData := []byte{0x00, 0x01, 0x02, 0xFF}
	chMsg := &channel.Message{
		ID:        "bin-01",
		Content:   "",
		Data:      binaryData,
		Timestamp: time.Now().Format(time.RFC3339),
	}

	gwMsg := FromChannelMessage(chMsg)
	if string(gwMsg.Data) != string(binaryData) {
		t.Errorf("binary data mismatch")
	}
}

func TestFromChannelMessage_EmptyTimestamp(t *testing.T) {
	chMsg := &channel.Message{
		ID:      "no-ts",
		Content: "test",
	}

	gwMsg := FromChannelMessage(chMsg)
	if !gwMsg.Timestamp.IsZero() {
		t.Errorf("expected zero timestamp for empty input, got %v", gwMsg.Timestamp)
	}
}

func TestEndToEnd_ChannelToWebSocket(t *testing.T) {
	env := setupTestServer(t)
	defer env.cleanup()

	ch := newMockIntegrationChannel("e2e-source", "source")
	env.gw.RegisterChannel(ch)

	conn := dialTestWS(t, env.ts)
	extractClientIDFromConn(t, conn)

	ch.SimulateInboundMessage("ping from wechat")

	resp := wsReadJSON(t, conn)
	if string(resp.Data) != "ping from wechat" {
		t.Errorf("e2e: expected 'ping from wechat', got %s", string(resp.Data))
	}
}

func TestEndToEnd_WebSocketToChannel(t *testing.T) {
	env := setupTestServer(t)
	defer env.cleanup()

	ch := newMockIntegrationChannel("e2e-target", "target")
	env.gw.RegisterChannel(ch)

	conn := dialTestWS(t, env.ts)
	extractClientIDFromConn(t, conn)

	env.gw.handler = func(g *Server, msg *Message) {
		g.SendToChannel(context.Background(), "e2e-target", msg)
	}

	sessionID := doSessionStart(t, conn, 1)
	doData(t, conn, sessionID, 0, 1, "send to dingtalk")
	doSessionEnd(t, conn, sessionID)

	time.Sleep(200 * time.Millisecond)

	received := ch.GetMessages()
	if len(received) == 0 {
		t.Fatal("e2e: channel should have received the message")
	}
	if received[0].Content != "send to dingtalk" {
		t.Errorf("e2e: expected 'send to dingtalk', got '%s'", received[0].Content)
	}
}

func TestWithChannels_Option(t *testing.T) {
	gw := New(WithChannels(
		newMockIntegrationChannel("opt-ch1", "a"),
		newMockIntegrationChannel("opt-ch2", "b"),
	))

	channels := gw.Channels()
	if len(channels) != 2 {
		t.Errorf("expected 2 channels from WithChannels, got %d", len(channels))
	}
}

func TestChannel_ClientCountViaSender(t *testing.T) {
	env := setupTestServer(t)
	defer env.cleanup()

	ch := newMockIntegrationChannel("count-checker", "test")
	env.gw.RegisterChannel(ch)

	dialTestWS(t, env.ts)
	dialTestWS(t, env.ts)
	time.Sleep(100 * time.Millisecond)

	sender := ch.GetGatewaySender()
	if sender.ClientCount() != 2 {
		t.Errorf("expected 2 clients via sender, got %d", sender.ClientCount())
	}
}

func TestChannel_SendJSONViaGateway(t *testing.T) {
	env := setupTestServer(t)
	defer env.cleanup()

	ch := newMockIntegrationChannel("json-sender", "test")
	env.gw.RegisterChannel(ch)

	conn := dialTestWS(t, env.ts)
	extractClientIDFromConn(t, conn)

	payload := map[string]string{"event": "new_message"}
	if sender := ch.GetGatewaySender(); sender != nil {
		sender.BroadcastJSON(payload)
	}

	msgJSON := wsReadText(t, conn)
	var m Message
	json.Unmarshal([]byte(msgJSON), &m)
	if m.ContentType != "application/json" {
		t.Errorf("expected json content type, got %s", m.ContentType)
	}

	var decoded map[string]string
	json.Unmarshal(m.Data, &decoded)
	if decoded["event"] != "new_message" {
		t.Errorf("unexpected JSON: %v", decoded)
	}
}
