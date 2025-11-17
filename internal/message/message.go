package message

import "time"

// Message describes the payload exchanged between peers.
type Message struct {
	MsgID     string    `json:"msg_id"`
	Type      string    `json:"type"`
	From      string    `json:"from"`
	Origin    string    `json:"origin"`
	To        string    `json:"to,omitempty"`
	ToAddr    string    `json:"to_addr,omitempty"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
	AckFor    string    `json:"ack_for,omitempty"`
	PeerList  []string  `json:"peer_list,omitempty"`
}
