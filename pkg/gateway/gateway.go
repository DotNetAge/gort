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

var upgrader *websocket.Upgrader

func initUpgrader(cfg *WSConfig) {
	upgrader = &websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			if cfg == nil {
				cfg = defaultWSConfig()
			}
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

type MessageHandler func(g *Server, msg *Message)

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

	initUpgrader(s.wsConfig)

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

		msg := &Message{
			ID:        uuid.New().String(),
			ClientID:  c.id,
			Direction: DirectionInbound,
			Data:      []byte("connected"),
			Timestamp: time.Now(),
		}
		data, _ := json.Marshal(msg)
		func() {
			defer func() { recover() }()
			select {
			case c.send <- data:
			default:
			}
		}()
		slog.Info("client connected", "id", c.id, "total", len(s.clients))
	}
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
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
}

func (s *Server) readPump(c *client) {
	defer func() {
		done(c.done)
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

		text := string(raw)
		s.dispatchCommand(c, text)
	}
}

func (s *Server) dispatchCommand(c *client, text string) {
	if len(text) > MaxMessageSize {
		slog.Warn("message exceeds max size", "id", c.id, "size", len(text))
		return
	}

	parts := strings.SplitN(text, "|", 5)
	if len(parts) < 1 {
		return
	}

	cmd := Command(parts[0])
	if !validCommands[cmd] {
		slog.Warn("unknown command rejected", "id", c.id, "cmd", cmd)
		return
	}
	switch cmd {
	case CmdSessionStart:
		s.handleSessionStart(c, parts)
	case CmdData:
		s.handleData(c, parts)
	case CmdSessionEnd:
		s.handleSessionEnd(c, parts)
	}
}

func (s *Server) handleSessionStart(c *client, parts []string) {
	total := 1
	if len(parts) >= 4 && parts[3] != "" {
		if _, err := fmt.Sscanf(parts[3], "%d", &total); err != nil {
			slog.Warn("invalid total format", "id", c.id, "value", parts[3])
			total = 1
		}
	}
	if total < 1 {
		total = 1
	}
	if total > MaxSessionTotal {
		total = MaxSessionTotal
	}

	sess, err := c.sessions.create(c.id, total)
	if err != nil {
		slog.Warn("failed to create session", "id", c.id, "error", err)
		return
	}
	resp := fmt.Sprintf("%s|%s|%s||", CmdSessionStart, sess.id, CmdOK)
	s.sendText(c, resp)
}

func (s *Server) handleData(c *client, parts []string) {
	if len(parts) < 5 {
		return
	}
	sessionID := parts[1]
	var index, total int
	fmt.Sscanf(parts[2], "%d", &index)
	fmt.Sscanf(parts[3], "%d", &total)
	data := parts[4]

	if err := c.sessions.addData(sessionID, index, []byte(data)); err != nil {
		slog.Warn("data add failed", "id", c.id, "session", sessionID, "error", err)
		return
	}

	resp := fmt.Sprintf("%s|%s|%d|%s||", CmdData, sessionID, index, CmdOK)
	s.sendText(c, resp)
}

func (s *Server) handleSessionEnd(c *client, parts []string) {
	if len(parts) < 2 || parts[1] == "" {
		return
	}
	sessionID := parts[1]

	sess, complete := c.sessions.assembleAndRemove(sessionID)
	if !complete {
		slog.Warn("incomplete session", "id", c.id, "session", sessionID)
		return
	}

	var assembled []byte
	for i := 0; i < sess.total; i++ {
		if data, ok := sess.parts[i]; ok {
			assembled = append(assembled, data...)
		}
	}

	msg := &Message{
		ID:        uuid.New().String(),
		SessionID: sess.id,
		ClientID:  c.id,
		Direction: DirectionOutbound,
		Data:      assembled,
		Timestamp: time.Now(),
	}

	resp := fmt.Sprintf("%s|%s|%s||", CmdSessionEnd, sessionID, CmdOK)
	s.sendText(c, resp)

	if s.handler != nil {
		s.handler(s, msg)
	}
}

func (s *Server) sendText(c *client, text string) {
	select {
	case c.send <- []byte(text):
	default:
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

func done(fn func()) { fn() }

func (s *Server) Send(to string, message string) {
	msg := &Message{
		ID:        uuid.New().String(),
		Direction: DirectionInbound,
		Data:      []byte(message),
		Timestamp: time.Now(),
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
		ContentType: "file",
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

func (s *Server) sendMessage(to string, msg *Message) {
	data, _ := json.Marshal(msg)

	if to == "" || to == "*" {
		s.mu.RLock()
		defer s.mu.RUnlock()
		for _, c := range s.clients {
			select {
			case c.send <- data:
			default:
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
		if base, ok := ch.(interface{ SetHandler(func(context.Context, *channel.Message) error) }); ok {
			wrappedHandler := func(cctx context.Context, cmsg *channel.Message) error {
				gwMsg := FromChannelMessage(cmsg)
				gwMsg.ChannelID = ch.Name()
				handler(s, gwMsg)
				return nil
			}
			base.SetHandler(wrappedHandler)
		}
		if err := ch.Start(ctx, nil); err != nil {
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
	return &channel.Message{
		ID:        msg.ID,
		Type:      channel.MessageTypeText,
		Direction: channel.DirectionOutbound,
		Content:   string(msg.Data),
		Data:      msg.Data,
		Timestamp: msg.Timestamp.Format(time.RFC3339),
		Metadata: map[string]interface{}{
			"session_id":  msg.SessionID,
			"client_id":   msg.ClientID,
			"content_type": msg.ContentType,
		},
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
	channelID := ""
	if m, ok := msg.Metadata["channel_id"].(string); ok {
		channelID = m
	}
	return &Message{
		ID:          msg.ID,
		SessionID:   channelID,
		Direction:   DirectionInbound,
		Data:        data,
		ContentType: string(msg.Type),
		Timestamp:    ts,
	}
}
