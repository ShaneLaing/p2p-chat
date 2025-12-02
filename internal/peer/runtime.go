package peer

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"p2p-chat/internal/authutil"
	"p2p-chat/internal/message"
)

func derivePeerDir(base, addr string) string {
	if base == "" {
		base = "."
	}
	hostPart := "peer"
	portPart := "peer"
	if host, port, err := net.SplitHostPort(addr); err == nil {
		if host != "" {
			hostPart = sanitizePathToken(host)
		}
		if port != "" {
			portPart = sanitizePathToken(port)
		}
	} else if addr != "" {
		hostPart = sanitizePathToken(strings.ReplaceAll(addr, ":", "_"))
	}
	folder := fmt.Sprintf("%s-%s", hostPart, portPart)
	return filepath.Join(base, folder)
}

func sanitizePathToken(val string) string {
	val = strings.TrimSpace(val)
	if val == "" {
		return "peer"
	}
	var b strings.Builder
	for _, r := range val {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-', r == '_':
			b.WriteRune(r)
		case r == '.', r == ':':
			b.WriteRune('-')
		}
	}
	out := b.String()
	if out == "" {
		return "peer"
	}
	return out
}

func readCLIInput(app *appContext) {
	reader := bufio.NewReader(os.Stdin)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				return
			}
			log.Printf("stdin err: %v", err)
			return
		}
		processLine(app, line)
	}
}

func processLine(app *appContext, line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	if strings.HasPrefix(line, "/") {
		handleCommand(app, line)
		return
	}
	sendChatMessage(app, line)
}

func handleIncoming(app *appContext) {
	for {
		select {
		case <-app.ctx.Done():
			return
		case msg, ok := <-app.cm.Incoming:
			if !ok {
				return
			}
			app.processIncoming(msg)
		}
	}
}

func (app *appContext) processIncoming(msg message.Message) {
	if msg.MsgID == "" {
		msg.MsgID = newMsgID()
	}
	if app.cache.Seen(msg.MsgID) {
		return
	}
	if msg.Origin == "" {
		msg.Origin = msg.From
	}
	if msg.Type == "" {
		msg.Type = msgTypeChat
	}

	switch msg.Type {
	case msgTypeAck:
		if msg.AckFor != "" {
			app.ack.Confirm(msg.AckFor)
			app.metrics.IncAck()
		}
		return
	case msgTypePeerSync:
		for _, peer := range msg.PeerList {
			app.dialer.Add(peer)
		}
		return
	case msgTypeHandshake:
		if msg.AuthToken != "" {
			username, err := authutil.ValidateToken(msg.AuthToken)
			if err != nil || !strings.EqualFold(username, msg.From) {
				log.Printf("handshake rejected from %s: %v", msg.Origin, err)
				return
			}
		}
		app.directory.Record(msg.From, msg.Origin)
		app.sink.UpdatePeers(app.directory.Snapshot())
		return
	}

	app.directory.Record(msg.From, msg.Origin)

	if app.blocklist.Blocks(msg.From, msg.Origin) {
		return
	}

	if msg.ToAddr != "" && msg.ToAddr != app.selfAddr {
		app.cm.Broadcast(msg, "")
		return
	}
	if msg.To != "" && !strings.EqualFold(msg.To, app.identity.Get()) && msg.ToAddr == "" {
		app.cm.Broadcast(msg, "")
		return
	}

	app.history.Add(msg)
	if err := app.store.Append(msg); err != nil {
		log.Printf("history append: %v", err)
	}
	app.metrics.IncSeen()
	app.sink.ShowMessage(msg)
	app.maybeNotify(msg)
	sendAck(app, msg)
	app.cm.Broadcast(msg, "")
}

func sendChatMessage(app *appContext, content string) {
	msg := message.Message{
		MsgID:     newMsgID(),
		Type:      msgTypeChat,
		From:      app.identity.Get(),
		Origin:    app.selfAddr,
		Content:   content,
		Timestamp: time.Now(),
	}
	app.cache.Seen(msg.MsgID)
	app.history.Add(msg)
	if err := app.store.Append(msg); err != nil {
		log.Printf("history append: %v", err)
	}
	app.metrics.IncSent()
	app.sink.ShowMessage(msg)
	app.cm.Broadcast(msg, "")
	app.ack.Track(msg)
	app.persistExternal(msg, "")
}

func sendDirectMessage(app *appContext, target, content string) {
	addr, resolvedName, _ := app.directory.Resolve(target)
	recipient := chooseName(target, resolvedName)
	msg := message.Message{
		MsgID:     newMsgID(),
		Type:      msgTypeDM,
		From:      app.identity.Get(),
		Origin:    app.selfAddr,
		To:        recipient,
		ToAddr:    addr,
		Content:   content,
		Timestamp: time.Now(),
	}
	app.cache.Seen(msg.MsgID)
	app.history.Add(msg)
	if err := app.store.Append(msg); err != nil {
		log.Printf("history append: %v", err)
	}
	app.metrics.IncSent()
	app.sink.ShowMessage(msg)
	app.cm.Broadcast(msg, "")
	app.ack.Track(msg)
	app.persistExternal(msg, recipient)
}

func sendFileFromPath(app *appContext, path, target string) error {
	if app.files == nil || app.web == nil {
		return fmt.Errorf("file sharing requires --web")
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	record, err := app.files.Save(filepath.Base(path), app.identity.Get(), file)
	if err != nil {
		return err
	}
	return shareFile(app, record, target)
}

func chooseName(target, resolved string) string {
	if resolved != "" {
		return resolved
	}
	return target
}

func (app *appContext) persistExternal(msg message.Message, receiver string) {
	if app.authAPI == "" {
		return
	}
	token := app.identity.Token()
	if token == "" {
		return
	}
	payload := map[string]interface{}{
		"sender":  msg.From,
		"content": msg.Content,
	}
	if receiver != "" {
		payload["receiver"] = receiver
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}
	url := strings.TrimRight(app.authAPI, "/") + "/messages"
	go func(endpoint string, data []byte, tok string) {
		req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(data))
		if err != nil {
			return
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Printf("auth store: %v", err)
			return
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}(url, body, token)
}

func sendAck(app *appContext, original message.Message) {
	ackMsg := message.Message{
		MsgID:     newMsgID(),
		Type:      msgTypeAck,
		From:      app.identity.Get(),
		Origin:    app.selfAddr,
		To:        original.From,
		ToAddr:    original.Origin,
		AckFor:    original.MsgID,
		Timestamp: time.Now(),
	}
	app.cm.Broadcast(ackMsg, "")
}
