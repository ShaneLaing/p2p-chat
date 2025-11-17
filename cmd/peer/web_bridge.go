package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"sync"

	"p2p-chat/internal/authutil"
	"p2p-chat/internal/message"

	"github.com/gorilla/websocket"
)

//go:embed webui/static
var webFS embed.FS

type webBridge struct {
	addr      string
	srv       *http.Server
	upgrader  websocket.Upgrader
	history   *historyBuffer
	submit    func(string)
	clientsMu sync.Mutex
	clients   map[*websocket.Conn]struct{}
	staticFS  http.Handler
	onSession func(string, string) error
}

func newWebBridge(addr string, history *historyBuffer, submit func(string), onSession func(string, string) error) (*webBridge, error) {
	sub, err := fs.Sub(webFS, "webui/static")
	if err != nil {
		return nil, err
	}
	wb := &webBridge{
		addr:      addr,
		history:   history,
		submit:    submit,
		clients:   make(map[*websocket.Conn]struct{}),
		staticFS:  http.StripPrefix("/static/", http.FileServer(http.FS(sub))),
		onSession: onSession,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", wb.handleIndex)
	mux.HandleFunc("/chat", wb.handleChat)
	mux.Handle("/static/", wb.staticFS)
	mux.HandleFunc("/ws", wb.handleWS)
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
}

func (wb *webBridge) handleIndex(w http.ResponseWriter, r *http.Request) {
	wb.serveHTML(w, "webui/static/index.html")
}

func (wb *webBridge) handleChat(w http.ResponseWriter, r *http.Request) {
	wb.serveHTML(w, "webui/static/chat.html")
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
}

func (wb *webBridge) sendEventTo(conn *websocket.Conn, evt webEvent) {
	data, err := json.Marshal(evt)
	if err != nil {
		return
	}
	_ = conn.WriteMessage(websocket.TextMessage, data)
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

type webEvent struct {
	Kind    string            `json:"kind"`
	Message message.Message   `json:"message,omitempty"`
	Text    string            `json:"text,omitempty"`
	Users   []peerPresence    `json:"users,omitempty"`
	History []message.Message `json:"history,omitempty"`
}
