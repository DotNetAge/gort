package gateway

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestHeartbeatConfig_Defaults(t *testing.T) {
	cfg := DefaultHeartbeatConfig()
	if cfg.Interval != 30*time.Second {
		t.Errorf("expected 30s interval, got %v", cfg.Interval)
	}
	if cfg.ReadTimeout != 10*time.Second {
		t.Errorf("expected 10s read timeout, got %v", cfg.ReadTimeout)
	}
	if cfg.PingPeriod != 27*time.Second {
		t.Errorf("expected 27s ping period, got %v", cfg.PingPeriod)
	}
	if cfg.MaxMissedPings != 3 {
		t.Errorf("expected max missed pings=3, got %d", cfg.MaxMissedPings)
	}
}

func TestHeartbeatMonitor_New(t *testing.T) {
	hm := newHeartbeatMonitor(nil)
	stats := hm.Stats()
	if stats.TotalPingsSent != 0 || stats.TotalPongsReceived != 0 {
		t.Error("new monitor should have zero stats")
	}
}

func TestHeartbeatMonitor_RecordPing(t *testing.T) {
	hm := newHeartbeatMonitor(nil)
	hm.recordPing()
	hm.recordPing()
	hm.recordPing()

	stats := hm.Stats()
	if stats.TotalPingsSent != 3 {
		t.Errorf("expected 3 pings sent, got %d", stats.TotalPingsSent)
	}
	if stats.LastPingAt.IsZero() {
		t.Error("LastPingAt should be set")
	}
}

func TestHeartbeatMonitor_RecordPong(t *testing.T) {
	hm := newHeartbeatMonitor(nil)
	hm.recordPong()
	hm.recordPong()

	stats := hm.Stats()
	if stats.TotalPongsReceived != 2 {
		t.Errorf("expected 2 pongs received, got %d", stats.TotalPongsReceived)
	}
	if stats.LastPongAt.IsZero() {
		t.Error("LastPongAt should be set")
	}
}

func TestHeartbeatMonitor_RecordMiss(t *testing.T) {
	hm := newHeartbeatMonitor(nil)
	hm.recordMiss()
	hm.recordMiss()

	stats := hm.Stats()
	if stats.MissedPings != 2 {
		t.Errorf("expected 2 misses, got %d", stats.MissedPings)
	}
}

func TestHeartbeatMonitor_ConnectionCount(t *testing.T) {
	hm := newHeartbeatMonitor(nil)
	hm.setConnectionCount(5)

	stats := hm.Stats()
	if stats.ConnectionCount != 5 {
		t.Errorf("expected 5 connections, got %d", stats.ConnectionCount)
	}
}

func TestHeartbeatMonitor_Config(t *testing.T) {
	customCfg := &HeartbeatConfig{
		Interval:       15 * time.Second,
		ReadTimeout:    5 * time.Second,
		PingPeriod:     13 * time.Second,
		MaxMissedPings: 5,
	}
	hm := newHeartbeatMonitor(customCfg)
	cfg := hm.Config()
	if cfg.Interval != 15*time.Second {
		t.Error("custom config not applied")
	}
}

func TestHeartbeatMonitor_Callbacks(t *testing.T) {
	hm := newHeartbeatMonitor(nil)

	stateChanges := make([]string, 0)
	var timeoutCalled bool
	var reconnectCalled bool

	hm.OnStateChange(func(clientID, oldState, newState string) {
		stateChanges = append(stateChanges, clientID+":"+oldState+"->"+newState)
	})
	hm.OnTimeout(func(id string) { timeoutCalled = true })
	hm.OnReconnect(func(id string, attempt int) { reconnectCalled = true })

	hm.notifyStateChange("c1", "disconnected", "connected")
	hm.notifyTimeout("c2")
	hm.notifyReconnect("c3", 1)

	time.Sleep(50 * time.Millisecond)

	if len(stateChanges) != 1 || stateChanges[0] != "c1:disconnected->connected" {
		t.Errorf("unexpected state changes: %v", stateChanges)
	}
	if !timeoutCalled {
		t.Error("OnTimeout callback should have been called")
	}
	if !reconnectCalled {
		t.Error("OnReconnect callback should have been called")
	}
}

func TestConnectionState_String(t *testing.T) {
	tests := []struct {
		state ConnectionState
		want  string
	}{
		{StateDisconnected, "disconnected"},
		{StateConnecting, "connecting"},
		{StateConnected, "connected"},
		{StateReconnecting, "reconnecting"},
		{ConnectionState(99), "unknown"},
	}
	for _, tt := range tests {
		got := tt.state.String()
		if got != tt.want {
			t.Errorf("State(%d).String() = %s, want %s", tt.state, got, tt.want)
		}
	}
}

func TestReconnectConfig_Defaults(t *testing.T) {
	rc := DefaultReconnectConfig()
	if !rc.Enabled {
		t.Error("reconnect should be enabled by default")
	}
	if rc.MaxRetries != 10 {
		t.Errorf("expected max retries=10, got %d", rc.MaxRetries)
	}
	if rc.InitialInterval != 1*time.Second {
		t.Errorf("expected initial interval=1s, got %v", rc.InitialInterval)
	}
	if rc.Multiplier != 2.0 {
		t.Errorf("expected multiplier=2.0, got %v", rc.Multiplier)
	}
	if rc.MaxInterval != 60*time.Second {
		t.Errorf("expected max interval=60s, got %v", rc.MaxInterval)
	}
}

func TestReconnectConfig_CalcDelay_Fixed(t *testing.T) {
	rc := &ReconnectConfig{Strategy: "fixed", InitialInterval: 500 * time.Millisecond, MaxInterval: 10 * time.Second, Multiplier: 1.0, Jitter: 0}
	for i := 0; i < 5; i++ {
		delay := rc.calcDelay(i)
		if delay < 450*time.Millisecond || delay > 550*time.Millisecond {
			t.Errorf("fixed strategy attempt %d: unexpected delay %v", i, delay)
		}
	}
}

func TestReconnectConfig_CalcDelay_Exponential(t *testing.T) {
	rc := &ReconnectConfig{
		Strategy:        "exponential",
		InitialInterval: 100 * time.Millisecond,
		Multiplier:      2.0,
		MaxInterval:     8 * time.Second,
		Jitter:          0.0,
	}

	prev := time.Duration(0)
	for i := 0; i < 6; i++ {
		delay := rc.calcDelay(i)
		if delay <= prev && i > 0 {
			t.Errorf("exponential: delay should increase (attempt=%d): prev=%v cur=%v", i, prev, delay)
		}
		if delay > 8*time.Second {
			t.Errorf("exponential: delay %v exceeds max 8s at attempt %d", delay, i)
		}
		prev = delay
	}
}

func TestReconnectConfig_CalcDelay_CapsMax(t *testing.T) {
	rc := &ReconnectConfig{
		Strategy:        "exponential",
		InitialInterval: 5 * time.Second,
		Multiplier:      10.0,
		MaxInterval:     30 * time.Second,
		Jitter:          0.0,
	}
	delay := rc.calcDelay(10)
	if delay > 31*time.Second {
		t.Errorf("delay with huge multiplier should cap to max: got %v", delay)
	}
}

func TestServer_WithHeartbeat_Option(t *testing.T) {
	s := New(WithHeartbeat(&HeartbeatConfig{
		Interval: 15 * time.Second,
	}))
	if s.hbMonitor == nil {
		t.Fatal("heartbeat monitor should be initialized")
	}
	if s.hbMonitor.config.Interval != 15*time.Second {
		t.Errorf("config not applied correctly")
	}
}

func TestServer_HeartbeatStats_Default(t *testing.T) {
	s := New()
	stats := s.HeartbeatStats()
	if stats.TotalPingsSent != 0 {
		t.Error("default server should have zero stats")
	}
}

func TestClient_State_Disconnected(t *testing.T) {
	c := NewClient()
	if c.State() != StateDisconnected {
		t.Errorf("new client should be disconnected, got %s", c.State())
	}
}

func TestClient_OnStateChange(t *testing.T) {
	c := NewClient()
	var oldGot, newGot ConnectionState
	c.OnStateChange(func(old, newState ConnectionState) {
		oldGot = old
		newGot = newState
	})
	c.stateChangeMu.Lock()
	c.stateChangeHandler(StateDisconnected, StateConnected)
	c.stateChangeMu.Unlock()
	if oldGot != StateDisconnected || newGot != StateConnected {
		t.Errorf("state change handler not triggered correctly")
	}
}

func TestClient_ReconnectAttempts_Default(t *testing.T) {
	c := NewClient()
	if c.ReconnectAttempts() != 0 {
		t.Errorf("new client should have 0 reconnect attempts, got %d", c.ReconnectAttempts())
	}
}

func TestMiddlewareChain_Basic(t *testing.T) {
	final := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
	mc := NewMiddlewareChain(final)
	if mc.Handler() == nil {
		t.Error("handler should not be nil")
	}
}

type dummyMiddleware struct {
	name     string
	priority int
	enabled  atomic.Bool
	header   string
	value    string
}

func (d *dummyMiddleware) Name() string      { return d.name }
func (d *dummyMiddleware) Priority() int     { return d.priority }
func (d *dummyMiddleware) Enabled() bool     { return d.enabled.Load() }
func (d *dummyMiddleware) SetEnabled(v bool) { d.enabled.Store(v) }
func (d *dummyMiddleware) Handle(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(d.header, d.value)
		next.ServeHTTP(w, r)
	})
}

func TestMiddlewareChain_Use_Order(t *testing.T) {
	final := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	mc := NewMiddlewareChain(final)

	mwA := &dummyMiddleware{name: "a", priority: 300, header: "X-A", value: "1"}
	mwB := &dummyMiddleware{name: "b", priority: 100, header: "X-B", value: "2"}
	mwC := &dummyMiddleware{name: "c", priority: 200, header: "X-C", value: "3"}
	mwA.SetEnabled(true)
	mwB.SetEnabled(true)
	mwC.SetEnabled(true)
	mc.Use(mwA)
	mc.Use(mwB)
	mc.Use(mwC)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	mc.ServeHTTP(rec, req)

	a := rec.Header().Get("X-A")
	b := rec.Header().Get("X-B")
	c := rec.Header().Get("X-C")

	if a == "" || b == "" || c == "" {
		t.Errorf("all middlewares should have set headers: a=%q b=%q c=%q", a, b, c)
	}
}

func TestMiddlewareChain_Remove(t *testing.T) {
	final := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("final"))
	})
	mc := NewMiddlewareChain(final)
	dmw := &dummyMiddleware{name: "removable", priority: 1, header: "X-Test", value: "yes"}
	mc.Use(dmw)

	if _, ok := mc.Get("removable"); !ok {
		t.Error("middleware should exist after Use()")
	}

	mc.Remove("removable")

	if _, ok := mc.Get("removable"); ok {
		t.Error("middleware should not exist after Remove()")
	}
}

func TestMiddlewareChain_Get_NotFound(t *testing.T) {
	mc := NewMiddlewareChain(http.NotFoundHandler())
	if _, ok := mc.Get("nonexistent"); ok {
		t.Error("should return false for non-existent middleware")
	}
}

func TestMiddlewareChain_List(t *testing.T) {
	mc := NewMiddlewareChain(http.NotFoundHandler())
	mc.Use(&dummyMiddleware{name: "alpha", priority: 10})
	mc.Use(&dummyMiddleware{name: "beta", priority: 20})

	list := mc.List()
	if len(list) != 2 {
		t.Errorf("expected 2 items in list, got %d", len(list))
	}
}

func TestMiddlewareChain_EnableDisable(t *testing.T) {
	dmw := &dummyMiddleware{
		name: "toggle", priority: 1, header: "X-Toggle", value: "on",
	}
	dmw.SetEnabled(true)
	if !dmw.Enabled() {
		t.Error("should be enabled after SetEnabled(true)")
	}
	dmw.SetEnabled(false)
	if dmw.Enabled() {
		t.Error("should be disabled after SetEnabled(false)")
	}
}

func TestLoggingMiddleware_Defaults(t *testing.T) {
	lm := NewLoggingMiddleware(nil)
	if lm.Name() != "logger" {
		t.Errorf("expected name=logger, got %s", lm.Name())
	}
	if lm.Priority() != 100 {
		t.Errorf("expected priority=100, got %d", lm.Priority())
	}
	if !lm.Enabled() {
		t.Error("should be enabled by default")
	}
}

func TestLoggingMiddleware_CustomConfig(t *testing.T) {
	lm := NewLoggingMiddleware(&LoggingConfig{
		Enabled:      false,
		OutputFormat: "text",
		LogLevel:     slog.LevelDebug,
		SkipPaths:    []string{"/health", "/ping"},
	})
	if lm.Enabled() {
		t.Error("should be disabled per config")
	}
}

func TestLoggingMiddleware_Handle(t *testing.T) {
	lm := NewLoggingMiddleware(DefaultLoggingConfig())

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("data"))
	})

	handler := lm.Handle(next)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/test?foo=bar", nil)
	req.Header.Set("X-Request-ID", "req-123")
	req.Header.Set("User-Agent", "test-agent/1.0")

	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("next handler should have been called")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestLoggingMiddleware_SkipHealthPath(t *testing.T) {
	cfg := DefaultLoggingConfig()
	cfg.SkipPaths = []string{"/health"}
	lm := NewLoggingMiddleware(cfg)

	callCount := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
	})

	handler := lm.Handle(next)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	handler.ServeHTTP(rec, req)

	if callCount != 1 {
		t.Error("health path should still reach next handler")
	}
}

func TestCompressionMiddleware_Defaults(t *testing.T) {
	cm := NewCompressionMiddleware(nil)
	if cm.Name() != "compression" {
		t.Errorf("expected name=compression, got %s", cm.Name())
	}
	if cm.Priority() != 200 {
		t.Errorf("expected priority=200, got %d", cm.Priority())
	}
}

func TestCompressionMiddleware_Handle_NoAcceptEncoding(t *testing.T) {
	cm := NewCompressionMiddleware(DefaultCompressionConfig())

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"key":"value"}`))
	})

	handler := cm.Handle(next)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	handler.ServeHTTP(rec, req)

	ct := rec.Header().Get("Content-Encoding")
	if ct == "gzip" {
		t.Error("should NOT compress when no Accept-Encoding header")
	}
}

func TestCompressionMiddleware_Handle_SmallBody(t *testing.T) {
	cm := NewCompressionMiddleware(&CompressionConfig{
		MinSize:      1024,
		ContentTypes: []string{"application/json"},
	})

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"small":true}`))
	})

	handler := cm.Handle(next)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/small", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	handler.ServeHTTP(rec, req)

	ct := rec.Header().Get("Content-Encoding")
	if ct == "gzip" {
		t.Error("should NOT compress body smaller than MinSize")
	}
}

func TestRateLimitMiddleware_Defaults(t *testing.T) {
	rm := NewRateLimitMiddleware(nil)
	if rm.Name() != "rate_limit" {
		t.Errorf("expected name=rate_limit, got %s", rm.Name())
	}
	if rm.Priority() != 300 {
		t.Errorf("expected priority=300, got %d", rm.Priority())
	}
	cfg := DefaultRateLimitConfig()
	if rm.config.MaxConcurrent != cfg.MaxConcurrent {
		t.Error("default config mismatch")
	}
}

func TestRateLimitMiddleware_ActiveCount(t *testing.T) {
	rm := NewRateLimitMiddleware(nil)
	if rm.ActiveCount() != 0 {
		t.Error("initial active count should be 0")
	}
}

func TestRateLimitMiddleware_QueueLength(t *testing.T) {
	rm := NewRateLimitMiddleware(&RateLimitConfig{QueueSize: 50})
	if rm.QueueLength() != 0 {
		t.Error("initial queue length should be 0")
	}
}

func TestRateLimitMiddleware_Handle_UnderLimit(t *testing.T) {
	rm := NewRateLimitMiddleware(&RateLimitConfig{MaxConcurrent: 10})

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	handler := rm.Handle(next)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/under", nil)
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("request under limit should succeed")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestRetryMiddleware_Defaults(t *testing.T) {
	ret := NewRetryMiddleware(nil)
	if ret.Name() != "retry" {
		t.Errorf("expected name=retry, got %s", ret.Name())
	}
	if ret.Priority() != 400 {
		t.Errorf("expected priority=400, got %d", ret.Priority())
	}
	cfg := DefaultRetryConfig()
	if ret.config.MaxRetries != cfg.MaxRetries {
		t.Error("default config mismatch")
	}
}

func TestRetryMiddleware_CalcDelay_Fixed(t *testing.T) {
	ret := &RetryMiddleware{config: &RetryConfig{
		Strategy:   "fixed",
		RetryAfter: 200 * time.Millisecond,
	}}
	d0 := ret.calcDelay(0)
	d3 := ret.calcDelay(3)
	if d0 != 200*time.Millisecond {
		t.Errorf("fixed delay attempt 0: expected 200ms, got %v", d0)
	}
	if d3 != 200*time.Millisecond {
		t.Errorf("fixed delay attempt 3: expected 200ms, got %v", d3)
	}
}

func TestRetryMiddleware_CalcDelay_Exponential(t *testing.T) {
	ret := &RetryMiddleware{config: &RetryConfig{
		Strategy:    "exponential",
		RetryAfter:  100 * time.Millisecond,
		Multiplier:  2.0,
		MaxInterval: 10 * time.Second,
	}}

	d0 := ret.calcDelay(0)
	d1 := ret.calcDelay(1)
	d2 := ret.calcDelay(2)

	if d1 <= d0 {
		t.Error("exponential: delay should grow")
	}
	if d2 <= d1 {
		t.Error("exponential: delay should continue growing")
	}
}

func TestRetryMiddleware_CalcDelay_CapsMax(t *testing.T) {
	ret := &RetryMiddleware{config: &RetryConfig{
		Strategy:    "exponential",
		RetryAfter:  1 * time.Second,
		Multiplier:  100.0,
		MaxInterval: 5 * time.Second,
	}}
	d := ret.calcDelay(5)
	if d > 7*time.Second {
		t.Errorf("delay should cap near max: got %v", d)
	}
}

func TestRetryMiddleware_Handle_NonRetryableMethod(t *testing.T) {
	ret := NewRetryMiddleware(&RetryConfig{
		RetryMethods: []string{"GET"},
	})

	attempts := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusInternalServerError)
	})

	handler := ret.Handle(next)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/post", nil)
	handler.ServeHTTP(rec, req)

	if attempts != 1 {
		t.Errorf("POST should not retry, expected 1 attempt, got %d", attempts)
	}
}

func TestRetryMiddleware_Handle_SuccessNoRetry(t *testing.T) {
	ret := NewRetryMiddleware(DefaultRetryConfig())

	attempts := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	handler := ret.Handle(next)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/ok", nil)
	handler.ServeHTTP(rec, req)

	if attempts != 1 {
		t.Errorf("successful request should not retry, got %d attempts", attempts)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if body != "ok" {
		t.Errorf("body mismatch: %s", body)
	}
}

func TestRetryMiddleware_Handle_Retries503(t *testing.T) {
	ret := NewRetryMiddleware(&RetryConfig{
		Enabled:        true,
		MaxRetries:     2,
		RetryAfter:     10 * time.Millisecond,
		Strategy:       "fixed",
		RetryableCodes: []int{503},
		RetryMethods:   []string{"GET"},
	})

	attempts := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusServiceUnavailable)
	})

	handler := ret.Handle(next)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/unavailable", nil)
	handler.ServeHTTP(rec, req)

	expectedAttempts := 3 // initial + 2 retries
	if attempts != expectedAttempts {
		t.Errorf("expected %d attempts for 503, got %d", expectedAttempts, attempts)
	}
}

func TestFullMiddlewareStack_Integration(t *testing.T) {
	final := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Final", "yes")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("stack"))
	})

	mc := NewMiddlewareChain(final)
	mc.Use(NewLoggingMiddleware(DefaultLoggingConfig()))
	mc.Use(NewCompressionMiddleware(DefaultCompressionConfig()))
	mc.Use(NewRateLimitMiddleware(&RateLimitConfig{MaxConcurrent: 100}))
	mc.Use(NewRetryMiddleware(&RetryConfig{Enabled: true, MaxRetries: 1}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/test?x=y", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	req.Header.Set("X-Request-ID", "stack-test-001")
	req.Header.Set("User-Agent", "integration-test/1.0")

	mc.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("full stack: expected 200, got %d", rec.Code)
	}
	if rec.Header().Get("X-Final") != "yes" {
		t.Error("full stack: final middleware should have executed")
	}
}

func TestWebSocket_PingPong_WithHeartbeat(t *testing.T) {
	env := setupTestServerWithHeartbeat(t, &HeartbeatConfig{
		Interval:    5 * time.Second,
		ReadTimeout: 5 * time.Second,
		PingPeriod:  200 * time.Millisecond,
	})
	defer env.cleanup()

	conn := dialTestWS(t, env.ts)
	defer conn.Close()

	extractClientIDFromConn(t, conn)

	time.Sleep(500 * time.Millisecond)

	stats := env.gw.HeartbeatStats()
	if stats.TotalPingsSent == 0 {
		t.Fatalf("expected pings sent with short PingPeriod, got %d", stats.TotalPingsSent)
	}
	if stats.ConnectionCount == 0 {
		t.Fatal("expected connected client in monitor")
	}

	conn.WriteMessage(websocket.PingMessage, []byte{})
	time.Sleep(100 * time.Millisecond)

	statsAfter := env.gw.HeartbeatStats()
	if statsAfter.TotalPongsReceived > 0 {
		t.Logf("pong recorded: total pongs=%d", statsAfter.TotalPongsReceived)
	}
}

func TestWebSocket_ClientDisconnect_Detection(t *testing.T) {
	env := setupTestServer(t)
	defer env.cleanup()

	conn := dialTestWS(t, env.ts)
	extractClientIDFromConn(t, conn)

	if env.gw.ClientCount() != 1 {
		t.Fatalf("expected 1 client before disconnect, got %d", env.gw.ClientCount())
	}

	conn.Close()
	time.Sleep(300 * time.Millisecond)

	if env.gw.ClientCount() != 0 {
		t.Errorf("expected 0 clients after disconnect, got %d", env.gw.ClientCount())
	}
}

func TestClient_AutoReconnectDisabled(t *testing.T) {
	c := NewClient(WithClientPort(1))
	c.reconnectCfg = &ReconnectConfig{Enabled: false}

	if c.reconnectCfg.Enabled {
		t.Error("reconnect should be disabled when configured so")
	}
}

func setupTestServerWithHeartbeat(t *testing.T, hc *HeartbeatConfig) *testEnv {
	t.Helper()
	mux := http.NewServeMux()
	var gw *Server
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		gw.handleWS(w, r)
	})

	ts := httptest.NewServer(mux)
	addr := testHostPort(ts)
	gw = New(WithAddr(addr), WithHeartbeat(hc))

	go gw.Start()
	time.Sleep(50 * time.Millisecond)

	env := &testEnv{gw: gw, ts: ts}
	env.cleanup = func() {
		ts.Close()
		gw.Shutdown(context.Background())
	}
	return env
}
