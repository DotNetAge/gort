package gateway

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

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
	respCh    chan string
	waitMu    sync.RWMutex
	waiting   bool

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

func (c *Client) OnMessage(h MessageHandlerClient) {
	c.handler = h
}

func (c *Client) Send(message string) error {
	sessionID, err := c.startSession(1)
	if err != nil {
		return err
	}
	if err := c.sendData(sessionID, 0, 1, message); err != nil {
		return err
	}
	return c.endSession(sessionID)
}

func (c *Client) SendFile(r io.Reader) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	sessionID, err := c.startSession(1)
	if err != nil {
		return err
	}
	if err := c.sendData(sessionID, 0, 1, string(data)); err != nil {
		return err
	}
	return c.endSession(sessionID)
}

func (c *Client) SendJSON(v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.Send(string(data))
}

func (c *Client) startSession(total int) (string, error) {
	c.setWaiting(true)
	defer c.setWaiting(false)

	cmd := fmt.Sprintf("%s|||%d|", CmdSessionStart, total)
	if err := c.writeCommand(cmd); err != nil {
		return "", err
	}
	resp, err := c.waitResponse(5 * time.Second)
	if err != nil {
		return "", err
	}
	parts := strings.Split(resp, "|")
	if len(parts) >= 3 && parts[2] == string(CmdOK) {
		return parts[1], nil
	}
	return "", fmt.Errorf("session start failed: %s", resp)
}

func (c *Client) sendData(sessionID string, index, total int, data string) error {
	c.setWaiting(true)
	defer c.setWaiting(false)

	cmd := fmt.Sprintf("DATA|%s|%d|%d|%s", sessionID, index, total, data)
	if err := c.writeCommand(cmd); err != nil {
		return err
	}
	_, err := c.waitResponse(5 * time.Second)
	return err
}

func (c *Client) endSession(sessionID string) error {
	cmd := fmt.Sprintf("%s|%s|||", CmdSessionEnd, sessionID)
	return c.writeCommand(cmd)
}

func (c *Client) writeCommand(cmd string) error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.running || c.conn == nil {
		return fmt.Errorf("client not connected")
	}
	select {
	case c.send <- []byte(cmd):
		return nil
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
	defer c.Close()

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

		if typ == websocket.TextMessage && c.isWaiting() {
			text := string(raw)
			if strings.HasPrefix(text, string(CmdSessionStart)) ||
				strings.HasPrefix(text, string(CmdData)) ||
				strings.HasPrefix(text, string(CmdOK)) ||
				strings.HasPrefix(text, string(CmdSessionEnd)) {
				select {
				case c.respCh <- text:
				default:
				}
				continue
			}
		}

		var msg Message
		if err := json.Unmarshal(raw, &msg); err != nil {
			continue
		}

		if c.handler != nil {
			c.handler(&msg)
		}
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
