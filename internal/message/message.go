package message

import "time"

// Message describes the payload exchanged between peers.
type Message struct {
	MsgID     string    `json:"msg_id"`
	From      string    `json:"from"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}
