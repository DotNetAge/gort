package gateway

import "errors"

var (
	ErrAlreadyRunning  = errors.New("gateway is already running")
	ErrNotRunning      = errors.New("gateway is not running")
	ErrClientNotFound  = errors.New("client not found")
	ErrSessionNotFound = errors.New("session not found")
	ErrInvalidCommand  = errors.New("invalid command")
)

type Direction string

const (
	DirectionInbound  Direction = "inbound"
	DirectionOutbound Direction = "outbound"
)

type Command string

const (
	CmdSessionStart Command = "SESSION_START"
	CmdData         Command = "DATA"
	CmdSessionEnd   Command = "SESSION_END"
	CmdOK           Command = "OK"
	CmdAck          Command = "ACK"
)

const (
	MaxMessageSize  = 1024 * 1024
	MaxSessionTotal = 1000
)

var validCommands = map[Command]bool{
	CmdSessionStart: true,
	CmdData:         true,
	CmdSessionEnd:   true,
}
