package peer

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"p2p-chat/internal/authutil"
	"p2p-chat/internal/message"

	"github.com/gorilla/websocket"
)

//go:embed webui/static
var webFS embed.FS

type webBridge struct {
	addr       string
	srv        *http.Server
	upgrader   websocket.Upgrader
	history    *historyBuffer
	submit     func(string)
	files      *fileStore
	share      func(fileRecord, string) error
	clientsMu  sync.Mutex
	clients    map[*websocket.Conn]struct{}
	sseMu      sync.Mutex
	sseClients map[chan webEvent]struct{}
	staticFS   http.Handler
	onSession  func(string, string) error
}

const maxUploadBytes = 25 << 20

func newWebBridge(addr string, history *historyBuffer, submit func(string), onSession func(string, string) error, files *fileStore, share func(fileRecord, string) error) (*webBridge, error) {
	sub, err := fs.Sub(webFS, "webui/static")
	if err != nil {
		return nil, err
	}
	wb := &webBridge{
		addr:       addr,
		history:    history,
		submit:     submit,
		files:      files,
		share:      share,
		clients:    make(map[*websocket.Conn]struct{}),
		sseClients: make(map[chan webEvent]struct{}),
		staticFS:   http.StripPrefix("/static/", http.FileServer(http.FS(sub))),
		onSession:  onSession,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", wb.handleIndex)
	mux.HandleFunc("/chat", wb.handleChat)
	mux.Handle("/static/", wb.staticFS)
	mux.HandleFunc("/ws", wb.handleWS)
	mux.HandleFunc("/events", wb.handleSSE)
	mux.HandleFunc("/api/files", wb.handleFiles)
	mux.HandleFunc("/api/files/", wb.handleFileDownload)
	mux.HandleFunc("/api/push/subscribe", wb.handlePushSubscribe)
	wb.srv = &http.Server{Addr: addr, Handler: mux}
	return wb, nil
}

func (wb *webBridge) Run(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		<-ctx.Done()
		_ = wb.srv.Shutdown(context.Background())
	}()
	log.Printf("web ui listening on http://%s", wb.addr)
	if err := wb.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Printf("web ui error: %v", err)
	}
	cancel()
}

func (wb *webBridge) Close() {
	_ = wb.srv.Shutdown(context.Background())
	wb.clientsMu.Lock()
	for conn := range wb.clients {
		_ = conn.Close()
	}
	wb.clientsMu.Unlock()
	wb.sseMu.Lock()
	for ch := range wb.sseClients {
		close(ch)
		delete(wb.sseClients, ch)
	}
	wb.sseMu.Unlock()
}

func (wb *webBridge) handleIndex(w http.ResponseWriter, r *http.Request) {
	wb.serveHTML(w, "webui/static/index.html")
}

func (wb *webBridge) handleChat(w http.ResponseWriter, r *http.Request) {
	wb.serveHTML(w, "webui/static/app.html")
}

func (wb *webBridge) handleFiles(w http.ResponseWriter, r *http.Request) {
	if wb.files == nil {
		http.Error(w, "file storage disabled", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodGet:
		wb.listFiles(w, r)
	case http.MethodPost:
		wb.uploadFile(w, r)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (wb *webBridge) handleFileDownload(w http.ResponseWriter, r *http.Request) {
	if wb.files == nil {
		http.Error(w, "file storage disabled", http.StatusServiceUnavailable)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/files/")
	if id == "" {
		http.NotFound(w, r)
		return
	}
	entry, file, err := wb.files.Open(id)
	if err != nil {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}
	defer file.Close()
	authorized := false
	if key := r.URL.Query().Get("key"); key != "" && entry.ShareKey != "" && key == entry.ShareKey {
		authorized = true
	}
	if !authorized {
		if _, err := wb.requireAuth(r); err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
	}
	filename := entry.Name
	contentType := entry.Mime
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.FormatInt(entry.Size, 10))
	w.Header().Set("X-Filename", filename)
	disposition := "inline"
	if strings.EqualFold(r.URL.Query().Get("download"), "1") {
		disposition = "attachment"
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("%s; filename=\"%s\"", disposition, url.PathEscape(filename)))
	if _, err := io.Copy(w, file); err != nil {
		log.Printf("file download %s: %v", id, err)
	}
}

func (wb *webBridge) listFiles(w http.ResponseWriter, r *http.Request) {
	if _, err := wb.requireAuth(r); err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	records, err := wb.files.List(100)
	if err != nil {
		http.Error(w, "unable to list files", http.StatusInternalServerError)
		return
	}
	wb.writeJSON(w, http.StatusOK, records)
}

func (wb *webBridge) uploadFile(w http.ResponseWriter, r *http.Request) {
	username, err := wb.requireAuth(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		http.Error(w, "invalid upload", http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "missing file", http.StatusBadRequest)
		return
	}
	defer file.Close()
	target := strings.TrimSpace(r.FormValue("target"))
	record, err := wb.files.Save(header.Filename, username, file)
	if err != nil {
		http.Error(w, "upload failed", http.StatusInternalServerError)
		return
	}
	wb.writeJSON(w, http.StatusCreated, record)
	wb.broadcastFile(record)
	if wb.share != nil {
		if err := wb.share(record, target); err != nil {
			log.Printf("share file broadcast: %v", err)
		}
	}
	wb.sendEvent(webEvent{Kind: "notification", Notification: notificationPayload{
		ID:        record.ID,
		From:      username,
		Level:     "file",
		Text:      fmt.Sprintf("%s uploaded %s", username, record.Name),
		Timestamp: time.Now(),
	}})
}

func (wb *webBridge) handlePushSubscribe(w http.ResponseWriter, r *http.Request) {
	if _, err := wb.requireAuth(r); err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	io.Copy(io.Discard, r.Body)
	w.WriteHeader(http.StatusAccepted)
}

func (wb *webBridge) requireAuth(r *http.Request) (string, error) {
	if token := r.URL.Query().Get("token"); token != "" {
		username := r.URL.Query().Get("username")
		resolved, err := authutil.ValidateToken(token)
		if err != nil {
			return "", err
		}
		if username != "" && !strings.EqualFold(username, resolved) {
			return "", fmt.Errorf("username mismatch")
		}
		return resolved, nil
	}
	authHeader := r.Header.Get("Authorization")
	parts := strings.Fields(authHeader)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return "", fmt.Errorf("missing authorization")
	}
	username, err := authutil.ValidateToken(parts[1])
	if err != nil {
		return "", err
	}
	return username, nil
}

func (wb *webBridge) writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("json write: %v", err)
	}
}

func (wb *webBridge) broadcastFile(record fileRecord) {
	wb.sendEvent(webEvent{Kind: "file", File: record})
}

func (wb *webBridge) serveHTML(w http.ResponseWriter, path string) {
	data, err := webFS.ReadFile(path)
	if err != nil {
		http.Error(w, "missing assets", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
}

func (wb *webBridge) handleWS(w http.ResponseWriter, r *http.Request) {
	username := r.URL.Query().Get("username")
	token := r.URL.Query().Get("token")
	if username == "" || token == "" {
		http.Error(w, "missing credentials", http.StatusUnauthorized)
		return
	}
	resolved, err := authutil.ValidateToken(token)
	if err != nil || !strings.EqualFold(resolved, username) {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}
	if wb.onSession != nil {
		if err := wb.onSession(username, token); err != nil {
			http.Error(w, fmt.Sprintf("session rejected: %v", err), http.StatusForbidden)
			return
		}
	}
	conn, err := wb.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws upgrade: %v", err)
		return
	}
	wb.register(conn)
	go wb.readLoop(conn)
	wb.sendHistory(conn)
}

func (wb *webBridge) handleSSE(w http.ResponseWriter, r *http.Request) {
	username := r.URL.Query().Get("username")
	token := r.URL.Query().Get("token")
	if username == "" || token == "" {
		http.Error(w, "missing credentials", http.StatusUnauthorized)
		return
	}
	resolved, err := authutil.ValidateToken(token)
	if err != nil || !strings.EqualFold(resolved, username) {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "stream unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// Each SSE subscriber gets a buffered channel so a slow browser cannot block
	// notifications destined for other clients.
	ch := make(chan webEvent, 8)
	wb.addSSEClient(ch)
	defer wb.removeSSEClient(ch)
	fmt.Fprint(w, ":ok\n\n")
	flusher.Flush()
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case evt := <-ch:
			data, err := json.Marshal(evt)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}

func (wb *webBridge) register(conn *websocket.Conn) {
	wb.clientsMu.Lock()
	wb.clients[conn] = struct{}{}
	wb.clientsMu.Unlock()
}

func (wb *webBridge) unregister(conn *websocket.Conn) {
	wb.clientsMu.Lock()
	delete(wb.clients, conn)
	wb.clientsMu.Unlock()
	_ = conn.Close()
}

func (wb *webBridge) readLoop(conn *websocket.Conn) {
	defer wb.unregister(conn)
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		line := strings.TrimSpace(string(data))
		if line == "" {
			continue
		}
		go wb.submit(line)
	}
}

func (wb *webBridge) sendHistory(conn *websocket.Conn) {
	event := webEvent{Kind: "history", History: wb.history.All()}
	wb.sendEventTo(conn, event)
}

func (wb *webBridge) sendEvent(evt webEvent) {
	data, err := json.Marshal(evt)
	if err != nil {
		log.Printf("web event encode: %v", err)
		return
	}
	wb.clientsMu.Lock()
	for conn := range wb.clients {
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			log.Printf("web send: %v", err)
			delete(wb.clients, conn)
			_ = conn.Close()
		}
	}
	wb.clientsMu.Unlock()
	wb.emitSSE(evt)
}

func (wb *webBridge) sendEventTo(conn *websocket.Conn, evt webEvent) {
	data, err := json.Marshal(evt)
	if err != nil {
		return
	}
	_ = conn.WriteMessage(websocket.TextMessage, data)
}

func (wb *webBridge) addSSEClient(ch chan webEvent) {
	wb.sseMu.Lock()
	wb.sseClients[ch] = struct{}{}
	wb.sseMu.Unlock()
}

func (wb *webBridge) removeSSEClient(ch chan webEvent) {
	wb.sseMu.Lock()
	delete(wb.sseClients, ch)
	wb.sseMu.Unlock()
	close(ch)
}

func (wb *webBridge) emitSSE(evt webEvent) {
	if evt.Kind != "notification" {
		return
	}
	wb.sseMu.Lock()
	for ch := range wb.sseClients {
		select {
		case ch <- evt:
		default:
		}
	}
	wb.sseMu.Unlock()
}

func (wb *webBridge) ShowMessage(msg message.Message) {
	wb.sendEvent(webEvent{Kind: "message", Message: msg})
}

func (wb *webBridge) ShowSystem(text string) {
	wb.sendEvent(webEvent{Kind: "system", Text: text})
}

func (wb *webBridge) UpdatePeers(peers []peerPresence) {
	wb.sendEvent(webEvent{Kind: "peers", Users: peers})
}

func (wb *webBridge) ShowNotification(n notificationPayload) {
	evt := webEvent{Kind: "notification", Notification: n}
	wb.sendEvent(evt)
}

type webEvent struct {
	Kind         string              `json:"kind"`
	Message      message.Message     `json:"message,omitempty"`
	Text         string              `json:"text,omitempty"`
	Users        []peerPresence      `json:"users,omitempty"`
	History      []message.Message   `json:"history,omitempty"`
	Notification notificationPayload `json:"notification,omitempty"`
	File         fileRecord          `json:"file,omitempty"`
}
