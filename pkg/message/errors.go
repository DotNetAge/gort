// Package message provides error definitions for message validation and processing.
//
// These errors are returned by the Message.Validate() method and other
// message-related operations to indicate specific validation failures.
//
// Error Handling:
//
//	msg := message.NewMessage("", "channel", message.DirectionInbound, from, "content", message.MessageTypeText)
//	if err := msg.Validate(); err != nil {
//	    switch err {
//	    case message.ErrEmptyID:
//	        // Handle missing message ID
//	    case message.ErrEmptyChannelID:
//	        // Handle missing channel ID
//	    case message.ErrEmptyContent:
//	        // Handle missing content
//	    case message.ErrInvalidMessageType:
//	        // Handle invalid message type
//	    }
//	}
package message

import "errors"

var (
	// ErrEmptyID indicates that the message ID is empty.
	// The ID field is required for message tracking and deduplication.
	ErrEmptyID = errors.New("message id cannot be empty")

	// ErrEmptyChannelID indicates that the channel ID is empty.
	// The ChannelID field is required to identify the source or destination channel.
	ErrEmptyChannelID = errors.New("channel id cannot be empty")

	// ErrEmptyContent indicates that the message content is empty.
	// Most message types require non-empty content.
	// This error is not returned for MessageTypeEvent messages.
	ErrEmptyContent = errors.New("message content cannot be empty")

	// ErrInvalidMessageType indicates that the message type is invalid.
	// The Type field must be one of the defined MessageType constants.
	ErrInvalidMessageType = errors.New("invalid message type")
)
