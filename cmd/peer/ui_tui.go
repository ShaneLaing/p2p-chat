package main

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"p2p-chat/internal/message"
)

type tuiDisplay struct {
	app      *tview.Application
	messages *tview.TextView
	input    *tview.InputField
	peers    *tview.List
	send     func(string)
	once     sync.Once
}

func newTUIDisplay(send func(string)) *tuiDisplay {
	messages := tview.NewTextView().
		SetDynamicColors(true).
		SetRegions(false).
		SetScrollable(true)
	messages.SetBorder(true).SetTitle("Chat")

	peers := tview.NewList()
	peers.SetBorder(true).SetTitle("Peers")

	input := tview.NewInputField().
		SetLabel("> ").
		SetFieldTextColor(tcell.ColorWhite)

	td := &tuiDisplay{
		app:      tview.NewApplication(),
		messages: messages,
		input:    input,
		peers:    peers,
		send:     send,
	}

	input.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEnter {
			text := strings.TrimSpace(input.GetText())
			if text != "" {
				go td.send(text)
			}
			input.SetText("")
		}
	})

	layout := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(messages, 0, 5, false).
		AddItem(peers, 10, 1, false).
		AddItem(input, 3, 1, true)

	td.app.SetRoot(layout, true).EnableMouse(true)
	return td
}

func (t *tuiDisplay) Run(ctx context.Context) error {
	var err error
	done := make(chan struct{})
	go func() {
		<-ctx.Done()
		t.once.Do(func() {
			t.app.Stop()
		})
	}()
	err = t.app.Run()
	close(done)
	return err
}

func (t *tuiDisplay) ShowMessage(msg message.Message) {
	ts := msg.Timestamp.Format("15:04:05")
	label := ""
	if msg.Type == msgTypeDM {
		label = " [DM]"
	}
	content := fmt.Sprintf("[yellow][%s][-] [lightgreen]%s%s[-]: %s\n", ts, msg.From, label, msg.Content)
	t.app.QueueUpdateDraw(func() {
		fmt.Fprint(t.messages, content)
	})
}

func (t *tuiDisplay) ShowSystem(text string) {
	content := fmt.Sprintf("[green]>>> %s[-]\n", text)
	t.app.QueueUpdateDraw(func() {
		fmt.Fprint(t.messages, content)
	})
}

func (t *tuiDisplay) UpdatePeers(peers []string) {
	t.app.QueueUpdateDraw(func() {
		t.peers.Clear()
		for _, p := range peers {
			t.peers.AddItem(p, "", 0, nil)
		}
	})
}
