package ui

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"p2p-chat/internal/message"
)

const (
	ansiReset = "\x1b[0m"
	ansiTime  = "\x1b[36m"
	ansiName  = "\x1b[33m"
	ansiDM    = "\x1b[35m"
	ansiSys   = "\x1b[32m"
)

// CLIDisplay renders chat events to stdout.
type CLIDisplay struct {
	color bool
	mu    sync.Mutex
}

func NewCLIDisplay(color bool) *CLIDisplay {
	return &CLIDisplay{color: color}
}

func (c *CLIDisplay) ShowMessage(msg message.Message) {
	c.mu.Lock()
	defer c.mu.Unlock()
	fmt.Println(c.formatLine(msg))
}

func (c *CLIDisplay) ShowSystem(text string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ts := time.Now().Format("15:04:05")
	if c.color {
		fmt.Printf("%s[%s]%s %sSYSTEM%s: %s\n", ansiTime, ts, ansiReset, ansiSys, ansiReset, text)
		return
	}
	fmt.Printf("[%s] SYSTEM: %s\n", ts, text)
}

func (c *CLIDisplay) UpdatePeers(peers []Presence) {
	c.mu.Lock()
	defer c.mu.Unlock()
	names := make([]string, 0, len(peers))
	for _, p := range peers {
		if p.Name != "" {
			names = append(names, p.Name)
		} else {
			names = append(names, p.Addr)
		}
	}
	if len(names) == 0 {
		return
	}
	msg := fmt.Sprintf("online: %s", strings.Join(names, ", "))
	if c.color {
		fmt.Printf("%s[peers]%s %s\n", ansiSys, ansiReset, msg)
		return
	}
	fmt.Printf("[peers] %s\n", msg)
}

func (c *CLIDisplay) ShowNotification(n Notification) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ts := n.Timestamp.Format("15:04:05")
	prefix := "NOTIFY"
	if n.Level != "" {
		prefix = strings.ToUpper(n.Level)
	}
	line := fmt.Sprintf("[%s] %s: %s", ts, prefix, n.Text)
	if c.color {
		fmt.Printf("%s%s%s\n", ansiSys, line, ansiReset)
		return
	}
	fmt.Println(line)
}

func (c *CLIDisplay) formatLine(msg message.Message) string {
	ts := msg.Timestamp.Format("15:04:05")
	label := ""
	switch msg.Type {
	case "dm":
		label = " (dm)"
	case "file":
		label = " (file)"
	}
	if c.color {
		nameColor := ansiName
		if msg.Type == "dm" {
			nameColor = ansiDM
		}
		line := fmt.Sprintf("%s[%s]%s %s%s%s%s: %s", ansiTime, ts, ansiReset, nameColor, msg.From, label, ansiReset, msg.Content)
		if extras := formatAttachments(msg); extras != "" {
			line += " " + extras
		}
		return line
	}
	line := fmt.Sprintf("[%s] %s%s: %s", ts, msg.From, label, msg.Content)
	if extras := formatAttachments(msg); extras != "" {
		line += " " + extras
	}
	return line
}

// ShouldUseColor determines if ANSI coloring should be enabled for CLI output.
func ShouldUseColor(disable bool) bool {
	if disable {
		return false
	}
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return false
	}
	if runtime.GOOS == "windows" {
		if os.Getenv("WT_SESSION") != "" || os.Getenv("ANSICON") != "" || strings.EqualFold(os.Getenv("ConEmuANSI"), "ON") {
			return true
		}
		return false
	}
	return true
}

func formatAttachments(msg message.Message) string {
	if len(msg.Attachments) == 0 {
		return ""
	}
	names := make([]string, 0, len(msg.Attachments))
	for _, att := range msg.Attachments {
		if att.Name != "" {
			names = append(names, att.Name)
		} else {
			names = append(names, att.ID)
		}
	}
	return fmt.Sprintf("[files: %s]", strings.Join(names, ", "))
}
