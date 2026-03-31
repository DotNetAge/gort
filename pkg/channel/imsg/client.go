// Package imsg provides a Go client for the steipete/imsg CLI tool.
// This client communicates with imsg via JSON-RPC over stdin/stdout.
//
// Requirements:
//   - macOS 14+ with Messages.app signed in
//   - imsg CLI installed: go install github.com/steipete/imsg/cmd/imsg@latest
//   - Full Disk Access for the terminal to read ~/Library/Messages/chat.db
//   - Automation permission for the terminal to control Messages.app (for sending)
//
// The imsg tool provides:
//   - List chats
//   - View message history
//   - Watch for new messages (event-driven)
//   - Send text and attachments
//   - Send tapback reactions (v0.5.0+)
//   - Typing indicators (v0.5.0+)
package imsg

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

var (
	ErrIMsgNotInstalled    = errors.New("imsg CLI not installed. Install with: go install github.com/steipete/imsg/cmd/imsg@latest")
	ErrNotRunning          = errors.New("imsg RPC server not running")
	ErrPermissionDenied    = errors.New("permission denied. Grant Full Disk Access and Automation permission in System Settings")
	ErrInvalidResponse     = errors.New("invalid response from imsg")
	ErrChatNotFound        = errors.New("chat not found")
	ErrMessageNotFound     = errors.New("message not found")
	ErrUnsupportedVersion  = errors.New("imsg version does not support this feature")
)

// Client represents a client for the imsg JSON-RPC server.
type Client struct {
	cmd      *exec.Cmd
	stdin    *json.Encoder
	stdout   *bufio.Scanner
	mu       sync.Mutex
	running  bool
	stopCh   chan struct{}
	version  string
	capabilities Capabilities
}

// Capabilities represents the features supported by the imsg version.
type Capabilities struct {
	HasTypingIndicators bool
	HasReactions        bool
	HasRPC              bool
}

// Chat represents an iMessage conversation.
type Chat struct {
	ID            int64    `json:"id"`
	Name          string   `json:"name"`
	Identifier    string   `json:"identifier"`
	Service       string   `json:"service"` // "iMessage" or "SMS"
	LastMessageAt string   `json:"last_message_at"`
	IsGroup       bool     `json:"is_group"`
	Participants  []string `json:"participants,omitempty"`
}

// Message represents an iMessage/SMS message.
type Message struct {
	ID                   int64        `json:"id"`
	ChatID               int64        `json:"chat_id"`
	GUID                 string       `json:"guid"`
	ReplyToGUID          string       `json:"reply_to_guid,omitempty"`
	DestinationCallerID  string       `json:"destination_caller_id,omitempty"`
	Sender               string       `json:"sender"`
	IsFromMe             bool         `json:"is_from_me"`
	Text                 string       `json:"text"`
	CreatedAt            string       `json:"created_at"`
	Attachments          []Attachment `json:"attachments,omitempty"`
	IsReaction           bool         `json:"is_reaction,omitempty"`
	ReactionType         string       `json:"reaction_type,omitempty"`
	ReactionEmoji        string       `json:"reaction_emoji,omitempty"`
	IsReactionAdd        bool         `json:"is_reaction_add,omitempty"`
	ReactedToGUID        string       `json:"reacted_to_guid,omitempty"`
	ThreadOriginatorGUID string       `json:"thread_originator_guid,omitempty"`
}

// Attachment represents a message attachment.
type Attachment struct {
	Filename      string `json:"filename"`
	TransferName  string `json:"transfer_name"`
	UTI           string `json:"uti"`
	MIMEType      string `json:"mime_type"`
	TotalBytes    int64  `json:"total_bytes"`
	IsSticker     bool   `json:"is_sticker"`
	OriginalPath  string `json:"original_path"`
	Missing       bool   `json:"missing"`
}

// NewClient creates a new imsg client.
func NewClient() (*Client, error) {
	// Check if imsg is installed
	if _, err := exec.LookPath("imsg"); err != nil {
		return nil, ErrIMsgNotInstalled
	}

	return &Client{
		stopCh: make(chan struct{}),
	}, nil
}

// Start starts the imsg RPC server.
func (c *Client) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.running {
		return nil
	}

	cmd := exec.CommandContext(ctx, "imsg", "rpc")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start imsg rpc: %w", err)
	}

	c.cmd = cmd
	c.stdin = json.NewEncoder(stdin)
	c.stdout = bufio.NewScanner(stdout)
	c.running = true

	// Detect version and capabilities
	if err := c.detectCapabilities(ctx); err != nil {
		c.Stop()
		return err
	}

	return nil
}

// Stop stops the imsg RPC server.
func (c *Client) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.running {
		return nil
	}

	close(c.stopCh)
	c.running = false

	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
		_ = c.cmd.Wait()
	}

	return nil
}

// IsRunning returns true if the RPC server is running.
func (c *Client) IsRunning() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.running
}

// GetCapabilities returns the capabilities of the imsg version.
func (c *Client) GetCapabilities() Capabilities {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.capabilities
}

// detectCapabilities detects the imsg version and capabilities.
func (c *Client) detectCapabilities(ctx context.Context) error {
	// Try to call a method that exists in all versions
	var result struct {
		Version string `json:"version"`
	}
	
	if err := c.call(ctx, "version", nil, &result); err != nil {
		// If version method doesn't exist, assume basic capabilities
		c.capabilities = Capabilities{
			HasRPC: true,
		}
		return nil
	}

	c.version = result.Version

	// Parse version to determine capabilities
	// v0.5.0+ has typing indicators and reactions
	c.capabilities = Capabilities{
		HasRPC: true,
	}

	if c.version >= "0.5.0" {
		c.capabilities.HasTypingIndicators = true
		c.capabilities.HasReactions = true
	}

	return nil
}

// ListChats returns a list of recent chats.
func (c *Client) ListChats(ctx context.Context, limit int) ([]Chat, error) {
	if !c.IsRunning() {
		return nil, ErrNotRunning
	}

	params := map[string]interface{}{
		"limit": limit,
	}

	var result []Chat
	if err := c.call(ctx, "chats", params, &result); err != nil {
		return nil, err
	}

	return result, nil
}

// GetHistory returns message history for a chat.
func (c *Client) GetHistory(ctx context.Context, chatID int64, limit int) ([]Message, error) {
	if !c.IsRunning() {
		return nil, ErrNotRunning
	}

	params := map[string]interface{}{
		"chat_id": chatID,
		"limit":   limit,
	}

	var result []Message
	if err := c.call(ctx, "history", params, &result); err != nil {
		return nil, err
	}

	return result, nil
}

// SendMessage sends a text message.
func (c *Client) SendMessage(ctx context.Context, to, text string, service string) error {
	if !c.IsRunning() {
		return ErrNotRunning
	}

	params := map[string]interface{}{
		"to":      to,
		"text":    text,
		"service": service,
	}

	var result interface{}
	if err := c.call(ctx, "send", params, &result); err != nil {
		return err
	}

	return nil
}

// SendFile sends a file attachment.
func (c *Client) SendFile(ctx context.Context, to, filePath, text, service string) error {
	if !c.IsRunning() {
		return ErrNotRunning
	}

	params := map[string]interface{}{
		"to":      to,
		"file":    filePath,
		"text":    text,
		"service": service,
	}

	var result interface{}
	if err := c.call(ctx, "send", params, &result); err != nil {
		return err
	}

	return nil
}

// SendReaction sends a tapback reaction (requires imsg v0.5.0+).
func (c *Client) SendReaction(ctx context.Context, chatID int64, messageGUID, reactionType string) error {
	if !c.IsRunning() {
		return ErrNotRunning
	}

	if !c.capabilities.HasReactions {
		return ErrUnsupportedVersion
	}

	params := map[string]interface{}{
		"chat_id":       chatID,
		"message_guid":  messageGUID,
		"reaction_type": reactionType,
	}

	var result interface{}
	if err := c.call(ctx, "react", params, &result); err != nil {
		return err
	}

	return nil
}

// SendTyping sends a typing indicator (requires imsg v0.5.0+).
func (c *Client) SendTyping(ctx context.Context, chatID int64, typing bool) error {
	if !c.IsRunning() {
		return ErrNotRunning
	}

	if !c.capabilities.HasTypingIndicators {
		return ErrUnsupportedVersion
	}

	method := "typing.stop"
	if typing {
		method = "typing.start"
	}

	params := map[string]interface{}{
		"chat_id": chatID,
	}

	var result interface{}
	if err := c.call(ctx, method, params, &result); err != nil {
		return err
	}

	return nil
}

// Watch starts watching for new messages.
func (c *Client) Watch(ctx context.Context, chatID int64, sinceRowID int64, includeReactions bool) (<-chan Message, error) {
	if !c.IsRunning() {
		return nil, ErrNotRunning
	}

	params := map[string]interface{}{
		"chat_id":           chatID,
		"since_rowid":       sinceRowID,
		"include_reactions": includeReactions,
	}

	// Start watch in a goroutine
	msgCh := make(chan Message)
	
	go func() {
		defer close(msgCh)

		// Send watch request
		if err := c.call(ctx, "watch.subscribe", params, nil); err != nil {
			return
		}

		// Read messages from stdout
		for {
			select {
			case <-ctx.Done():
				return
			case <-c.stopCh:
				return
			default:
				if !c.stdout.Scan() {
					return
				}

				line := c.stdout.Text()
				if line == "" {
					continue
				}

				var msg Message
				if err := json.Unmarshal([]byte(line), &msg); err != nil {
					continue
				}

				select {
				case msgCh <- msg:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return msgCh, nil
}

// call makes a JSON-RPC call to the imsg server.
func (c *Client) call(ctx context.Context, method string, params interface{}, result interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.running {
		return ErrNotRunning
	}

	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  method,
		"id":      time.Now().UnixNano(),
	}

	if params != nil {
		req["params"] = params
	}

	if err := c.stdin.Encode(req); err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}

	// For notifications (result is nil), don't wait for response
	if result == nil {
		return nil
	}

	// Wait for response with timeout
	done := make(chan error, 1)
	go func() {
		if !c.stdout.Scan() {
			done <- errors.New("failed to read response")
			return
		}

		line := c.stdout.Text()
		
		var resp struct {
			JSONRPC string          `json:"jsonrpc"`
			Result  json.RawMessage `json:"result"`
			Error   *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
			ID interface{} `json:"id"`
		}

		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			done <- fmt.Errorf("failed to parse response: %w", err)
			return
		}

		if resp.Error != nil {
			errMsg := resp.Error.Message
			if strings.Contains(errMsg, "permission") {
				done <- ErrPermissionDenied
			} else if strings.Contains(errMsg, "not found") {
				done <- ErrChatNotFound
			} else {
				done <- fmt.Errorf("imsg error: %s", errMsg)
			}
			return
		}

		if result != nil && resp.Result != nil {
			if err := json.Unmarshal(resp.Result, result); err != nil {
				done <- fmt.Errorf("failed to unmarshal result: %w", err)
				return
			}
		}

		done <- nil
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		return err
	case <-time.After(30 * time.Second):
		return errors.New("request timeout")
	}
}
