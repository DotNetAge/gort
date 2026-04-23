package gateway

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type ClientOption func(*Client)

func WithClientAddr(addr string) ClientOption {
	return func(c *Client) { c.addr = addr }
}

func WithClientPort(port int) ClientOption {
	return func(c *Client) { c.addr = fmt.Sprintf("localhost:%d", port) }
}

func WithClientPath(path string) ClientOption {
	return func(c *Client) { c.path = path }
}

type MessageHandlerClient func(msg *Message)

type Client struct {
	addr      string
	path      string
	handler   MessageHandlerClient
	conn      *websocket.Conn
	send      chan []byte
	mu        sync.RWMutex
	running   bool
	done      chan struct{}
	once      sync.Once
	sendMu    sync.Mutex
	respCh    chan string
	waitMu    sync.RWMutex
	waiting   bool
	sessionID string
	clientID  string

	reconnectCfg      *ReconnectConfig
	reconnectMu       sync.RWMutex
	reconnectAttempts int
	onReconnectFail   func(err error)

	stateChangeMu      sync.RWMutex
	stateChangeHandler func(oldState, newState ConnectionState)
}

func NewClient(opts ...ClientOption) *Client {
	c := &Client{
		addr:   "localhost:8081",
		path:   "/ws",
		send:   make(chan []byte, 256),
		done:   make(chan struct{}),
		respCh: make(chan string, 16),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (c *Client) Connect() error {
	return c.connect()
}

func (c *Client) connect() error {
	u := url.URL{Scheme: "ws", Host: c.addr, Path: c.path}
	header := http.Header{"Origin": {"ws://" + c.addr}}
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), header)
	if err != nil {
		return err
	}
	c.conn = conn
	c.mu.Lock()
	c.running = true
	c.mu.Unlock()

	go c.readPump()
	go c.writePump()
	return nil
}

func (c *Client) Close() error {
	c.once.Do(func() { close(c.done) })
	c.mu.Lock()
	c.running = false
	c.mu.Unlock()
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

func (c *Client) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.running
}

func (c *Client) OnReceived(h MessageHandlerClient) {
	c.handler = h
}

func (c *Client) Send(msg *Message) error {
	return c.sendMessage(msg)
}

// SendCommand sends a CMD protocol message and returns the raw response text.
// The caller is responsible for parsing the response.
func (c *Client) SendCommand(name, args string) (string, error) {
	payload := args
	protocolCmd := fmt.Sprintf("%s|%s|%s|||", CmdCmd, name, payload)
	return c.sendAndWaitForResponse(protocolCmd, 30*time.Second)
}

func (c *Client) SendJSON(v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	msg := &Message{
		ID:          uuid.New().String(),
		Direction:   DirectionOutbound,
		Data:        data,
		ContentType: "application/json",
		Timestamp:   time.Now(),
	}
	return c.sendMessage(msg)
}

func (c *Client) SendFile(filename string) error {
	f, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return err
	}
	msg := &Message{
		ID:          uuid.New().String(),
		Direction:   DirectionOutbound,
		Data:        data,
		ContentType: "application/octet-stream",
		Timestamp:   time.Now(),
	}
	return c.sendMessage(msg)
}

func (c *Client) sendMessage(msg *Message) error {
	var cmd string
	var payload string

	switch msg.ContentType {
	case "application/json":
		cmd = "JSON"
		payload = string(msg.Data)
	case "application/octet-stream":
		cmd = "FILE"
		payload = string(msg.Data)
	default:
		cmd = "TEXT"
		payload = string(msg.Data)
	}

	protocolCmd := fmt.Sprintf("%s|%s|%s|||", cmd, c.sessionID, payload)
	return c.sendAndWaitAck(protocolCmd, 30*time.Second)
}

func (c *Client) sendAndWaitAck(cmd string, timeout time.Duration) error {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()

	c.setWaiting(true)
	defer c.setWaiting(false)

	if err := c.writeCommand(cmd); err != nil {
		return err
	}

	resp, err := c.waitResponse(timeout)
	if err != nil {
		return err
	}

	return parseAckResponse(resp)
}

// sendAndWaitForResponse sends a command and waits for any response (not just ACK).
// Used for CMD protocol where the response is a JSON payload, not a simple ACK.
func (c *Client) sendAndWaitForResponse(cmd string, timeout time.Duration) (string, error) {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()

	c.setWaiting(true)
	defer c.setWaiting(false)

	if err := c.writeCommand(cmd); err != nil {
		return "", err
	}

	resp, err := c.waitResponse(timeout)
	if err != nil {
		return "", err
	}

	return resp, nil
}

func parseAckResponse(resp string) error {
	parts := strings.Split(resp, "|")
	if len(parts) < 1 {
		return fmt.Errorf("invalid response: %s", resp)
	}

	switch parts[0] {
	case "OK":
		return nil
	case "ERR":
		if len(parts) >= 2 {
			return fmt.Errorf("server error: %s", parts[1])
		}
		return fmt.Errorf("server error")
	default:
		return fmt.Errorf("unexpected response: %s", resp)
	}
}

func (c *Client) BeginSession() (string, error) {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()

	sessionID := uuid.New().String()
	cmd := fmt.Sprintf("BEGN|%s|||", sessionID)

	c.setWaiting(true)
	defer c.setWaiting(false)

	if err := c.writeCommand(cmd); err != nil {
		return "", err
	}

	resp, err := c.waitResponse(30 * time.Second)
	if err != nil {
		return "", err
	}

	parts := strings.Split(resp, "|")
	if len(parts) >= 3 && parts[2] == "OK" {
		c.sessionID = sessionID
		return sessionID, nil
	}

	if len(parts) >= 2 {
		return "", fmt.Errorf("failed to begin session: %s", parts[1])
	}
	return "", fmt.Errorf("failed to begin session")
}

func (c *Client) EndSession() error {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()

	if c.sessionID == "" {
		return nil
	}

	cmd := fmt.Sprintf("CLSE|%s|||", c.sessionID)

	c.setWaiting(true)
	defer c.setWaiting(false)

	if err := c.writeCommand(cmd); err != nil {
		return err
	}

	resp, err := c.waitResponse(30 * time.Second)
	if err != nil {
		return err
	}

	parts := strings.Split(resp, "|")
	if len(parts) >= 1 && parts[0] == "OK" {
		c.sessionID = ""
		return nil
	}

	if len(parts) >= 2 {
		return fmt.Errorf("failed to end session: %s", parts[1])
	}
	return fmt.Errorf("failed to end session")
}

func (c *Client) writeCommand(cmd string) error {
	c.mu.RLock()
	running := c.running
	conn := c.conn
	c.mu.RUnlock()

	if !running || conn == nil {
		return fmt.Errorf("client not connected")
	}
	select {
	case c.send <- []byte(cmd):
		return nil
	case <-c.done:
		return fmt.Errorf("client closed")
	default:
		return fmt.Errorf("send buffer full")
	}
}

func (c *Client) setWaiting(waiting bool) {
	c.waitMu.Lock()
	c.waiting = waiting
	c.waitMu.Unlock()
}

func (c *Client) isWaiting() bool {
	c.waitMu.RLock()
	defer c.waitMu.RUnlock()
	return c.waiting
}

func (c *Client) waitResponse(timeout time.Duration) (string, error) {
	select {
	case resp := <-c.respCh:
		return resp, nil
	case <-time.After(timeout):
		return "", fmt.Errorf("response timeout")
	case <-c.done:
		return "", fmt.Errorf("client closed")
	}
}

func (c *Client) readPump() {
	defer c.doReconnectIfNeeded()

	c.conn.SetReadLimit(10 << 20)
	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		select {
		case <-c.done:
			return
		default:
		}

		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		typ, raw, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				slog.Error("client read error", "error", err)
			}
			return
		}

		// Server writePump may batch multiple protocol messages into a single
		// WebSocket frame separated by newlines. Split them and process each one.
		lines := strings.Split(string(raw), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			c.processMessage(typ, []byte(line))
		}
	}
}

func (c *Client) processMessage(typ int, raw []byte) {
	if typ != websocket.TextMessage {
		return
	}

	text := string(raw)
	waiting := c.isWaiting()

	// When waiting for a response, check for CMD JSON responses first.
	// CMD responses are JSON: {"cmd":"CMD","name":"...","data":...}
	if waiting && strings.HasPrefix(text, "{") {
		var cmdResp commandResponse
		if json.Unmarshal(raw, &cmdResp) == nil && cmdResp.Cmd == "CMD" {
			select {
			case c.respCh <- text:
			default:
				slog.Warn("response channel full, dropping CMD response", "name", cmdResp.Name)
			}
			return
		}
	}

	// Check for pipe-format ACK/ERR responses.
	if waiting {
		parts := strings.Split(text, "|")
		if len(parts) >= 3 && (parts[2] == "OK" || parts[2] == "ERR") {
			select {
			case c.respCh <- text:
			default:
				slog.Warn("response channel full, dropping ACK response", "response", text)
			}
			return
		}
		if len(parts) >= 1 && (parts[0] == "OK" || parts[0] == "ERR") {
			select {
			case c.respCh <- text:
			default:
				slog.Warn("response channel full, dropping ACK response", "response", text)
			}
			return
		}
	}

	// Regular Message handling.
	var msg Message
	if err := json.Unmarshal(raw, &msg); err != nil {
		return
	}

	if c.clientID == "" && msg.ClientID != "" {
		c.clientID = msg.ClientID
	}

	if c.handler != nil {
		c.handler(&msg)
	}
}

func (c *Client) doReconnectIfNeeded() {
	c.reconnectMu.RLock()
	cfg := c.reconnectCfg
	c.reconnectMu.RUnlock()

	if cfg == nil || !cfg.Enabled {
		return
	}

	c.notifyStateChange(StateConnected, StateReconnecting)

	var lastErr error
	for attempt := 0; attempt < cfg.MaxRetries; attempt++ {
		c.reconnectMu.Lock()
		c.reconnectAttempts = attempt + 1
		c.reconnectMu.Unlock()

		delay := cfg.calcDelay(attempt)
		slog.Info("attempting reconnect", "attempt", attempt+1, "max", cfg.MaxRetries, "delay", delay)

		select {
		case <-time.After(delay):
		case <-c.done:
			c.notifyStateChange(StateReconnecting, StateDisconnected)
			return
		}

		c.mu.Lock()
		c.running = false
		c.mu.Unlock()

		err := c.connect()
		if err == nil {
			c.reconnectMu.Lock()
			c.reconnectAttempts = 0
			c.reconnectMu.Unlock()
			c.notifyStateChange(StateReconnecting, StateConnected)
			slog.Info("reconnected successfully", "attempt", attempt+1)
			if cfg.OnConnected != nil {
				cfg.OnConnected()
			}
			return
		}
		lastErr = err
		slog.Warn("reconnect attempt failed", "attempt", attempt+1, "error", err)
	}

	c.notifyStateChange(StateReconnecting, StateDisconnected)
	c.reconnectMu.RLock()
	onFail := c.onReconnectFail
	c.reconnectMu.RUnlock()
	if onFail != nil {
		onFail(fmt.Errorf("all %d reconnect attempts failed, last error: %w", cfg.MaxRetries, lastErr))
	}
}

func (c *Client) notifyStateChange(oldState, newState ConnectionState) {
	c.stateChangeMu.RLock()
	fn := c.stateChangeHandler
	c.stateChangeMu.RUnlock()
	if fn != nil {
		go fn(oldState, newState)
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(54 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case data, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, nil)
				return
			}
			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(data)
			for i := 0; i < len(c.send); i++ {
				w.Write([]byte{'\n'})
				w.Write(<-c.send)
			}
			w.Close()
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if c.conn.WriteMessage(websocket.PingMessage, nil) != nil {
				return
			}
		case <-c.done:
			return
		}
	}
}

func (c *Client) ClientID() string {
	return c.clientID
}

func (c *Client) SessionID() string {
	return c.sessionID
}

func (c *Client) SendBatch(msgs []*Message) error {
	for _, msg := range msgs {
		if err := c.Send(msg); err != nil {
			return err
		}
	}
	return nil
}
