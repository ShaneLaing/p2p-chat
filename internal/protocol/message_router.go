package protocol

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"p2p-chat/internal/authutil"
	"p2p-chat/internal/message"
	"p2p-chat/internal/storage"
	"p2p-chat/internal/ui"
)

func (r *Runtime) ReadCLIInput(reader io.Reader) {
	buf := bufio.NewReader(reader)
	for {
		line, err := buf.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				return
			}
			log.Printf("stdin err: %v", err)
			return
		}
		r.ProcessLine(line)
	}
}

func (r *Runtime) ProcessLine(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	if strings.HasPrefix(line, "/") {
		r.handleCommand(line)
		return
	}
	r.sendChatMessage(line)
}

func (r *Runtime) handleCommand(line string) {
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return
	}
	switch parts[0] {
	case "/peers":
		conns := r.cm.ConnsList()
		desired := r.dialer.Desired()
		r.sink.ShowSystem(fmt.Sprintf("connected: %v | desired: %v", conns, desired))
	case "/history":
		for _, msg := range r.history.All() {
			r.sink.ShowMessage(msg)
		}
	case "/save":
		if len(parts) < 2 {
			r.sink.ShowSystem("usage: /save <path>")
			return
		}
		if err := saveHistoryToFile(r.history.All(), parts[1]); err != nil {
			r.sink.ShowSystem(fmt.Sprintf("save failed: %v", err))
			return
		}
		r.sink.ShowSystem("history saved")
	case "/load":
		limit := 20
		if len(parts) >= 2 {
			if v, err := strconv.Atoi(parts[1]); err == nil {
				limit = v
			}
		}
		if r.store == nil {
			r.sink.ShowSystem("history persistence disabled")
			return
		}
		records, err := r.store.Recent(limit)
		if err != nil {
			r.sink.ShowSystem(fmt.Sprintf("load failed: %v", err))
			return
		}
		for i := len(records) - 1; i >= 0; i-- {
			r.sink.ShowMessage(records[i])
		}
	case "/msg":
		if len(parts) < 3 {
			r.sink.ShowSystem("usage: /msg <target> <message>")
			return
		}
		target := parts[1]
		idx := strings.Index(line, target)
		content := strings.TrimSpace(line[idx+len(target):])
		if content == "" {
			r.sink.ShowSystem("message required")
			return
		}
		r.sendDirectMessage(target, content)
	case "/file":
		if len(parts) < 2 {
			r.sink.ShowSystem("usage: /file <path> [target]")
			return
		}
		target := ""
		if len(parts) >= 3 {
			target = parts[2]
		}
		if err := r.SendFileFromPath(parts[1], target); err != nil {
			r.sink.ShowSystem(fmt.Sprintf("file send failed: %v", err))
		}
	case "/nick":
		if len(parts) < 2 {
			r.sink.ShowSystem("usage: /nick <name>")
			return
		}
		if r.identity.SetDisplay(parts[1]) {
			r.sink.ShowSystem(fmt.Sprintf("nickname set to %s", parts[1]))
			r.BroadcastHandshake()
		}
	case "/stats":
		snap := r.metrics.Snapshot()
		r.sink.ShowSystem(snap.String())
	case "/block":
		if len(parts) < 2 {
			r.sink.ShowSystem("usage: /block <name|addr>")
			return
		}
		r.blocklist.Add(parts[1])
		r.sink.ShowSystem(fmt.Sprintf("blocked %s", parts[1]))
	case "/unblock":
		if len(parts) < 2 {
			r.sink.ShowSystem("usage: /unblock <name|addr>")
			return
		}
		r.blocklist.Remove(parts[1])
		r.sink.ShowSystem(fmt.Sprintf("unblocked %s", parts[1]))
	case "/blocked":
		r.sink.ShowSystem(fmt.Sprintf("blocked: %v", r.blocklist.List()))
	case "/quit":
		r.sink.ShowSystem("bye")
		os.Exit(0)
	default:
		r.sink.ShowSystem("commands: /peers /history /save /load /msg /file /nick /stats /block /unblock /blocked /quit")
	}
}

func (r *Runtime) HandleIncoming() {
	for {
		select {
		case <-r.ctx.Done():
			return
		case msg, ok := <-r.cm.Incoming:
			if !ok {
				return
			}
			r.processIncoming(msg)
		}
	}
}

func (r *Runtime) processIncoming(msg message.Message) {
	if msg.MsgID == "" {
		msg.MsgID = NewMsgID()
	}
	if r.cache.Seen(msg.MsgID) {
		return
	}
	if msg.Origin == "" {
		msg.Origin = msg.From
	}
	if msg.Type == "" {
		msg.Type = MsgTypeChat
	}

	switch msg.Type {
	case MsgTypeAck:
		if msg.AckFor != "" {
			r.ack.Confirm(msg.AckFor)
			r.metrics.IncAck()
		}
		return
	case MsgTypePeerSync:
		for _, peer := range msg.PeerList {
			r.dialer.Add(peer)
		}
		return
	case MsgTypeHandshake:
		if msg.AuthToken != "" {
			username, err := authutil.ValidateToken(msg.AuthToken)
			if err != nil || !strings.EqualFold(username, msg.From) {
				log.Printf("handshake rejected from %s: %v", msg.Origin, err)
				return
			}
		}
		r.directory.Record(msg.From, msg.Origin)
		r.sink.UpdatePeers(r.directory.Snapshot())
		return
	}

	r.directory.Record(msg.From, msg.Origin)

	if r.blocklist.Blocks(msg.From, msg.Origin) {
		return
	}

	if msg.ToAddr != "" && msg.ToAddr != r.selfAddr {
		r.cm.Broadcast(msg, "")
		return
	}
	if msg.To != "" && !strings.EqualFold(msg.To, r.identity.Get()) && msg.ToAddr == "" {
		r.cm.Broadcast(msg, "")
		return
	}

	r.history.Add(msg)
	if err := r.store.Append(msg); err != nil {
		log.Printf("history append: %v", err)
	}
	r.metrics.IncSeen()
	r.sink.ShowMessage(msg)
	r.maybeNotify(msg)
	r.sendAck(msg)
	r.cm.Broadcast(msg, "")
}

func (r *Runtime) sendChatMessage(content string) {
	msg := message.Message{
		MsgID:     NewMsgID(),
		Type:      MsgTypeChat,
		From:      r.identity.Get(),
		Origin:    r.selfAddr,
		Content:   content,
		Timestamp: time.Now(),
	}
	r.cache.Seen(msg.MsgID)
	r.history.Add(msg)
	if err := r.store.Append(msg); err != nil {
		log.Printf("history append: %v", err)
	}
	r.metrics.IncSent()
	r.sink.ShowMessage(msg)
	r.cm.Broadcast(msg, "")
	r.ack.Track(msg)
	r.persistExternal(msg, "")
}

func (r *Runtime) sendDirectMessage(target, content string) {
	addr, resolvedName, _ := r.directory.Resolve(target)
	recipient := chooseName(target, resolvedName)
	msg := message.Message{
		MsgID:     NewMsgID(),
		Type:      MsgTypeDM,
		From:      r.identity.Get(),
		Origin:    r.selfAddr,
		To:        recipient,
		ToAddr:    addr,
		Content:   content,
		Timestamp: time.Now(),
	}
	r.cache.Seen(msg.MsgID)
	r.history.Add(msg)
	if err := r.store.Append(msg); err != nil {
		log.Printf("history append: %v", err)
	}
	r.metrics.IncSent()
	r.sink.ShowMessage(msg)
	r.cm.Broadcast(msg, "")
	r.ack.Track(msg)
	r.persistExternal(msg, recipient)
}

func (r *Runtime) SendFileFromPath(path, target string) error {
	if r.files == nil || r.web == nil {
		return fmt.Errorf("file sharing requires --web")
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	record, err := r.files.Save(filepath.Base(path), r.identity.Get(), file)
	if err != nil {
		return err
	}
	return r.ShareFile(record, target)
}

func chooseName(target, resolved string) string {
	if resolved != "" {
		return resolved
	}
	return target
}

func (r *Runtime) persistExternal(msg message.Message, receiver string) {
	if r.authAPI == "" {
		return
	}
	token := r.identity.Token()
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
	url := strings.TrimRight(r.authAPI, "/") + "/messages"
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

func (r *Runtime) sendAck(original message.Message) {
	ackMsg := message.Message{
		MsgID:     NewMsgID(),
		Type:      MsgTypeAck,
		From:      r.identity.Get(),
		Origin:    r.selfAddr,
		To:        original.From,
		ToAddr:    original.Origin,
		AckFor:    original.MsgID,
		Timestamp: time.Now(),
	}
	r.cm.Broadcast(ackMsg, "")
}

func (r *Runtime) BroadcastHandshake() {
	name := r.identity.Get()
	if name == "" {
		return
	}
	msg := message.Message{
		MsgID:     NewMsgID(),
		Type:      MsgTypeHandshake,
		From:      name,
		Origin:    r.selfAddr,
		AuthToken: r.identity.Token(),
		Timestamp: time.Now(),
	}
	r.cm.Broadcast(msg, "")
}

func (r *Runtime) maybeNotify(msg message.Message) {
	self := r.identity.Get()
	if self == "" || strings.EqualFold(msg.From, self) {
		return
	}
	n := ui.Notification{
		ID:        msg.MsgID,
		From:      msg.From,
		Timestamp: time.Now(),
	}
	if msg.Type == MsgTypeDM {
		if strings.EqualFold(msg.To, self) || strings.EqualFold(msg.ToAddr, r.selfAddr) {
			n.Level = "dm"
			n.Text = fmt.Sprintf("%s sent you a direct message", msg.From)
			r.sink.ShowNotification(n)
		}
		return
	}
	content := strings.ToLower(msg.Content)
	needle := strings.ToLower(self)
	if content != "" && strings.Contains(content, needle) {
		n.Level = "mention"
		n.Text = fmt.Sprintf("%s mentioned you", msg.From)
		r.sink.ShowNotification(n)
	}
}

func saveHistoryToFile(entries []message.Message, path string) error {
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func (r *Runtime) ShareFile(record storage.FileRecord, target string) error {
	if r.web == nil {
		return fmt.Errorf("file sharing unavailable (web UI disabled)")
	}
	downloadURL := r.buildDownloadURL(record)
	attachment := message.Attachment{
		ID:   record.ID,
		Name: record.Name,
		Size: record.Size,
		Mime: record.Mime,
		URL:  downloadURL,
	}

	msg := message.Message{
		MsgID:       NewMsgID(),
		Type:        MsgTypeFile,
		From:        r.identity.Get(),
		Origin:      r.selfAddr,
		Timestamp:   time.Now(),
		Attachments: []message.Attachment{attachment},
	}

	if target != "" {
		addr, resolvedName, _ := r.directory.Resolve(target)
		recipient := chooseName(target, resolvedName)
		msg.To = recipient
		msg.ToAddr = addr
		msg.Content = fmt.Sprintf("sent a file to %s: %s", recipient, record.Name)
	} else {
		msg.Content = fmt.Sprintf("shared a file: %s", record.Name)
	}

	r.cache.Seen(msg.MsgID)
	r.history.Add(msg)
	if r.store != nil {
		if err := r.store.Append(msg); err != nil {
			r.sink.ShowSystem(fmt.Sprintf("file history append failed: %v", err))
		}
	}
	r.metrics.IncSent()
	r.sink.ShowMessage(msg)
	r.cm.Broadcast(msg, "")
	r.ack.Track(msg)
	return nil
}

func (r *Runtime) buildDownloadURL(record storage.FileRecord) string {
	if r.web == nil {
		return ""
	}
	base := fmt.Sprintf("http://%s/api/files/%s", r.web.Addr(), url.PathEscape(record.ID))
	q := url.Values{}
	if record.ShareKey != "" {
		q.Set("key", record.ShareKey)
	}
	if enc := q.Encode(); enc != "" {
		return fmt.Sprintf("%s?%s", base, enc)
	}
	return base
}
