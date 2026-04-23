package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/DotNetAge/gort/pkg/channel"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type WSConfig struct {
	AllowedOrigins []string
	AllowAllInDev  bool
}

func matchOrigin(origin, pattern string) bool {
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(origin, prefix)
	}
	return origin == pattern
}

func defaultWSConfig() *WSConfig {
	return &WSConfig{
		AllowedOrigins: []string{
			"http://localhost:*",
			"http://127.0.0.1:*",
			"ws://localhost:*",
			"ws://127.0.0.1:*",
		},
		AllowAllInDev: true,
	}
}

func WithWSConfig(cfg *WSConfig) Option {
	return func(s *Server) { s.wsConfig = cfg }
}

func newUpgrader(cfg *WSConfig) *websocket.Upgrader {
	if cfg == nil {
		cfg = defaultWSConfig()
	}
	return &websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			origin := r.Header.Get("Origin")
			if cfg.AllowAllInDev && os.Getenv("GO_ENV") == "development" {
				return true
			}
			for _, allowed := range cfg.AllowedOrigins {
				if matchOrigin(origin, allowed) {
					return true
				}
			}
			slog.Warn("rejected WebSocket connection", "origin", origin)
			return false
		},
	}
}

type Option func(*Server)

func WithAddr(addr string) Option {
	return func(s *Server) { s.addr = addr }
}

func WithPort(port int) Option {
	return func(s *Server) { s.addr = fmt.Sprintf(":%d", port) }
}

func WithPath(path string) Option {
	return func(s *Server) { s.path = path }
}

func WithHandler(h MessageHandler) Option {
	return func(s *Server) { s.handler = h }
}

func WithSessionTimeout(d time.Duration) Option {
	return func(s *Server) { s.sessionTimeout = d }
}

type MessageHandler func(msg *Message)

type client struct {
	id       string
	send     chan []byte
	conn     *websocket.Conn
	ctx      context.Context
	done     context.CancelFunc
	sessions *SessionManager
}

type Server struct {
	addr           string
	path           string
	handler        MessageHandler
	sessionTimeout time.Duration
	heartbeatCfg   *HeartbeatConfig
	hbMonitor      *HeartbeatMonitor
	wsConfig       *WSConfig
	upgrader       *websocket.Upgrader
	cmdRegistry    *CommandRegistry

	mu       sync.RWMutex
	server   *http.Server
	running  bool
	clients  map[string]*client
	register chan *client
	channels sync.Map
}

func New(opts ...Option) *Server {
	s := &Server{
		addr:           ":8081",
		path:           "/ws",
		sessionTimeout: 30 * time.Minute,
		clients:        make(map[string]*client),
		register:       make(chan *client),
		cmdRegistry:    NewCommandRegistry(),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *Server) Start() error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return ErrAlreadyRunning
	}
	s.running = true
	s.mu.Unlock()

	s.upgrader = newUpgrader(s.wsConfig)

	go s.hubLoop()

	mux := http.NewServeMux()
	mux.HandleFunc(s.path, s.handleWS)
	s.server = &http.Server{Addr: s.addr, Handler: mux}

	return s.server.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return ErrNotRunning
	}
	s.running = false
	s.mu.Unlock()

	close(s.register)

	for _, c := range s.clients {
		c.sessions.Close()
		c.sessions.cleanupClient(c.id)
	}

	if s.server != nil {
		shutdownCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		return s.server.Shutdown(shutdownCtx)
	}
	return nil
}

func (s *Server) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

func (s *Server) hubLoop() {
	for c := range s.register {
		s.mu.RLock()
		if !s.running {
			s.mu.RUnlock()
			return
		}
		s.mu.RUnlock()

		s.mu.Lock()
		s.clients[c.id] = c
		s.mu.Unlock()

		if s.hbMonitor != nil {
			s.hbMonitor.setConnectionCount(len(s.clients))
			s.hbMonitor.notifyStateChange(c.id, StateDisconnected.String(), StateConnected.String())
		}

		slog.Info("client connected", "id", c.id, "total", len(s.clients))
	}
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("ws upgrade failed", "error", err)
		return
	}

	ctx, done := context.WithCancel(context.Background())
	c := &client{
		id:       uuid.New().String(),
		send:     make(chan []byte, 256),
		conn:     conn,
		ctx:      ctx,
		done:     done,
		sessions: newSessionManager(s.sessionTimeout),
	}

	s.register <- c
	go s.writePump(c)
	go s.readPump(c)

	msg := &Message{
		ID:        uuid.New().String(),
		ClientID:  c.id,
		Direction: DirectionInbound,
		Timestamp: time.Now(),
	}
	if data, err := json.Marshal(msg); err == nil {
		conn.WriteMessage(websocket.TextMessage, data)
	}
}

func (s *Server) readPump(c *client) {
	defer func() {
		c.done()
		c.sessions.Close()
		c.sessions.cleanupClient(c.id)
		s.removeClient(c.id)
		c.conn.Close()
		if s.hbMonitor != nil {
			s.hbMonitor.setConnectionCount(s.ClientCount())
			s.hbMonitor.notifyTimeout(c.id)
		}
	}()

	c.conn.SetReadLimit(10 << 20)
	readTimeout := 60 * time.Second
	if s.heartbeatCfg != nil {
		readTimeout = s.heartbeatCfg.ReadTimeout
	}
	c.conn.SetReadDeadline(time.Now().Add(readTimeout))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(readTimeout))
		if s.hbMonitor != nil {
			s.hbMonitor.recordPong()
		}
		return nil
	})

	for {
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				slog.Error("read error", "id", c.id, "error", err)
			}
			return
		}

		// Client writePump may batch multiple protocol messages into a single
		// WebSocket frame separated by newlines. Split them and process each one.
		lines := strings.Split(string(raw), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			s.dispatchCommand(c, line)
		}
	}
}

func (s *Server) dispatchCommand(c *client, text string) {
	if len(text) > MaxMessageSize {
		slog.Warn("message exceeds max size", "id", c.id, "size", len(text))
		return
	}

	trimmed := strings.TrimSpace(text)
	if strings.HasPrefix(trimmed, "{") {
		s.handleJSONMessage(c, trimmed)
		return
	}

	parts := strings.SplitN(trimmed, "|", 5)
	if len(parts) < 1 {
		return
	}

	cmd := Command(parts[0])
	if !validCommands[cmd] {
		slog.Warn("unknown command rejected", "id", c.id, "cmd", cmd)
		return
	}

	switch cmd {
	case CmdBegn:
		s.handleBegn(c, parts)
	case CmdText:
		s.handleText(c, parts)
	case CmdJSON:
		s.handleJSON(c, parts)
	case CmdFile:
		s.handleFile(c, parts)
	case CmdFrame:
		s.handleFrame(c, parts)
	case CmdClse:
		s.handleClse(c, parts)
	case CmdCmd:
		s.handleCommand(c, parts)
	}
}

func (s *Server) handleJSONMessage(c *client, text string) {
	var proto ProtocolMessage
	if err := json.Unmarshal([]byte(text), &proto); err != nil {
		slog.Warn("invalid JSON message", "id", c.id, "error", err)
		return
	}

	msg := &Message{
		ID:          proto.ID,
		ClientID:    c.id,
		SessionID:   proto.SessID,
		Direction:   DirectionOutbound,
		Data:        []byte(proto.Data),
		ContentType: "application/octet-stream",
		Timestamp:   time.Now(),
	}

	switch proto.Cmd {
	case CmdText, CmdJSON, CmdFile:
		if proto.Cmd == CmdJSON {
			msg.ContentType = "application/json"
		}
		if s.handler != nil {
			s.handler(msg)
		}
		s.sendOK(c)
	case CmdBegn, CmdFrame, CmdClse:
		slog.Warn("JSON format not supported for session commands", "id", c.id, "cmd", proto.Cmd)
		s.sendErr(c, "session commands must use pipe format")
	default:
		slog.Warn("unknown JSON command", "id", c.id, "cmd", proto.Cmd)
	}
}

func (s *Server) handleBegn(c *client, parts []string) {
	sessionID := ""
	if len(parts) >= 2 {
		sessionID = parts[1]
	}

	var sess *session
	var err error

	if sessionID != "" {
		sess, err = c.sessions.createWithID(c.id, sessionID)
		if err != nil {
			slog.Warn("session create with id failed", "id", c.id, "session", sessionID, "error", err)
			s.sendErr(c, err.Error())
			return
		}
	} else {
		sess, err = c.sessions.create(c.id)
		if err != nil {
			slog.Warn("session create failed", "id", c.id, "error", err)
			s.sendErr(c, err.Error())
			return
		}
	}
	resp := fmt.Sprintf("%s|%s|%s||", CmdBegn, sess.id, CmdOK)
	s.sendText(c, resp)
}

func (s *Server) handleText(c *client, parts []string) {
	s.handleContentCommand(c, parts, "text/plain")
}

func (s *Server) handleJSON(c *client, parts []string) {
	s.handleContentCommand(c, parts, "application/json")
}

func (s *Server) handleFile(c *client, parts []string) {
	s.handleContentCommand(c, parts, "application/octet-stream")
}

func (s *Server) handleContentCommand(c *client, parts []string, contentType string) {
	if len(parts) < 2 {
		return
	}
	sessionID := parts[1]
	data := ""
	if len(parts) >= 3 {
		data = parts[2]
	}

	msg := &Message{
		ID:          uuid.New().String(),
		ClientID:     c.id,
		SessionID:    sessionID,
		Direction:    DirectionOutbound,
		Data:         []byte(data),
		ContentType:  contentType,
		Timestamp:    time.Now(),
	}

	if sessionID != "" {
		if err := c.sessions.addMessage(sessionID, msg); err != nil {
			slog.Warn("add message to session failed", "id", c.id, "session", sessionID)
			s.sendErr(c, err.Error())
			return
		}
	} else {
		if s.handler != nil {
			s.handler(msg)
		}
	}
	s.sendOK(c)
}

func (s *Server) handleFrame(c *client, parts []string) {
	if len(parts) < 5 {
		return
	}
	sessionID := parts[1]
	if sessionID == "" {
		slog.Warn("frame without session id", "id", c.id)
		return
	}
	var index, total int
	fmt.Sscanf(parts[2], "%d", &index)
	fmt.Sscanf(parts[3], "%d", &total)
	data := parts[4]

	if err := c.sessions.addFrame(sessionID, index, total, []byte(data)); err != nil {
		slog.Warn("frame add failed", "id", c.id, "session", sessionID, "error", err)
		return
	}
}

func (s *Server) handleClse(c *client, parts []string) {
	if len(parts) < 2 || parts[1] == "" {
		return
	}
	sessionID := parts[1]

	sess, _ := c.sessions.assembleAndRemove(sessionID)
	if sess == nil {
		slog.Warn("session not found", "id", c.id, "session", sessionID)
		return
	}

	if s.handler != nil {
		for _, msg := range sess.messages {
			msg.ClientID = c.id
			s.handler(msg)
		}
	}
	s.sendOK(c)
}

func (s *Server) sendOK(c *client) {
	s.sendText(c, fmt.Sprintf("%s|||", CmdOK))
}

func (s *Server) sendErr(c *client, reason string) {
	s.sendText(c, fmt.Sprintf("%s|%s|||", CmdErr, reason))
}

func (s *Server) sendText(c *client, text string) {
	select {
	case c.send <- []byte(text):
	default:
		slog.Warn("send buffer full, dropping message", "client_id", c.id, "message_len", len(text))
	}
}

func (s *Server) writePump(c *client) {
	pingPeriod := 54 * time.Second
	if s.heartbeatCfg != nil {
		pingPeriod = s.heartbeatCfg.PingPeriod
	}
	ticker := time.NewTicker(pingPeriod)
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
			if s.hbMonitor != nil {
				s.hbMonitor.recordPing()
			}
			if c.conn.WriteMessage(websocket.PingMessage, nil) != nil {
				return
			}
		case <-c.ctx.Done():
			return
		}
	}
}

func (s *Server) removeClient(id string) {
	s.mu.Lock()
	if c, ok := s.clients[id]; ok {
		delete(s.clients, id)
		close(c.send)
	}
	s.mu.Unlock()
	slog.Info("client disconnected", "id", id, "total", len(s.clients))
}

func (s *Server) Send(to string, message string) {
	msg := &Message{
		ID:          uuid.New().String(),
		Direction:   DirectionInbound,
		Data:        []byte(message),
		ContentType: "text/plain",
		Timestamp:   time.Now(),
	}
	s.sendMessage(to, msg)
}

func (s *Server) Broadcast(message string) {
	s.Send("*", message)
}

func (s *Server) SendFile(to string, filename string) error {
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
		Direction:   DirectionInbound,
		Data:        data,
		ContentType: "application/octet-stream",
		Timestamp:   time.Now(),
	}
	s.sendMessage(to, msg)
	return nil
}

func (s *Server) BroadcastFile(filename string) error {
	return s.SendFile("*", filename)
}

func (s *Server) SendJSON(to string, v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	msg := &Message{
		ID:          uuid.New().String(),
		Direction:   DirectionInbound,
		Data:        data,
		ContentType: "application/json",
		Timestamp:   time.Now(),
	}
	s.sendMessage(to, msg)
	return nil
}

func (s *Server) BroadcastJSON(v interface{}) error {
	return s.SendJSON("*", v)
}

func (s *Server) SendBatch(to string, msgs []*Message) {
	for _, msg := range msgs {
		s.sendMessage(to, msg)
	}
}

func (s *Server) BroadcastBatch(msgs []*Message) {
	s.SendBatch("*", msgs)
}

func (s *Server) sendMessage(to string, msg *Message) {
	data, err := json.Marshal(msg)
	if err != nil {
		slog.Error("failed to marshal message", "id", msg.ID, "error", err)
		return
	}

	if to == "" || to == "*" {
		s.mu.RLock()
		defer s.mu.RUnlock()
		for _, c := range s.clients {
			select {
			case c.send <- data:
			default:
				slog.Warn("send buffer full, dropping message", "client_id", c.id, "message_id", msg.ID)
			}
		}
		return
	}

	s.mu.RLock()
	c, ok := s.clients[to]
	s.mu.RUnlock()
	if !ok {
		slog.Warn("client not found for send", "to", to)
		return
	}
	select {
	case c.send <- data:
	default:
		slog.Warn("send buffer full, dropping message", "client_id", to, "message_id", msg.ID)
	}
}

func (s *Server) ClientCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.clients)
}

func (s *Server) RegisterChannel(ch channel.Channel) {
	if gwSender, ok := ch.(interface{ SetGatewaySender(channel.GatewaySender) }); ok {
		gwSender.SetGatewaySender(s)
	}
	s.channels.Store(ch.Name(), ch)
}

func (s *Server) GetChannel(name string) (channel.Channel, bool) {
	v, ok := s.channels.Load(name)
	if !ok {
		return nil, false
	}
	return v.(channel.Channel), ok
}

func (s *Server) Channels() map[string]channel.Channel {
	result := make(map[string]channel.Channel)
	s.channels.Range(func(k, v interface{}) bool {
		result[k.(string)] = v.(channel.Channel)
		return true
	})
	return result
}

func WithChannels(chs ...channel.Channel) Option {
	return func(s *Server) {
		for _, ch := range chs {
			s.RegisterChannel(ch)
		}
	}
}

func (s *Server) StartAllChannels(ctx context.Context, handler MessageHandler) error {
	var lastErr error
	s.channels.Range(func(_, value interface{}) bool {
		ch := value.(channel.Channel)
		wrappedHandler := func(cctx context.Context, cmsg *channel.Message) error {
			gwMsg := FromChannelMessage(cmsg)
			gwMsg.ChannelID = ch.Name()
			handler(gwMsg)
			return nil
		}
		if base, ok := ch.(interface {
			SetHandler(func(context.Context, *channel.Message) error)
		}); ok {
			base.SetHandler(wrappedHandler)
		}
		if err := ch.Start(ctx, wrappedHandler); err != nil {
			lastErr = err
			slog.Error("failed to start channel", "name", ch.Name(), "error", err)
		} else {
			slog.Info("channel started", "name", ch.Name())
		}
		return true
	})
	return lastErr
}

func (s *Server) StopAllChannels(ctx context.Context) error {
	var lastErr error
	s.channels.Range(func(_, value interface{}) bool {
		ch := value.(channel.Channel)
		if err := ch.Stop(ctx); err != nil {
			lastErr = err
			slog.Error("failed to stop channel", "name", ch.Name(), "error", err)
		}
		return true
	})
	return lastErr
}

func (s *Server) SendToChannel(ctx context.Context, channelName string, msg *Message) error {
	ch, ok := s.GetChannel(channelName)
	if !ok {
		return fmt.Errorf("%w: %s", ErrClientNotFound, channelName)
	}
	chMsg := ToChannelMessage(msg)
	return ch.SendMessage(ctx, chMsg)
}

func ToChannelMessage(msg *Message) *channel.Message {
	// Preserve the original message type from metadata if available.
	// When converting back, the original MessageType is stored in metadata.
	msgType := channel.MessageTypeText
	switch msg.ContentType {
	case "application/json":
		msgType = channel.MessageTypeText
	case "application/octet-stream":
		msgType = channel.MessageTypeFile
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		msgType = channel.MessageTypeImage
	}

	metadata := map[string]interface{}{
		"session_id":   msg.SessionID,
		"client_id":    msg.ClientID,
		"content_type": msg.ContentType,
	}
	// Preserve original message type for round-trip fidelity
	if msg.ChannelID != "" {
		metadata["channel_id"] = msg.ChannelID
	}

	return &channel.Message{
		ID:        msg.ID,
		ChannelID: msg.ChannelID,
		Type:      msgType,
		Direction: channel.DirectionOutbound,
		Content:   string(msg.Data),
		Data:      msg.Data,
		Timestamp: msg.Timestamp.Format(time.RFC3339),
		Metadata:  metadata,
	}
}

func FromChannelMessage(msg *channel.Message) *Message {
	data := []byte(msg.Content)
	if len(msg.Data) > 0 {
		data = msg.Data
	}
	var ts time.Time
	if msg.Timestamp != "" {
		ts, _ = time.Parse(time.RFC3339, msg.Timestamp)
	}
	sessionID := ""
	if v, ok := msg.Metadata["session_id"].(string); ok {
		sessionID = v
	}
	clientID := ""
	if v, ok := msg.Metadata["client_id"].(string); ok {
		clientID = v
	}
	channelID := msg.ChannelID
	if channelID == "" {
		if v, ok := msg.Metadata["channel_id"].(string); ok {
			channelID = v
		}
	}

	// Use the original MessageType as ContentType for better type fidelity.
	// This avoids the semantic mismatch of storing MessageType as ContentType.
	contentType := string(msg.Type)
	if contentType == "" {
		contentType = "text/plain"
	}

	return &Message{
		ID:          msg.ID,
		SessionID:   sessionID,
		ClientID:    clientID,
		ChannelID:   channelID,
		Direction:   DirectionInbound,
		Data:        data,
		ContentType: contentType,
		Timestamp:   ts,
	}
}
