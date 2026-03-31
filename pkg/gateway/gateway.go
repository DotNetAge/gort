package gateway

import (
	"context"
	"errors"
	"sync"

	"github.com/DotNetAge/gort/pkg/channel"
	"github.com/DotNetAge/gort/pkg/message"
	"github.com/DotNetAge/gort/pkg/middleware"
	"github.com/DotNetAge/gort/pkg/session"
)

var (
	ErrChannelNotFound = errors.New("channel not found")
	ErrNotRunning      = errors.New("gateway is not running")
	ErrAlreadyRunning  = errors.New("gateway is already running")
)

type ClientHandler func(ctx context.Context, clientID string, msg *message.Message) error

type ChannelHandler func(ctx context.Context, msg *message.Message) error

type Gateway struct {
	registry       *channel.Registry
	sessionManager *session.Manager
	middleware     *middleware.Chain

	clientHandler  ClientHandler
	channelHandler ChannelHandler

	running bool
	mu      sync.RWMutex
}

func New(sessionManager *session.Manager) *Gateway {
	return &Gateway{
		registry:       channel.NewRegistry(),
		sessionManager: sessionManager,
		middleware:     middleware.NewChain(),
	}
}

func (g *Gateway) RegisterChannel(ch channel.Channel) error {
	return g.registry.Register(ch)
}

func (g *Gateway) GetChannel(name string) (channel.Channel, bool) {
	return g.registry.Get(name)
}

func (g *Gateway) RegisterClientHandler(handler ClientHandler) {
	g.clientHandler = handler
}

func (g *Gateway) RegisterChannelHandler(handler ChannelHandler) {
	g.channelHandler = handler
}

func (g *Gateway) Use(m middleware.Middleware) {
	g.middleware.Use(m)
}

func (g *Gateway) Start(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.running {
		return ErrAlreadyRunning
	}

	for _, ch := range g.registry.GetAll() {
		if err := ch.Start(ctx, g.handleChannelMessage); err != nil {
			g.stopChannels(ctx)
			return err
		}
	}

	g.running = true
	return nil
}

func (g *Gateway) Stop(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	if !g.running {
		return ErrNotRunning
	}

	g.stopChannels(ctx)

	if err := g.sessionManager.Stop(ctx); err != nil {
		return err
	}

	g.running = false
	return nil
}

func (g *Gateway) stopChannels(ctx context.Context) {
	for _, ch := range g.registry.GetAll() {
		ch.Stop(ctx)
	}
}

func (g *Gateway) IsRunning() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.running
}

func (g *Gateway) HandleChannelMessage(ctx context.Context, msg *message.Message) error {
	return g.middleware.Execute(ctx, msg, g.processChannelMessage)
}

func (g *Gateway) HandleClientMessage(ctx context.Context, clientID string, msg *message.Message) error {
	return g.middleware.Execute(ctx, msg, func(ctx context.Context, msg *message.Message) error {
		return g.processClientMessage(ctx, clientID, msg)
	})
}

func (g *Gateway) GetClientCount() int {
	return g.sessionManager.GetClientCount()
}

func (g *Gateway) Broadcast(ctx context.Context, msg *message.Message) error {
	return g.sessionManager.Broadcast(ctx, msg)
}

func (g *Gateway) SendTo(ctx context.Context, clientID string, msg *message.Message) error {
	return g.sessionManager.SendTo(ctx, clientID, msg)
}

func (g *Gateway) handleChannelMessage(ctx context.Context, msg *message.Message) error {
	msg.Direction = message.DirectionInbound
	return g.middleware.Execute(ctx, msg, g.processChannelMessage)
}

func (g *Gateway) processChannelMessage(ctx context.Context, msg *message.Message) error {
	if g.channelHandler != nil {
		if err := g.channelHandler(ctx, msg); err != nil {
			return err
		}
	}

	return g.sessionManager.Broadcast(ctx, msg)
}

func (g *Gateway) processClientMessage(ctx context.Context, clientID string, msg *message.Message) error {
	msg.Direction = message.DirectionOutbound

	if g.clientHandler != nil {
		if err := g.clientHandler(ctx, clientID, msg); err != nil {
			return err
		}
	}

	ch, ok := g.registry.Get(msg.ChannelID)
	if !ok {
		return ErrChannelNotFound
	}

	return ch.SendMessage(ctx, msg)
}
