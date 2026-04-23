package gateway

import "errors"

var (
	ErrAlreadyRunning    = errors.New("gateway is already running")
	ErrNotRunning       = errors.New("gateway is not running")
	ErrClientNotFound   = errors.New("client not found")
	ErrSessionNotFound  = errors.New("session not found")
	ErrSessionLimitReached = errors.New("session limit reached")
	ErrInvalidCommand   = errors.New("invalid command")
	ErrMaxSize          = errors.New("message exceeds max size")
	ErrTimeout          = errors.New("operation timeout")
	ErrNotConnected     = errors.New("client not connected")
)

const (
	MaxMessageSize  = 1024 * 1024
	MaxSessionTotal = 1000
	MaxSessionIdle  = 30 * 60
)

type Command string

const (
	CmdBegn  Command = "BEGN"
	CmdText  Command = "TEXT"
	CmdJSON  Command = "JSON"
	CmdFile  Command = "FILE"
	CmdFrame Command = "FRAME"
	CmdClse  Command = "CLSE"
	CmdOK    Command = "OK"
	CmdErr   Command = "ERR"
	CmdCmd   Command = "CMD"
)

var validCommands = map[Command]bool{
	CmdBegn:  true,
	CmdText:  true,
	CmdJSON:  true,
	CmdFile:  true,
	CmdFrame: true,
	CmdClse:  true,
	CmdCmd:   true,
}

type ProtocolMessage struct {
	Cmd    Command `json:"cmd,omitempty"`
	ID     string  `json:"id,omitempty"`
	SessID string  `json:"session_id,omitempty"`
	Index  int     `json:"index,omitempty"`
	Total  int     `json:"total,omitempty"`
	Data   string  `json:"data,omitempty"`
	Reason string  `json:"reason,omitempty"`
}
