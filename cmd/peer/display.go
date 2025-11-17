package main

import "p2p-chat/internal/message"

// displaySink fanouts chat events to different surfaces (CLI, TUI, Web UI).
type displaySink interface {
	ShowMessage(message.Message)
	ShowSystem(string)
	UpdatePeers([]string)
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

func (m *multiSink) UpdatePeers(peers []string) {
	for _, sink := range m.sinks {
		if sink != nil {
			sink.UpdatePeers(peers)
		}
	}
}
