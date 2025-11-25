package peer

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"p2p-chat/internal/message"
)

func (a *App) StartCLI() displaySink {
	return newCLIDisplay(shouldUseColor(a.Cfg.NoColor))
}

type cliDisplay struct {
	color bool
	mu    sync.Mutex
}

func newCLIDisplay(color bool) *cliDisplay {
	return &cliDisplay{color: color}
}

func (c *cliDisplay) ShowMessage(msg message.Message) {
	c.mu.Lock()
	defer c.mu.Unlock()
	fmt.Println(c.formatLine(msg))
}

func (c *cliDisplay) ShowSystem(text string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ts := time.Now().Format("15:04:05")
	if c.color {
		fmt.Printf("%s[%s]%s %sSYSTEM%s: %s\n", ansiTime, ts, ansiReset, ansiSys, ansiReset, text)
		return
	}
	fmt.Printf("[%s] SYSTEM: %s\n", ts, text)
}

func (c *cliDisplay) UpdatePeers(peers []peerPresence) {
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

func (c *cliDisplay) ShowNotification(n notificationPayload) {
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

const (
	ansiReset = "\x1b[0m"
	ansiTime  = "\x1b[36m"
	ansiName  = "\x1b[33m"
	ansiDM    = "\x1b[35m"
	ansiSys   = "\x1b[32m"
)

func (c *cliDisplay) formatLine(msg message.Message) string {
	ts := msg.Timestamp.Format("15:04:05")
	label := ""
	if msg.Type == msgTypeDM {
		label = " (dm)"
	}
	if c.color {
		nameColor := ansiName
		if msg.Type == msgTypeDM {
			nameColor = ansiDM
		}
		return fmt.Sprintf("%s[%s]%s %s%s%s%s: %s", ansiTime, ts, ansiReset, nameColor, msg.From, label, ansiReset, msg.Content)
	}
	return fmt.Sprintf("[%s] %s%s: %s", ts, msg.From, label, msg.Content)
}

func shouldUseColor(disable bool) bool {
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
