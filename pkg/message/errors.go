package message

import "errors"

var (
	// ErrEmptyID indicates that the message ID is empty.
	ErrEmptyID = errors.New("message id cannot be empty")
	// ErrEmptyChannelID indicates that the channel ID is empty.
	ErrEmptyChannelID = errors.New("channel id cannot be empty")
	// ErrEmptyContent indicates that the message content is empty.
	ErrEmptyContent = errors.New("message content cannot be empty")
	// ErrInvalidMessageType indicates that the message type is invalid.
	ErrInvalidMessageType = errors.New("invalid message type")
)
