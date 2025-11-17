一、專案概述

本專案旨在利用 Go 語言實作一套 點對點（Peer-to-Peer, P2P）聊天系統。不同於一般以中央伺服器進行訊息轉發的架構，本系統允許各個 Peer 使用者彼此直接建立 TCP 連線進行通訊。

每位 Peer 具備雙重角色：

Server：負責監聽來自其他 Peer 的連線

Client：主動連線至已知的其他 Peer

系統啟動時，Peer 會向一個簡單的 bootstrap server 登記並取得目前在線 Peer 清單，接著利用該清單與其他 Peer 建立直接 TCP 通道，形成一個去中心化的聊天網絡。

整體使用 Goroutines + Channels 實現多連線管理，支援多人即時互動，並可在本機使用多個 Terminal 模擬器進行 P2P 測試。

二、運行環境

開發語言：Go 1.21+

系統需求：Windows / macOS / Linux（可跨平台）

執行方式：

本機模擬多個 Peer → 多開 Terminal 即可

可將 bootstrap server 與 P2P peer 各自開啟

網路傳輸：TCP Socket

編碼方式：UTF-8

三、使用技術
1. Go Concurrency（並發）

goroutine 處理每個 Peer 所建立的多條 TCP 連線

channel 用於跨 goroutine 傳遞訊息與同步

2. Network Programming（網路程式設計）

使用 net 套件建立 TCP 伺服器與客戶端

實作訊息格式、序列化與傳輸協定

3. P2P 基礎網路拓樸

Bootstrap server 提供 Peer 清單

Peer 間直接連線形成 Mesh-like 聊天網路

4. 基礎 UI/UX

CLI 終端機界面

日後可擴充：

WebSocket + Web 前端

TUI（Text UI，像 Chat-GPT 的 CLI）

四、專案架構
(1) 目錄結構（建議）
專案簡要目錄（先建立）
p2p-chat/
├─ cmd/
│  ├─ bootstrap/
│  │   └─ main.go
│  └─ peer/
│      └─ main.go
├─ internal/
│  ├─ message/
│  │   └─ message.go
│  ├─ peerlist/
│  │   └─ peerlist.go
│  └─ network/
│      └─ connmgr.go
├─ go.mod

│── go.mod

(2) 系統流程圖
啟動 Peer
   ↓
連線 Bootstrap Server → 登記並取得 peers 清單
   ↓
向每一個 peer 主動建立 TCP 連線（client）
   ↓
同時啟動 TCP server 監聽其他 peer 的連線
   ↓
所有連線皆用 goroutine 處理（read & write）
   ↓
收到訊息 → 廣播給其他 peer

(3) Bootstrap Server

維護：目前在線 Peer 清單

提供 API：

POST /register

GET /peers

(4) Peer 端

TCP server：等待其他 Peer 主動連線

TCP client：與清單中的 Peer 主動建立連線

Message handler：接受訊息 → 廣播 → 顯示在 CLI


Step 1：了解 P2P 與 bootstrap 概念（做什麼、為什麼）

怎做（概念）

Bootstrap server：一個非常簡單的 HTTP 服務，用來 註冊（register） peer 的位址（例如 ip:port）並回傳目前在線的 peers 清單。它只是「名冊」，不轉發聊天訊息。

Peer：每個 peer 同時充當 server（監聽 port）和 client（向其他 peers 連線），使用 TCP 直接傳訊。

使用本機測試時，bootstrap server 與多個 peer 都在同一台機器上啟動，透過不同 port 區分。

說明

Bootstrap server 簡化 peer 發現流程；在真實 P2P 網路會有更複雜 DHT / NAT 穿透，但本專案以教學為主，使用簡單清單即可。

Step 2：撰寫 Bootstrap Server（cmd/bootstrap/main.go）

怎做（code）
建立一個最簡的 HTTP server，提供兩個 API：

POST /register：body 包含 peerAddr（例如 127.0.0.1:9001），server 把它加入清單並回傳 success

GET /peers：回傳目前 peer 清單（JSON）

程式碼

// cmd/bootstrap/main.go
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
)

type RegisterReq struct {
	Addr string `json:"addr"`
}

var (
	mu    sync.Mutex
	peers = make(map[string]bool)
)

func registerHandler(w http.ResponseWriter, r *http.Request) {
	var req RegisterReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	mu.Lock()
	peers[req.Addr] = true
	mu.Unlock()
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"ok":true}`))
}

func peersHandler(w http.ResponseWriter, r *http.Request) {
	mu.Lock()
	defer mu.Unlock()
	list := make([]string, 0, len(peers))
	for p := range peers {
		list = append(list, p)
	}
	json.NewEncoder(w).Encode(list)
}

func main() {
	http.HandleFunc("/register", registerHandler)
	http.HandleFunc("/peers", peersHandler)
	log.Println("bootstrap server listening on :8000")
	log.Fatal(http.ListenAndServe(":8000", nil))
}


說明

很直觀：以 map 存 peer 位址；GET /peers 回傳清單。

在本機測試時，bootstrap server 在 :8000。

Step 3：定義訊息格式（internal/message/message.go）

怎做（code）
定義聊天訊息的 JSON 結構，包含來源、內容、時間戳等。

// internal/message/message.go
package message

import "time"

type Message struct {
	From      string    `json:"from"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
	// MsgID 可選，用於去重或循環檢查
	MsgID string `json:"msg_id,omitempty"`
}


說明

使用 JSON 編碼傳輸，簡單易讀且可 debug。

MsgID 可用 UUID 或 timestamp+from 做為唯一識別，防止重播/循環廣播。

Step 4：Peer 的連線管理（internal/network/connmgr.go）

怎做（code）
實作管理連線的基本功能：接受連線、主動連線、讀寫、廣播訊息。使用 channels 在 goroutine 間傳遞 incoming/outgoing 訊息。

// internal/network/connmgr.go
package network

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"p2p-chat/internal/message"
)

type ConnManager struct {
	addr       string
	listener   net.Listener
	connsMu    sync.Mutex
	conns      map[string]net.Conn
	Incoming   chan message.Message // 從其他 peer 收到的訊息（給 UI 使用）
	outgoingCh chan message.Message // 要送到其他 peer 的訊息
	quit       chan struct{}
}

func NewConnManager(addr string) *ConnManager {
	return &ConnManager{
		addr:       addr,
		conns:      make(map[string]net.Conn),
		Incoming:   make(chan message.Message, 100),
		outgoingCh: make(chan message.Message, 100),
		quit:       make(chan struct{}),
	}
}

func (cm *ConnManager) StartListen() error {
	ln, err := net.Listen("tcp", cm.addr)
	if err != nil {
		return err
	}
	cm.listener = ln
	go cm.acceptLoop()
	return nil
}

func (cm *ConnManager) acceptLoop() {
	for {
		conn, err := cm.listener.Accept()
		if err != nil {
			select {
			case <-cm.quit:
				return
			default:
				log.Println("accept error:", err)
				continue
			}
		}
		remote := conn.RemoteAddr().String()
		cm.addConn(remote, conn)
		go cm.handleConn(conn, remote)
	}
}

func (cm *ConnManager) addConn(key string, conn net.Conn) {
	cm.connsMu.Lock()
	defer cm.connsMu.Unlock()
	cm.conns[key] = conn
}

// ConnectToPeer 主動連線
func (cm *ConnManager) ConnectToPeer(peerAddr string) error {
	cm.connsMu.Lock()
	if _, exists := cm.conns[peerAddr]; exists {
		cm.connsMu.Unlock()
		return nil // 已連線
	}
	cm.connsMu.Unlock()

	conn, err := net.DialTimeout("tcp", peerAddr, 3*time.Second)
	if err != nil {
		return err
	}
	cm.addConn(peerAddr, conn)
	go cm.handleConn(conn, peerAddr)
	return nil
}

func (cm *ConnManager) handleConn(conn net.Conn, key string) {
	defer func() {
		cm.removeConn(key)
		conn.Close()
	}()
	reader := bufio.NewReader(conn)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err != io.EOF {
				log.Println("read error:", err)
			}
			return
		}
		var msg message.Message
		if err := json.Unmarshal(line, &msg); err != nil {
			log.Println("json unmarshal:", err, string(line))
			continue
		}
		// 收到訊息，放入 Incoming channel
		cm.Incoming <- msg
	}
}

func (cm *ConnManager) Broadcast(msg message.Message, except string) {
	// 將 msg 序列化並寫入每個 conn（except 為來源地址，用以避免回傳）
	cm.connsMu.Lock()
	defer cm.connsMu.Unlock()

	data, _ := json.Marshal(msg)
	data = append(data, '\n')
	for k, c := range cm.conns {
		if k == except {
			continue
		}
		// 非阻塞寫入 (簡單實作)
		_, err := c.Write(data)
		if err != nil {
			// 如果寫失敗，關掉該連線
			log.Println("write err to", k, err)
			c.Close()
			delete(cm.conns, k)
		}
	}
}

func (cm *ConnManager) removeConn(key string) {
	cm.connsMu.Lock()
	defer cm.connsMu.Unlock()
	if c, ok := cm.conns[key]; ok {
		c.Close()
		delete(cm.conns, key)
	}
}

func (cm *ConnManager) Stop() {
	close(cm.quit)
	if cm.listener != nil {
		cm.listener.Close()
	}
	cm.connsMu.Lock()
	for _, c := range cm.conns {
		c.Close()
	}
	cm.conns = map[string]net.Conn{}
	cm.connsMu.Unlock()
}


說明

ConnManager 負責所有 TCP 連線（incoming + outgoing）。

讀到每條 JSON newline-delimited 訊息後送到 Incoming channel。

Broadcast 可以將訊息送給所有連線（支援 except 以避免把訊息回傳給來源，降低循環）。

Step 5：Peer 啟動時向 bootstrap 註冊並取得 peers（整合在 cmd/peer/main.go）

怎做（code）

啟動 ConnManager 監聽自己的 port

向 http://bootstrap:8000/register POST 自己的 addr

取得 GET /peers，對每個 peer 主動連線

同時啟動 CLI loop 讀取輸入，送到 ConnManager.outgoingCh → 再由 Broadcast 廣播

// cmd/peer/main.go
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"p2p-chat/internal/message"
	"p2p-chat/internal/network"
)

var bootstrap = flag.String("bootstrap", "http://127.0.0.1:8000", "bootstrap url")
var port = flag.String("port", "9001", "listening port")

func registerSelf(addr string) error {
	req := map[string]string{"addr": addr}
	b, _ := json.Marshal(req)
	resp, err := http.Post(*bootstrap+"/register", "application/json", bytes.NewReader(b))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return nil
}

func getPeers() ([]string, error) {
	resp, err := http.Get(*bootstrap + "/peers")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var list []string
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, err
	}
	return list, nil
}

func main() {
	flag.Parse()
	addr := "127.0.0.1:" + *port

	// 1. start listener
	cm := network.NewConnManager(addr)
	if err := cm.StartListen(); err != nil {
		log.Fatal("listen err:", err)
	}
	log.Println("listening on", addr)

	// 2. register to bootstrap
	if err := registerSelf(addr); err != nil {
		log.Println("register err:", err)
		// 繼續，即便註冊失敗也能本機測試
	}

	// 3. get peers and connect
	peers, err := getPeers()
	if err != nil {
		log.Println("get peers err:", err)
	} else {
		for _, p := range peers {
			if p == addr {
				continue
			}
			if err := cm.ConnectToPeer(p); err != nil {
				log.Println("connect to", p, "err:", err)
			} else {
				log.Println("connected to", p)
			}
		}
	}

	// 4. goroutine: 處理 incoming 訊息並顯示
	go func() {
		for msg := range cm.Incoming {
			// 顯示訊息
			fmt.Printf("[%s] %s: %s\n", msg.Timestamp.Format("15:04:05"), msg.From, msg.Content)
			// 再轉發（簡單 flood 策略，注意避免無限循環；此範例透過 MsgID 或 except 解決）
			cm.Broadcast(msg, "") // 如果要避免循環，可帶入來源
		}
	}()

	// 5. CLI 讀入並廣播
	go func() {
		reader := bufio.NewReader(os.Stdin)
		for {
			line, _ := reader.ReadString('\n')
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			msg := message.Message{
				From:      addr,
				Content:   line,
				Timestamp: time.Now(),
			}
			// 直接廣播
			cm.Broadcast(msg, "")
		}
	}()

	// 6. block
	select {}
}


說明

啟動後會先註冊到 bootstrap，然後嘗試連線清單內的 peers。

收到 Incoming 訊息會印出並再次 Broadcast（這裡要注意循環，後面 steps 會改進）。

CLI 讀入使用者輸入，立刻包成 message.Message 並廣播。
Step 6：避免訊息循環與去重（改進廣播策略）

若每個 peer 收到就盲目再廣播，會在 mesh 中造成訊息迴圈 (flooding)。常見解法：

每條訊息加唯一 MsgID，收到時記錄已處理的 MsgID，如果重複就丟棄。

廣播時排除來源 socket（except）以降低回傳。

實作（修改）

在 message.Message 加 MsgID（例如使用 timestamp+from 或 UUID）

在 ConnManager 或 peer 層加 seen map 保存已處理 MsgID

範例程式片段（概念）：

// 在 peer main.go 裡面加一個 map 記錄
seen := make(map[string]bool)

// 處理 incoming
for msg := range cm.Incoming {
    if msg.MsgID != "" && seen[msg.MsgID] {
        continue
    }
    if msg.MsgID != "" {
        seen[msg.MsgID] = true
    }
    fmt.Printf("[%s] %s: %s\n", ...)
    // Broadcast 時帶上來源 addr 以避免直接回傳
    cm.Broadcast(msg, msg.From) 
}


說明

這樣可以大幅減少循環。

MsgID 建議用 from + timestamp 或 uuid。

Step 7：Handling Peer 動態加入 / 離線與重試

怎做

在 peer 啟動後定時向 bootstrap 輪詢 GET /peers（或 bootstrap 提供 push / websocket，但輪詢最簡單）

當發現新 peer，呼叫 ConnectToPeer

當連線失敗時，紀錄並定期重試

簡單實作（輪詢）
在 cmd/peer/main.go 增加一個 goroutine：

go func() {
    for {
        time.Sleep(5 * time.Second)
        list, err := getPeers()
        if err != nil {
            continue
        }
        for _, p := range list {
            if p == addr { continue }
            if err := cm.ConnectToPeer(p); err == nil {
                log.Println("connected to", p)
            }
        }
    }
}()


說明

輪詢間隔可調（例如 5 秒、10 秒）。

也可優化成僅 connect 新登錄的 peers（比對已知列表）。

Step 8：Graceful Shutdown（優雅關閉）

怎做

捕捉 SIGINT (Ctrl+C) 與 SIGTERM，呼叫 cm.Stop() 結束 listener 與關閉連線。

程式範例片段

// main.go (於 select {} 前加)
c := make(chan os.Signal, 1)
signal.Notify(c, os.Interrupt)
<-c
log.Println("shutting down...")
cm.Stop()
os.Exit(0)


說明

避免半斷線影響其他 peers 的連線表現。

Step 9：UI/UX 整合 — CLI 介面強化（顯示、分色、命令）

這是第一個 UI/UX 整合步驟（請注意你要求把 UI/UX 放在 Step 9-12）。

怎做

使用簡潔的 CLI 顯示格式：時間 + 發送者 + 訊息

增加本地命令（例如 /peers 顯示已連線 peers、/quit 離開）

為了簡單，不使用第三方 TUI lib（但後面可擴充）

範例：擴充 CLI
在 cmd/peer/main.go 的 CLI goroutine 中處理以 / 開頭的命令：

go func() {
    reader := bufio.NewReader(os.Stdin)
    for {
        line, _ := reader.ReadString('\n')
        line = strings.TrimSpace(line)
        if line == "" {
            continue
        }
        if strings.HasPrefix(line, "/") {
            switch {
            case line == "/peers":
                cm.ConnsList() // 請在 connmgr 實作回傳連線清單的函式
            case line == "/quit":
                cm.Stop()
                os.Exit(0)
            default:
                fmt.Println("unknown command")
            }
            continue
        }
        // 傳送訊息...
    }
}()


ConnManager 新增 ConnsList()（示範）

func (cm *ConnManager) ConnsList() []string {
    cm.connsMu.Lock()
    defer cm.connsMu.Unlock()
    keys := make([]string, 0, len(cm.conns))
    for k := range cm.conns {
        keys = append(keys, k)
    }
    return keys
}


說明

/peers 指令讓使用者快速檢視連線狀態，增加可用性。

若要更好 UX，可加入顏色顯示（在 CLI 中用 ANSI escape sequences），或採用 tview 做 TUI。

Step 10：UI/UX 整合 — 使用者名稱與訊息格式化

怎做

允許在啟動 peer 時提供 --nick（暱稱），訊息顯示用暱稱而非 ip:port。

加上時間、訊息氣泡或前綴以更好辨識。

code
在 cmd/peer/main.go 加 flag：

var nick = flag.String("nick", "", "nickname")
...
displayName := *nick
if displayName == "" {
    displayName = addr
}


在產生訊息時：

msg := message.Message{
    From:      displayName,
    Content:   line,
    Timestamp: time.Now(),
    MsgID:     fmt.Sprintf("%s-%d", displayName, time.Now().UnixNano()),
}


訊息顯示：

fmt.Printf("\x1b[32m[%s]\x1b[0m \x1b[33m%s\x1b[0m: %s\n", 
    msg.Timestamp.Format("15:04:05"), msg.From, msg.Content)


（\x1b[32m 等為 ANSI 顏色碼）

說明

暱稱讓聊天更友善；ANSI 顏色使訊息更好閱讀。

對 Windows（舊版）可能需啟用 ANSI 支援；在大多數現代 Terminal 都支援。

Step 11：UI/UX 整合 — 訊息歷史與滾動（本地保存）

怎做

在 Peer 本地保留最近 N 條訊息（環形 buffer 或 slice），讓使用者可以使用 /history 查看聊天紀錄

可把歷史寫入檔案方便事後查閱

實作片段

// main.go 中加一個 history slice 與 mutex
var historyMu sync.Mutex
var history []message.Message

// 接收訊息時 push 到 history
historyMu.Lock()
history = append(history, msg)
if len(history) > 200 {
    history = history[len(history)-200:] // keep last 200
}
historyMu.Unlock()

// 在命令處理中
case line == "/history":
    historyMu.Lock()
    for _, m := range history {
        fmt.Printf("%s %s: %s\n", m.Timestamp.Format("01-02 15:04:05"), m.From, m.Content)
    }
    historyMu.Unlock()


說明

本地保存歷史有助於回顧對話，提升使用者體驗。

未來可以把歷史上傳到檔案或簡單 DB（例如 boltdb）。

Step 12：UI/UX 整合 — Web GUI 或 TUI（擴充建議與基本方向）

這步為「擴充方向」，會提供如何把 CLI 介面換成網頁或更漂亮的 TUI 的詳細建議，並給出最小可行性藍圖（MVP）。

方向 A — Web GUI（較推薦）

建議架構：

在每個 peer 啟一個小型 HTTP server（或 WebSocket server）。

CLI 的 ConnManager 保持不變，但新增一個 WebSocket endpoint (/ws) 供本地瀏覽器連線，瀏覽器透過 WS 與 peer HTTPS server 溝通，由 peer 負責轉送到其他 peers。

優點：

支援富介面（avatar、時間軸、消息輸入框）

可在任何裝置的瀏覽器上觀察本地 peer 的訊息流

實作要點：

使用 github.com/gorilla/websocket 或標準 lib（簡單）

前端可用純 HTML+JS 或 React（簡單版只需一個 HTML 檔）

最簡 MVP：

啟動一個 / 回傳靜態 HTML，/ws 作為 websocket，收到 websocket send 後由 peer 廣播，收到 peer 訊息後推送 websocket 給瀏覽器，瀏覽器呈現。

方向 B — Terminal UI（TUI）

使用 github.com/rivo/tview 或 tcell 實作分割視窗（左邊為 peers 列表、右邊為訊息、底部為輸入欄）

優點：在 Terminal 就能有 GUI 感體驗

實作要點：將 cm.Incoming 綁定到 tview 的消息表格更新函式

示意（WebSocket 基本思路）

Peer 啟 http.ListenAndServe(":port+1000", ...)，提供 /ws。

若瀏覽器發送訊息 JSON => Peer 接收到後 cm.Broadcast(msg, "")。

Peer 接收到 cm.Incoming 的訊息 => 發給所有已 connect 的 websocket clients（即本地 ui）。

本地測試示例 — 如何啟動（Step-by-step）

初始化 go module：

cd p2p-chat
go mod init p2p-chat


編譯或直接運行 bootstrap：

go run cmd/bootstrap/main.go
# 或: go build -o bootstrap ./cmd/bootstrap && ./bootstrap


在另一個 terminal 啟動 peer：

go run cmd/peer/main.go --port=9001 --nick=Alice


再開兩個 terminal：

go run cmd/peer/main.go --port=9002 --nick=Bob
go run cmd/peer/main.go --port=9003 --nick=Carol


在任何 peer 後的 CLI 輸入文字並按 Enter，訊息應會被其他 peers 顯示（注意 MsgID 去重與 except 處理）



根據上述文件，請將