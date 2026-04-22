package gateway

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Middleware interface {
	Name() string
	Priority() int
	Enabled() bool
	SetEnabled(bool)
	Handle(next http.Handler) http.Handler
}

type MiddlewareChain struct {
	middlewares  []Middleware
	handler      http.Handler
	finalHandler http.Handler
	mu           sync.RWMutex
}

func NewMiddlewareChain(final http.Handler) *MiddlewareChain {
	return &MiddlewareChain{
		handler:      final,
		finalHandler: final,
	}
}

func (mc *MiddlewareChain) Use(mw Middleware) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	mc.middlewares = append(mc.middlewares, mw)
	sort.Slice(mc.middlewares, func(i, j int) bool {
		return mc.middlewares[i].Priority() < mc.middlewares[j].Priority()
	})
	mc.build()
}

func (mc *MiddlewareChain) Remove(name string) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	filtered := mc.middlewares[:0]
	for _, mw := range mc.middlewares {
		if mw.Name() != name {
			filtered = append(filtered, mw)
		}
	}
	mc.middlewares = filtered
	mc.build()
}

func (mc *MiddlewareChain) Get(name string) (Middleware, bool) {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	for _, mw := range mc.middlewares {
		if mw.Name() == name {
			return mw, true
		}
	}
	return nil, false
}

func (mc *MiddlewareChain) List() []string {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	names := make([]string, len(mc.middlewares))
	for i, mw := range mc.middlewares {
		names[i] = fmt.Sprintf("%s(prio=%d,en=%v)", mw.Name(), mw.Priority(), mw.Enabled())
	}
	return names
}

func (mc *MiddlewareChain) build() {
	var handler = mc.finalHandler
	for i := len(mc.middlewares) - 1; i >= 0; i-- {
		if mc.middlewares[i].Enabled() {
			handler = mc.middlewares[i].Handle(handler)
		}
	}
	mc.handler = handler
}

func (mc *MiddlewareChain) Handler() http.Handler {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	return mc.handler
}

func (mc *MiddlewareChain) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	mc.Handler().ServeHTTP(w, r)
}

type LoggingConfig struct {
	Enabled         bool
	LogLevel        slog.Level
	OutputFormat    string
	SkipPaths       []string
	RequestIDHeader string
}

func DefaultLoggingConfig() *LoggingConfig {
	return &LoggingConfig{
		Enabled:         true,
		LogLevel:        slog.LevelInfo,
		OutputFormat:    "json",
		SkipPaths:       []string{"/health", "/metrics"},
		RequestIDHeader: "X-Request-ID",
	}
}

type LoggingMiddleware struct {
	config   *LoggingConfig
	logger   *slog.Logger
	priority int
	enabled  atomic.Bool
}

func NewLoggingMiddleware(cfg *LoggingConfig) *LoggingMiddleware {
	if cfg == nil {
		cfg = DefaultLoggingConfig()
	}
	lm := &LoggingMiddleware{
		config:   cfg,
		priority: 100,
	}
	lm.enabled.Store(cfg.Enabled)
	lm.logger = slog.Default()
	return lm
}

func (lm *LoggingMiddleware) Name() string      { return "logger" }
func (lm *LoggingMiddleware) Priority() int     { return lm.priority }
func (lm *LoggingMiddleware) Enabled() bool     { return lm.enabled.Load() }
func (lm *LoggingMiddleware) SetEnabled(v bool) { lm.enabled.Store(v) }

func (lm *LoggingMiddleware) Handle(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(wrapped, r)

		duration := time.Since(start)
		requestID := r.Header.Get(lm.config.RequestIDHeader)
		if requestID == "" {
			requestID = "n/a"
		}

		skip := false
		for _, p := range lm.config.SkipPaths {
			if strings.HasPrefix(r.URL.Path, p) {
				skip = true
				break
			}
		}
		if skip {
			return
		}

		entry := map[string]interface{}{
			"time":        start.Format(time.RFC3339Nano),
			"request_id":  requestID,
			"method":      r.Method,
			"path":        r.URL.Path,
			"query":       r.URL.RawQuery,
			"remote_addr": r.RemoteAddr,
			"user_agent":  r.UserAgent(),
			"status":      wrapped.statusCode,
			"duration_ms": duration.Milliseconds(),
			"size":        wrapped.written,
		}

		switch lm.config.OutputFormat {
		case "json":
			data, _ := json.Marshal(entry)
			lm.logger.Log(r.Context(), lm.config.LogLevel, "", "raw", data)
		default:
			lm.logger.Log(r.Context(), lm.config.LogLevel, "http_request",
				"request_id", entry["request_id"],
				"method", entry["method"],
				"path", entry["path"],
				"status", entry["status"],
				"duration_ms", entry["duration_ms"],
				"size", entry["size"])
		}
	})
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
	written    int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	n, err := rw.ResponseWriter.Write(b)
	rw.written += n
	return n, err
}

type CompressionConfig struct {
	Enabled      bool
	MinSize      int64
	Level        int
	ContentTypes []string
	SkipWhen     func(*http.Request) bool
}

func DefaultCompressionConfig() *CompressionConfig {
	return &CompressionConfig{
		Enabled: true,
		MinSize: 1024,
		Level:   gzip.DefaultCompression,
		ContentTypes: []string{
			"text/html", "text/plain", "text/css", "text/javascript",
			"application/javascript", "application/json", "application/xml",
			"image/svg+xml",
		},
	}
}

type CompressionMiddleware struct {
	config   *CompressionConfig
	priority int
	enabled  atomic.Bool
}

func NewCompressionMiddleware(cfg *CompressionConfig) *CompressionMiddleware {
	if cfg == nil {
		cfg = DefaultCompressionConfig()
	}
	cm := &CompressionMiddleware{
		config:   cfg,
		priority: 200,
	}
	cm.enabled.Store(cfg.Enabled)
	return cm
}

func (cm *CompressionMiddleware) Name() string      { return "compression" }
func (cm *CompressionMiddleware) Priority() int     { return cm.priority }
func (cm *CompressionMiddleware) Enabled() bool     { return cm.enabled.Load() }
func (cm *CompressionMiddleware) SetEnabled(v bool) { cm.enabled.Store(v) }

func (cm *CompressionMiddleware) Handle(next http.Handler) http.Handler {
	contentTypeMap := make(map[string]bool)
	for _, ct := range cm.config.ContentTypes {
		contentTypeMap[ct] = true
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !cm.shouldCompress(r) || cm.config.SkipWhen != nil && cm.config.SkipWhen(r) {
			next.ServeHTTP(w, r)
			return
		}

		gw := &gzipResponseWriter{
			ResponseWriter: w,
			level:          cm.config.Level,
			minSize:        cm.config.MinSize,
			contentTypes:   contentTypeMap,
		}
		next.ServeHTTP(gw, r)
		gw.maybeFlush()
	})
}

func (cm *CompressionMiddleware) shouldCompress(r *http.Request) bool {
	ae := r.Header.Get("Accept-Encoding")
	if !strings.Contains(ae, "gzip") {
		return false
	}
	return true
}

type gzipResponseWriter struct {
	http.ResponseWriter
	level        int
	minSize      int64
	contentTypes map[string]bool
	buf          []byte
	flushed      bool
	code         int
	headers      http.Header
}

func (gw *gzipResponseWriter) Write(b []byte) (int, error) {
	if gw.flushed {
		return gw.ResponseWriter.Write(b)
	}
	gw.buf = append(gw.buf, b...)
	return len(b), nil
}

func (gw *gzipResponseWriter) WriteHeader(code int) {
	if gw.code == 0 {
		gw.code = code
	}
}

func (gw *gzipResponseWriter) Header() http.Header {
	if gw.headers == nil {
		gw.headers = http.Header{}
	}
	return gw.headers
}

func (gw *gzipResponseWriter) maybeFlush() {
	if gw.flushed || len(gw.buf) < int(gw.minSize) {
		for k, v := range gw.headers {
			if k != "Content-Length" {
				gw.ResponseWriter.Header()[k] = v
			}
		}
		if gw.code > 0 {
			gw.ResponseWriter.WriteHeader(gw.code)
		}
		if len(gw.buf) > 0 {
			gw.ResponseWriter.Write(gw.buf)
		}
		return
	}

	ct := gw.headers.Get("Content-Type")
	baseCT := strings.Split(ct, ";")[0]
	if baseCT != "" && !gw.contentTypes[baseCT] {
		gw.ResponseWriter.Write(gw.buf)
		return
	}

	if gw.code > 0 {
		gw.ResponseWriter.WriteHeader(gw.code)
	} else {
		gw.code = http.StatusOK
		gw.ResponseWriter.WriteHeader(http.StatusOK)
	}

	for k, v := range gw.headers {
		if k != "Content-Length" {
			gw.ResponseWriter.Header()[k] = v
		}
	}
	gw.ResponseWriter.Header().Set("Content-Encoding", "gzip")

	gz, _ := gzip.NewWriterLevel(gw.ResponseWriter, gw.level)
	gz.Write(gw.buf)
	gz.Close()
	gw.flushed = true
}

type RateLimitConfig struct {
	Enabled       bool
	MaxConcurrent int
	QueueSize     int
	Timeout       time.Duration
	OnRejected    func(r *http.Request) error
	Weights       map[string]int
}

func DefaultRateLimitConfig() *RateLimitConfig {
	return &RateLimitConfig{
		Enabled:       true,
		MaxConcurrent: 100,
		QueueSize:     50,
		Timeout:       30 * time.Second,
		Weights:       map[string]int{},
	}
}

type RateLimitMiddleware struct {
	config   *RateLimitConfig
	active   int32
	queue    chan context.CancelFunc
	priority int
	enabled  atomic.Bool
	mu       sync.Mutex
}

func NewRateLimitMiddleware(cfg *RateLimitConfig) *RateLimitMiddleware {
	if cfg == nil {
		cfg = DefaultRateLimitConfig()
	}
	rm := &RateLimitMiddleware{
		config:   cfg,
		queue:    make(chan context.CancelFunc, cfg.QueueSize),
		priority: 300,
	}
	rm.enabled.Store(cfg.Enabled)
	return rm
}

func (rm *RateLimitMiddleware) Name() string      { return "rate_limit" }
func (rm *RateLimitMiddleware) Priority() int     { return rm.priority }
func (rm *RateLimitMiddleware) Enabled() bool     { return rm.enabled.Load() }
func (rm *RateLimitMiddleware) SetEnabled(v bool) { rm.enabled.Store(v) }

func (rm *RateLimitMiddleware) ActiveCount() int { return int(atomic.LoadInt32(&rm.active)) }
func (rm *RateLimitMiddleware) QueueLength() int { return len(rm.queue) }

func (rm *RateLimitMiddleware) Handle(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !rm.enabled.Load() {
			next.ServeHTTP(w, r)
			return
		}

		active := atomic.AddInt32(&rm.active, 1)
		defer atomic.AddInt32(&rm.active, -1)

		if active > int32(rm.config.MaxConcurrent) {
			select {
			case cancel := <-rm.queue:
				ctx, cancelFn := context.WithTimeout(r.Context(), rm.config.Timeout)
				cancelHolder := func() { cancel(); cancelFn() }
				go func(ch chan context.CancelFunc, holder func()) {
					time.Sleep(time.Duration(rand.Float64()) * time.Second)
					select {
					case ch <- holder:
					default:
					}
				}(rm.queue, cancelHolder)

				next.ServeHTTP(w, r.WithContext(ctx))
				return
			default:
				if rm.config.OnRejected != nil {
					if err := rm.config.OnRejected(r); err != nil {
						slog.Warn("rate limit rejected", "path", r.URL.Path, "error", err)
					}
				}
				http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

type RetryConfig struct {
	Enabled        bool
	MaxRetries     int
	RetryAfter     time.Duration
	Strategy       string
	Multiplier     float64
	RetryableCodes []int
	RetryMethods   []string
	MaxInterval    time.Duration
	OnRetry        func(attempt int, err error)
}

func DefaultRetryConfig() *RetryConfig {
	return &RetryConfig{
		Enabled:        true,
		MaxRetries:     3,
		RetryAfter:     500 * time.Millisecond,
		Strategy:       "exponential",
		Multiplier:     2.0,
		MaxInterval:    10 * time.Second,
		RetryableCodes: []int{502, 503, 504},
		RetryMethods:   []string{"GET", "HEAD"},
	}
}

type RetryMiddleware struct {
	config   *RetryConfig
	priority int
	enabled  atomic.Bool
	handler  http.Handler
}

func NewRetryMiddleware(cfg *RetryConfig) *RetryMiddleware {
	if cfg == nil {
		cfg = DefaultRetryConfig()
	}
	ret := &RetryMiddleware{
		config:   cfg,
		priority: 400,
	}
	ret.enabled.Store(cfg.Enabled)
	return ret
}

func (ret *RetryMiddleware) Name() string      { return "retry" }
func (ret *RetryMiddleware) Priority() int     { return ret.priority }
func (ret *RetryMiddleware) Enabled() bool     { return ret.enabled.Load() }
func (ret *RetryMiddleware) SetEnabled(v bool) { ret.enabled.Store(v) }

func (ret *RetryMiddleware) Handle(next http.Handler) http.Handler {
	retryCodes := make(map[int]bool)
	for _, c := range ret.config.RetryableCodes {
		retryCodes[c] = true
	}
	retryMethods := make(map[string]bool)
	for _, m := range ret.config.RetryMethods {
		retryMethods[m] = true
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !ret.enabled.Load() || !retryMethods[r.Method] {
			next.ServeHTTP(w, r)
			return
		}

		var lastErr error
		for attempt := 0; attempt <= ret.config.MaxRetries; attempt++ {
			if attempt > 0 {
				delay := ret.calcDelay(attempt - 1)
				if ret.config.OnRetry != nil {
					ret.config.OnRetry(attempt, lastErr)
				}
				time.Sleep(delay)
			}

			rw := newRetryResponseWriter()
			next.ServeHTTP(rw, r)

			if rw.Code == 0 || !retryCodes[rw.Code] {
				if rw.Code > 0 {
					w.WriteHeader(rw.Code)
				}
				w.Write(rw.Body.Bytes())
				return
			}

			lastErr = fmt.Errorf("status %d on attempt %d", rw.Code, attempt+1)
			slog.Warn("retrying request", "attempt", attempt+1, "max", ret.config.MaxRetries, "status", rw.Code, "path", r.URL.Path)
		}

		lastErr = fmt.Errorf("all %d retries exhausted", ret.config.MaxRetries+1)
		slog.Error("retries exhausted", "path", r.URL.Path, "error", lastErr)
		http.Error(w, "Gateway Timeout", http.StatusGatewayTimeout)
	})
}

func (ret *RetryMiddleware) calcDelay(attempt int) time.Duration {
	switch ret.config.Strategy {
	case "fixed":
		return ret.config.RetryAfter
	case "exponential":
		delay := ret.config.RetryAfter
		for i := 0; i < attempt; i++ {
			d := float64(delay) * ret.config.Multiplier
			delay = time.Duration(math.Min(d, float64(ret.config.MaxInterval)))
		}
		jitter := time.Duration(float64(delay) * 0.1 * rand.Float64())
		return delay + jitter
	default:
		return ret.config.RetryAfter
	}
}

type retryResponseWriter struct {
	*httptest.ResponseRecorder
}

func newRetryResponseWriter() *retryResponseWriter {
	return &retryResponseWriter{ResponseRecorder: httptest.NewRecorder()}
}

func WithHeartbeat(hc *HeartbeatConfig) Option {
	return func(s *Server) {
		if hc == nil {
			hc = DefaultHeartbeatConfig()
		}
		s.heartbeatCfg = hc
		s.hbMonitor = newHeartbeatMonitor(hc)
	}
}

func WithReconnect(rc *ReconnectConfig) ClientOption {
	return func(c *Client) {
		if rc == nil {
			rc = DefaultReconnectConfig()
		}
		c.reconnectCfg = rc
	}
}

func (s *Server) HeartbeatStats() HeartbeatStats {
	if s.hbMonitor == nil {
		return HeartbeatStats{}
	}
	return s.hbMonitor.Stats()
}

func (c *Client) ReconnectAttempts() int {
	c.reconnectMu.RLock()
	defer c.reconnectMu.RUnlock()
	return c.reconnectAttempts
}

func (c *Client) State() ConnectionState {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.running {
		return StateDisconnected
	}
	if c.conn == nil {
		return StateConnecting
	}
	return StateConnected
}

func (c *Client) OnStateChange(fn func(oldState, newState ConnectionState)) {
	c.stateChangeMu.Lock()
	c.stateChangeHandler = fn
	c.stateChangeMu.Unlock()
}

func (c *Client) OnReconnectFail(fn func(err error)) {
	c.reconnectMu.Lock()
	c.onReconnectFail = fn
	c.reconnectMu.Unlock()
}
