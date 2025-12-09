package ui

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"p2p-chat/internal/message"
)

// TUIDisplay renders chat data using tview.
type TUIDisplay struct {
	app      *tview.Application
	messages *tview.TextView
	input    *tview.InputField
	peers    *tview.List
	send     func(string)
	once     sync.Once
}

func NewTUIDisplay(send func(string)) *TUIDisplay {
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

	td := &TUIDisplay{
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

func (t *TUIDisplay) Run(ctx context.Context) error {
	var err error
	go func() {
		<-ctx.Done()
		t.once.Do(func() {
			t.app.Stop()
		})
	}()
	err = t.app.Run()
	return err
}

func (t *TUIDisplay) ShowMessage(msg message.Message) {
	ts := msg.Timestamp.Format("15:04:05")
	label := ""
	switch msg.Type {
	case "dm":
		label = " [DM]"
	case "file":
		label = " [FILE]"
	}
	content := fmt.Sprintf("[yellow][%s][-] [lightgreen]%s%s[-]: %s", ts, msg.From, label, msg.Content)
	if len(msg.Attachments) > 0 {
		names := make([]string, 0, len(msg.Attachments))
		for _, att := range msg.Attachments {
			if att.Name != "" {
				names = append(names, att.Name)
			} else {
				names = append(names, att.ID)
			}
		}
		content += fmt.Sprintf(" [orange](files: %s)[-]", strings.Join(names, ", "))
	}
	content += "\n"
	t.app.QueueUpdateDraw(func() {
		fmt.Fprint(t.messages, content)
	})
}

func (t *TUIDisplay) ShowSystem(text string) {
	content := fmt.Sprintf("[green]>>> %s[-]\n", text)
	t.app.QueueUpdateDraw(func() {
		fmt.Fprint(t.messages, content)
	})
}

func (t *TUIDisplay) UpdatePeers(peers []Presence) {
	t.app.QueueUpdateDraw(func() {
		t.peers.Clear()
		for _, p := range peers {
			label := p.Name
			if label == "" {
				label = p.Addr
			}
			status := "offline"
			if p.Online {
				status = "online"
			}
			t.peers.AddItem(fmt.Sprintf("%s (%s)", label, status), "", 0, nil)
		}
	})
}

func (t *TUIDisplay) ShowNotification(n Notification) {
	content := fmt.Sprintf("[orange]** %s [-] %s\n", strings.ToUpper(n.Level), n.Text)
	t.app.QueueUpdateDraw(func() {
		fmt.Fprint(t.messages, content)
	})
}
