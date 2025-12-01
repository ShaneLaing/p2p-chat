package main

import (
	"time"

	"p2p-chat/internal/message"
)

type peerPresence struct {
	Name   string `json:"name"`
	Addr   string `json:"addr"`
	Online bool   `json:"online"`
}

// displaySink fanouts chat events to different surfaces (CLI, TUI, Web UI).
type displaySink interface {
	ShowMessage(message.Message)
	ShowSystem(string)
	UpdatePeers([]peerPresence)
	ShowNotification(notificationPayload)
}

type multiSink struct {
	sinks []displaySink
}

func newMultiSink(sinks ...displaySink) displaySink {
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

func (m *multiSink) UpdatePeers(peers []peerPresence) {
	for _, sink := range m.sinks {
		if sink != nil {
			sink.UpdatePeers(peers)
		}
	}
}

func (m *multiSink) ShowNotification(n notificationPayload) {
	for _, sink := range m.sinks {
		if sink != nil {
			sink.ShowNotification(n)
		}
	}
}

// notificationPayload provides a consistent structure for mentions, DMs, etc.
type notificationPayload struct {
	ID        string    `json:"id"`
	Text      string    `json:"text"`
	Level     string    `json:"level"`
	Timestamp time.Time `json:"timestamp"`
	From      string    `json:"from"`
}
