package ui

import (
	"time"

	"p2p-chat/internal/message"
)

// Presence describes the availability of a peer so each UI can display it.
type Presence struct {
	Name   string `json:"name"`
	Addr   string `json:"addr"`
	Online bool   `json:"online"`
}

// Notification is used for system level alerts such as mentions or DMs.
type Notification struct {
	ID        string    `json:"id"`
	Text      string    `json:"text"`
	Level     string    `json:"level"`
	Timestamp time.Time `json:"timestamp"`
	From      string    `json:"from"`
}

// Sink is the unified interface every UI surface must satisfy.
type Sink interface {
	ShowMessage(message.Message)
	ShowSystem(string)
	UpdatePeers([]Presence)
	ShowNotification(Notification)
}

type multiSink struct {
	sinks []Sink
}

// NewMultiSink fans chat events out to each registered sink.
func NewMultiSink(sinks ...Sink) Sink {
	return &multiSink{sinks: sinks}
}

func (m *multiSink) ShowMessage(msg message.Message) {
	for _, sink := range m.sinks {
		if sink != nil {
			sink.ShowMessage(msg)
		}
	}
}

func (m *multiSink) ShowSystem(text string) {
	for _, sink := range m.sinks {
		if sink != nil {
			sink.ShowSystem(text)
		}
	}
}

func (m *multiSink) UpdatePeers(peers []Presence) {
	for _, sink := range m.sinks {
		if sink != nil {
			sink.UpdatePeers(peers)
		}
	}
}

func (m *multiSink) ShowNotification(n Notification) {
	for _, sink := range m.sinks {
		if sink != nil {
			sink.ShowNotification(n)
		}
	}
}
