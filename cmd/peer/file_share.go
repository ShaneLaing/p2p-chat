package main

import (
	"fmt"
	"net/url"
	"time"

	"p2p-chat/internal/message"
)

// buildDownloadURL generates a peer-accessible URL for the given file record.
// When a share key exists, the URL carries it as a query param so remote peers
// can fetch without bearing the uploader's auth token.
func buildDownloadURL(web *webBridge, record fileRecord) string {
	if web == nil {
		return ""
	}
	base := fmt.Sprintf("http://%s/api/files/%s", web.addr, url.PathEscape(record.ID))
	q := url.Values{}
	if record.ShareKey != "" {
		q.Set("key", record.ShareKey)
	}
	if enc := q.Encode(); enc != "" {
		return fmt.Sprintf("%s?%s", base, enc)
	}
	return base
}

// shareFile crafts and floods a file message (optionally targeted to a single
// peer). The attachment metadata lives inside the chat history so both the CLI
// and web UI can expose downloads inline with messages.
func shareFile(app *appContext, record fileRecord, target string) error {
	if app == nil || app.web == nil {
		return fmt.Errorf("file sharing unavailable (web UI disabled)")
	}
	downloadURL := buildDownloadURL(app.web, record)
	attachment := message.Attachment{
		ID:   record.ID,
		Name: record.Name,
		Size: record.Size,
		Mime: record.Mime,
		URL:  downloadURL,
	}

	msg := message.Message{
		MsgID:       newMsgID(),
		Type:        msgTypeFile,
		From:        app.identity.Get(),
		Origin:      app.selfAddr,
		Timestamp:   time.Now(),
		Attachments: []message.Attachment{attachment},
	}

	if target != "" {
		addr, resolvedName, _ := app.directory.Resolve(target)
		recipient := chooseName(target, resolvedName)
		msg.To = recipient
		msg.ToAddr = addr
		msg.Content = fmt.Sprintf("sent a file to %s: %s", recipient, record.Name)
	} else {
		msg.Content = fmt.Sprintf("shared a file: %s", record.Name)
	}

	app.cache.Seen(msg.MsgID)
	app.history.Add(msg)
	if app.store != nil {
		if err := app.store.Append(msg); err != nil {
			app.sink.ShowSystem(fmt.Sprintf("file history append failed: %v", err))
		}
	}
	app.metrics.IncSent()
	app.sink.ShowMessage(msg)
	app.cm.Broadcast(msg, "")
	app.ack.Track(msg)
	return nil
}
