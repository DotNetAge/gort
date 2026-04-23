package gateway

import (
	"encoding/json"
	"fmt"
	"log/slog"
)

// CommandContext carries the context of an incoming CMD request.
type CommandContext struct {
	Name      string         // command name (e.g. "agents", "skills")
	Args      string         // raw argument string
	ClientID  string         // requesting client ID
	SessionID string         // session ID if any
	Server    *Server        // reference to the gateway server
}

// CommandHandler handles a specific CMD command and returns a response.
// The returned string is sent back to the client as a JSON payload.
type CommandHandler func(ctx *CommandContext) (interface{}, error)

// commandResponse is the envelope sent back to the client for CMD results.
type commandResponse struct {
	Cmd      string      `json:"cmd"`
	Name     string      `json:"name"`
	Data     interface{} `json:"data,omitempty"`
	Error    string      `json:"error,omitempty"`
}

// CommandRegistry manages named command handlers.
type CommandRegistry struct {
	handlers map[string]CommandHandler
	help     map[string]string // command name -> one-line description
}

// NewCommandRegistry creates an empty registry.
func NewCommandRegistry() *CommandRegistry {
	return &CommandRegistry{
		handlers: make(map[string]CommandHandler),
		help:     make(map[string]string),
	}
}

// Register adds a named command handler with an optional help description.
func (r *CommandRegistry) Register(name string, handler CommandHandler, help string) {
	r.handlers[name] = handler
	if help != "" {
		r.help[name] = help
	}
}

// Lookup returns the handler for the given command name.
func (r *CommandRegistry) Lookup(name string) (CommandHandler, bool) {
	h, ok := r.handlers[name]
	return h, ok
}

// List returns all registered command names and their help text.
func (r *CommandRegistry) List() map[string]string {
	result := make(map[string]string, len(r.help))
	for k, v := range r.help {
		result[k] = v
	}
	return result
}

// Has returns true if the command name is registered.
func (r *CommandRegistry) Has(name string) bool {
	_, ok := r.handlers[name]
	return ok
}

// handleCommand dispatches a CMD request to the appropriate handler
// and sends the response back to the client.
func (s *Server) handleCommand(c *client, parts []string) {
	if len(parts) < 2 || parts[1] == "" {
		s.sendCommandResponse(c, "", nil, fmt.Errorf("missing command name"))
		return
	}

	name := parts[1]
	args := ""
	if len(parts) >= 3 {
		args = parts[2]
	}

	ctx := &CommandContext{
		Name:      name,
		Args:      args,
		ClientID:  c.id,
		SessionID: "",
	}

	handler, ok := s.cmdRegistry.Lookup(name)
	if !ok {
		s.sendCommandResponse(c, name, nil, fmt.Errorf("unknown command: %s", name))
		return
	}

	result, err := handler(ctx)
	if err != nil {
		s.sendCommandResponse(c, name, nil, err)
		return
	}

	s.sendCommandResponse(c, name, result, nil)
}

// sendCommandResponse marshals the result and sends it as a CMD response to the client.
func (s *Server) sendCommandResponse(c *client, name string, data interface{}, err error) {
	resp := commandResponse{
		Cmd:  "CMD",
		Name: name,
	}
	if err != nil {
		resp.Error = err.Error()
	} else {
		resp.Data = data
	}

	payload, err := json.Marshal(resp)
	if err != nil {
		slog.Error("failed to marshal command response", "error", err)
		return
	}

	s.sendText(c, string(payload))
}

// RegisterCommand registers a command handler on the server.
func (s *Server) RegisterCommand(name string, handler CommandHandler, help string) {
	s.cmdRegistry.Register(name, handler, help)
}

// CommandList returns all registered command names and their help text.
func (s *Server) CommandList() map[string]string {
	return s.cmdRegistry.List()
}
