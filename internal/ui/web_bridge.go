package ui

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

	"github.com/gorilla/websocket"

	"p2p-chat/internal/authutil"
	"p2p-chat/internal/message"
	"p2p-chat/internal/storage"
)

//go:embed webui/static
var webFS embed.FS

// HistoryProvider exposes the chat backlog to the web UI without coupling
// the ui package to a specific runtime implementation.
type HistoryProvider interface {
	All() []message.Message
}

// WebBridge wires the embedded web UI to the runtime via HTTP, WS and SSE.
type WebBridge struct {
	addr       string
	srv        *http.Server
	upgrader   websocket.Upgrader
	history    HistoryProvider
	submit     func(string)
	files      *storage.FileStore
	share      func(storage.FileRecord, string) error
	clientsMu  sync.Mutex
	clients    map[*websocket.Conn]struct{}
	sseMu      sync.Mutex
	sseClients map[chan webEvent]struct{}
	staticFS   http.Handler
	onSession  func(string, string) error
}

const maxUploadBytes = 25 << 20

func NewWebBridge(addr string, history HistoryProvider, submit func(string), onSession func(string, string) error, files *storage.FileStore, share func(storage.FileRecord, string) error) (*WebBridge, error) {
	sub, err := fs.Sub(webFS, "webui/static")
	if err != nil {
		return nil, err
	}
	wb := &WebBridge{
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

func (wb *WebBridge) Run(ctx context.Context) {
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

func (wb *WebBridge) Close() {
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

// Addr exposes the bound address so other layers can build public URLs.
func (wb *WebBridge) Addr() string {
	return wb.addr
}

func (wb *WebBridge) handleIndex(w http.ResponseWriter, r *http.Request) {
	wb.serveHTML(w, "webui/static/index.html")
}

func (wb *WebBridge) handleChat(w http.ResponseWriter, r *http.Request) {
	wb.serveHTML(w, "webui/static/app.html")
}

func (wb *WebBridge) handleFiles(w http.ResponseWriter, r *http.Request) {
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

func (wb *WebBridge) handleFileDownload(w http.ResponseWriter, r *http.Request) {
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

func (wb *WebBridge) listFiles(w http.ResponseWriter, r *http.Request) {
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

func (wb *WebBridge) uploadFile(w http.ResponseWriter, r *http.Request) {
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
	wb.sendEvent(webEvent{Kind: "notification", Notification: Notification{
		ID:        record.ID,
		From:      username,
		Level:     "file",
		Text:      fmt.Sprintf("%s uploaded %s", username, record.Name),
		Timestamp: time.Now(),
	}})
}

func (wb *WebBridge) handlePushSubscribe(w http.ResponseWriter, r *http.Request) {
	if _, err := wb.requireAuth(r); err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	io.Copy(io.Discard, r.Body)
	w.WriteHeader(http.StatusAccepted)
}

func (wb *WebBridge) requireAuth(r *http.Request) (string, error) {
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

func (wb *WebBridge) writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("json write: %v", err)
	}
}

func (wb *WebBridge) broadcastFile(record storage.FileRecord) {
	wb.sendEvent(webEvent{Kind: "file", File: record})
}

func (wb *WebBridge) serveHTML(w http.ResponseWriter, path string) {
	data, err := webFS.ReadFile(path)
	if err != nil {
		http.Error(w, "missing assets", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
}

func (wb *WebBridge) handleWS(w http.ResponseWriter, r *http.Request) {
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

func (wb *WebBridge) handleSSE(w http.ResponseWriter, r *http.Request) {
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

func (wb *WebBridge) register(conn *websocket.Conn) {
	wb.clientsMu.Lock()
	wb.clients[conn] = struct{}{}
	wb.clientsMu.Unlock()
}

func (wb *WebBridge) unregister(conn *websocket.Conn) {
	wb.clientsMu.Lock()
	delete(wb.clients, conn)
	wb.clientsMu.Unlock()
	_ = conn.Close()
}

func (wb *WebBridge) readLoop(conn *websocket.Conn) {
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

func (wb *WebBridge) sendHistory(conn *websocket.Conn) {
	event := webEvent{Kind: "history", History: wb.history.All()}
	wb.sendEventTo(conn, event)
}

func (wb *WebBridge) sendEvent(evt webEvent) {
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

func (wb *WebBridge) sendEventTo(conn *websocket.Conn, evt webEvent) {
	data, err := json.Marshal(evt)
	if err != nil {
		return
	}
	_ = conn.WriteMessage(websocket.TextMessage, data)
}

func (wb *WebBridge) addSSEClient(ch chan webEvent) {
	wb.sseMu.Lock()
	wb.sseClients[ch] = struct{}{}
	wb.sseMu.Unlock()
}

func (wb *WebBridge) removeSSEClient(ch chan webEvent) {
	wb.sseMu.Lock()
	delete(wb.sseClients, ch)
	wb.sseMu.Unlock()
	close(ch)
}

func (wb *WebBridge) emitSSE(evt webEvent) {
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

func (wb *WebBridge) ShowMessage(msg message.Message) {
	wb.sendEvent(webEvent{Kind: "message", Message: msg})
}

func (wb *WebBridge) ShowSystem(text string) {
	wb.sendEvent(webEvent{Kind: "system", Text: text})
}

func (wb *WebBridge) UpdatePeers(peers []Presence) {
	wb.sendEvent(webEvent{Kind: "peers", Users: peers})
}

func (wb *WebBridge) ShowNotification(n Notification) {
	evt := webEvent{Kind: "notification", Notification: n}
	wb.sendEvent(evt)
}

type webEvent struct {
	Kind         string             `json:"kind"`
	Message      message.Message    `json:"message,omitempty"`
	Text         string             `json:"text,omitempty"`
	Users        []Presence         `json:"users,omitempty"`
	History      []message.Message  `json:"history,omitempty"`
	Notification Notification       `json:"notification,omitempty"`
	File         storage.FileRecord `json:"file,omitempty"`
}
