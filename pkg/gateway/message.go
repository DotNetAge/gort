package gateway

import "time"

type Message struct {
	ID          string    `json:"id"`
	ChannelID   string    `json:"channel_id"`
	ClientID    string    `json:"client_id"`
	SessionID   string    `json:"session_id"`
	Direction   Direction `json:"direction"`
	Data        []byte    `json:"data,omitempty"`
	ContentType string    `json:"content_type,omitempty"`
	Timestamp   time.Time `json:"timestamp"`
}

func (m *Message) Text() string { return string(m.Data) }
