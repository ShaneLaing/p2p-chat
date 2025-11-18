1. 專案總覽（Project Overview）

---

### 專案背景與目的

p2p-chat 是一套以 Go 1.21+ 撰寫的點對點聊天系統。它讓多個節點（Peer）同時扮演 TCP 伺服器與客戶端，透過去中心化的 mesh 網路交換訊息。專案的後期需求（req13-36）加入了圖形化網頁介面、認證伺服器、聊天歷史保存與更佳的使用者體驗。

### 問題定義

傳統聊天室仰賴中央伺服器，容易形成單點故障。此專案探索「如何在沒有中央訊息轉發伺服器下，依然維持可靠、安全、易用的多人聊天」。

### 專案目標

1. 建立簡易 bootstrap 服務提供 peer discovery。
2. 讓 Peer 彼此間能自動建立多重 TCP 連線並進行加密訊息泛洪。
3. 提供 CLI/TUI/Web 三種介面，並支援私訊、歷史記錄、封鎖與指令快捷鍵。
4. 透過 Postgres + JWT 提供使用者註冊、登入、訊息留存與網頁登入流程。

### 使用情境與實際應用

- 學術或教學：展示 P2P 與加密網路程式設計概念。
- 小型團隊：快速搭建局域網聊天或災難備援通訊。
- 實驗平台：測試不同的訊息路由、加密或 UI 改良策略。

2. 系統架構（System Architecture）

---

### 全域架構圖（ASCII）

```
┌──────────────┐       ┌──────────────┐
│ Bootstrap    │<─────>│ Peer A       │<─────┐
│ HTTP Server  │       │ (CLI/TUI/Web)│      │
└──────────────┘       └──────────────┘      │
      ▲                   ▲      ▲           │
      │register/peers     │WS    │TCP        │
┌──────────────┐       ┌──────────────┐      │
│ Auth Server  │<─────>│ Peer B       │<─────┘
│ (REST+DB)    │  REST │ (Web UI)     │
└──────────────┘       └──────────────┘
```

### 模組間關係（Module Dependency Graph）

- `cmd/bootstrap` 依賴 Go net/http。
- `cmd/peer` 依賴 `internal/*`（crypto, message, network, authutil）、外部 `gorilla/websocket`, `tcell/tview`, `boltdb` 等。
- `cmd/auth` 依賴 `internal/authutil`、`chi`, `pgx`, `bcrypt`。
- `internal/authutil` 只依賴 `golang-jwt`。

### 分層架構

- **Presentation Layer**: CLI (`display.go`)、TUI (`ui_tui.go`)、Web UI (`web_bridge.go` + `webui/static/*`)。
- **Business Logic Layer**: Peer app (`main.go`), connection manager (`internal/network`), message handling, handshake、blocklist、metrics。
- **Data Access Layer**: BoltDB history store (`history_store.go`), Postgres via auth server (`cmd/auth`), JWT issuance (`internal/authutil`).

### 系統流程（System Flow）

1. Peer 啟動 → 向 bootstrap `/register` → `/peers` 取得清單。
2. ConnManager 監聽 TCP，並對 peer 清單撥號建立 mesh。
3. CLI/TUI/Web 產生聊天輸入，透過 `processLine` 轉為 message JSON → `ConnManager.Broadcast` 泛洪。
4. 收到訊息 → 去重 → 儲存於 `historyBuffer`/BoltDB → UI 顯示 → 若為本地發送則呼叫 Auth API `/messages` 留存。
5. Web UI 使用者先到 Auth Server `/login` 取得 JWT，透過 WebSocket 將 token 送給 peer，完成 handshake 後即可收發。
6. 程式碼結構（Code Structure）

---

```
p2p-chat/
├─ cmd/
│  ├─ auth/        # chi REST server + Postgres migrations
│  ├─ bootstrap/   # HTTP registry for peer discovery
│  └─ peer/        # P2P executable + UI bridges
├─ internal/
│  ├─ authutil/    # JWT helper
│  ├─ crypto/      # AES-GCM box (scrypt-based key)
│  ├─ message/     # Message struct definition
│  └─ network/     # ConnManager (listen/dial/broadcast)
├─ image/          # 文檔用圖片
├─ web assets: cmd/peer/webui/static/*
├─ README.md / REPORT.md / req*.md
└─ go.mod / go.sum
```

主要模組職責：

- **cmd/bootstrap**：維護 peers map、提供 REST API。
- **cmd/peer**：核心邏輯；處理命令、訊息、UI 更新、handshake。
- **cmd/auth**：使用者註冊登入、訊息歷史 REST 介面。
- **internal/network.ConnManager**：handle TCP accept/connect, broadcast。
- **internal/authutil**：JWT Issue/Validate。
- **web_bridge**：serves login/chat HTML, upgrades WS,同步 UI 事件。

4. 資料結構與資料流

---

### 主要資料結構

- `message.Message`: `From, To, Content, Type, Timestamp, MsgID, PeerList, AuthToken`。
- `historyBuffer`: 環狀記錄最近 N 則訊息。
- `peerDirectory`: 保存 username ↔ address ↔ lastSeen。
- `identity`: 儲存目前顯示名稱與 JWT token。
- `historyStore`: BoltDB wrapper 儲存訊息紀錄。

### 資料實體

- Postgres `users(id, username, password_hash, created_at)`。
- Postgres `messages(id, sender, receiver, content, timestamp)`。

### 資料流流程

1. 使用者輸入 → `processLine` → `sendChatMessage` 或 `sendDirectMessage`。
2. 產生 `message.Message` → 推入 `historyBuffer`/BoltDB → `cm.Broadcast`。
3. 其他 peer 收到後 → `processIncoming` 去重、檢查 blocklist、更新 directory。
4. Web UI 透過 WS 收到 `webEvent{kind: message/system/peers/history}` 更新 DOM。

### Data Lifecycle

- 產生：CLI/TUI/Web input 或 bootstrap handshake。
- 傳輸：TCP JSON 或 WebSocket。
- 儲存：短期 `historyBuffer`、長期 BoltDB/Postgres。
- 消費：UI 顯示、metrics 統計、history 命令。
- 清理：historyBuffer 保持固定大小，BoltDB/DB 由管理者決定 retention。

5. 函數與方法設計

---

### 函數分類

- **Input/Output**：`readCLIInput`, `webBridge.readLoop`。
- **Helper utilities**：`newMsgID`, `chooseName`, `authutil.IssueToken`。
- **Core logic**：`processLine`, `sendChatMessage`, `processIncoming`, `ConnManager.Broadcast`。
- **Validation**：`authutil.ValidateToken`, `/msg` 參數檢查, REST handlers。
- **API handlers**：`registerHandler`, `loginHandler`, `storeMessageHandler`, `historyHandler`。

### 範例詳細（節錄）

- `processLine(app, line string)`：輸入命令或聊天文字；無回傳，根據前綴 `/` 決定流程。
- `sendChatMessage(app, content string)`：產生 `message.Message`、更新歷史、廣播；O(P) 其中 P 為目前連線數。
- `authutil.IssueToken(username string) (string, error)`：使用 HS256 與環境變數密鑰簽發 JWT。
- `webBridge.handleWS`：驗證 query string username/token，透過 `authutil.ValidateToken`；成功後升級 WebSocket 並註冊客戶端。

### 關鍵函數深度解析（示例 `processIncoming`）

1. 檢查 `MsgID` → 若已在 cache 則忽略，避免循環。
2. 若為 `ack` / `peer_sync` / `handshake` 類型，分別更新 ack tracker、dialer、peerDirectory。
3. 驗證 blocklist；若為 DM 但非本地接收者則 retransmit。
4. 將訊息寫入歷史與 BoltDB，再透過 sink 顯示並廣播給其他連線。
5. 演算法設計

---

### 演算法流程

- **訊息泛洪**：所有訊息帶 `MsgID`，ConnManager 保持簡單廣播。使用 `msgCache` (hash map + TTL) 去除重送迴圈。

### 正確性與複雜度

- 每個訊息在每個節點被處理一次，時間複雜度 O(E)（E 為連線數）。
- Cache 使用 map，查找/插入 O(1)。

### 優化策略

- 可改為 gossip/partial flooding 減少頻寬。
- 可在 `peerDirectory` 中加入距離或優先順序。

7. 程式控制流程

---

- **Main (peer)**：解析 flag → 初始化 ConnManager/identity/directory → 啟動 UI → 註冊 bootstrap → 啟動 goroutine (dialer, incoming handler, bootstrap poll, gossip, peer list更新) → 等待 OS signal。
- **模組互動**：ConnManager 將訊息送進 channel；appContext 消費並交給 sink；sink 可能是 CLI/TUI/WebBridge。
- **Edge cases**：blocklist, handshake 驗證失敗、沒有 secret 時的明文模式、BoltDB 不可用時 fallback。

8. Error Handling 與 Exception Management

---

- REST handler 使用 `http.Error` 傳遞 4xx/5xx。
- Peer 對於網路錯誤採用 log.Printf + 自動重試（dial scheduler）。
- WebSocket 錯誤：寫入失敗即關閉連線並從 `clients` map 移除。
- Fail-safe：BoltDB 開啟失敗會記錄 warning、仍以記憶體歷史繼續；auth API 呼叫失敗只記錄 log 不中斷聊天。

9. 設計模式

---

- **Observer**：webBridge/CLI/TUI 實作 `displaySink` 介面，appContext 透過 `sink.ShowMessage/ShowSystem/UpdatePeers` 廣播狀態。
- **Strategy**：`displaySink` 可用不同實作（CLI、TUI、Web），在 runtime 視 flag 決定。
- **Factory**：`crypto.NewBox(secret)` 根據是否提供 secret 回傳不同行為（加密/明文）。

10. 系統管理與擴充方式

---

- 新增功能：在 `handleCommand` 加入指令並於 `displaySink`/Web UI 建立按鈕即可。
- 修改邏輯：appContext 集中狀態，易於擴充 blocklist、metrics、persistExternal。
- 可擴充模組：auth server 可加入 refresh token、Role-Based Access；ConnManager 可導入 QUIC。
- 重構策略：拆分 `main.go` 的大型函式至專屬檔案、引入 interface 以利測試。

11. 測試（Testing）

---

- 目前以手動測試為主（`go run` 多個 peers + web UI）。
- 建議補齊：
  - 單元測試：msgCache、peerDirectory、authutil。
  - 整合測試：模擬 bootstrap + 2 peers 的訊息流。
  - Web E2E：利用 Playwright 自動登入/傳訊。

12. 效能分析

---

- 主要瓶頸：訊息泛洪 O(P^2) 連線成本、WebSocket/HTTP 換手成本。
- 記憶體：historyBuffer 控制在 ~200 筆，BoltDB 按需儲存。
- 可透過 `pprof` 分析 ConnManager broadcast/JSON 序列成本。
- 優化：改成差異同步、壓縮 JSON、支援批次 ack。

13. 安全性（Security）

---

- Input validation：REST handler 驗證 JSON 與必填欄位；CLI `/msg` 檢查 target/content。
- Injection 防護：使用 parameterized SQL（pgx）與 `encoding/json`。
- 加密：AES-256-GCM 對 TCP payload；JWT 簽章；web login 走 HTTPS（視部署）。
- 訪問控制：WebSocket 前置 token 驗證；handshake 會檢查 token 與 `From` 一致。

14. Dependencies

---

- `github.com/go-chi/chi/cors/httplog`：REST server 與 CORS 支援。
- `github.com/jackc/pgx/v5/stdlib`：Postgres driver。
- `github.com/gorilla/websocket`：Web UI WS。
- `github.com/gdamore/tcell` + `github.com/rivo/tview`：TUI。
- `go.etcd.io/bbolt`：本地訊息存檔。
- `golang.org/x/crypto`：scrypt/bcrypt。

15. 部署方式（Deployment）

---

- 環境：Go 1.21+、Postgres、（可選）瀏覽器。
- 流程：
  1. 啟動 Postgres，設定 `DATABASE_URL`。
  2. `go run ./cmd/auth`（自動遷移）
  3. `go run ./cmd/bootstrap --addr=:8000`
  4. 啟動多個 peer：`go run ./cmd/peer --port=9001 --secret=supersecret --web`
- 可用簡單批次檔或 systemd 管理。

16. 使用指南（Usage Guide）

---

- CLI 與 TUI：在終端輸入文字或 `/command`。
- Web：開啟 `http://<web-addr>` → 登入 → 在 UI 中輸入文字。
- 常見 Config：`--secret`, `--auth-api`, `--bootstrap`, `--history-db`, `--web-addr`。
- 輸入/輸出：文字訊息採 JSON 封包 `{type, from, content, timestamp}`；REST 回應為 JSON 或文字錯誤。
- Demo：使用 README 中的 quick-start 指令即可重現兩人對話。

17. 未來改善（Future Work）

---

- 已知限制：
  - Mesh 連線數成長後，泛洪成本上升。
  - 尚未有自動化測試/CI。
  - Web UI 尚未實作推播或檔案傳輸。
- 技術債：`cmd/peer/main.go` 過於龐大；需拆模組並加 interface。
- 改善方向：
  - 導入真正的 gossip/overlay protocol。
  - 增加離線訊息與多裝置同步。
  - Web UI 加入通知、表情、暗色主題切換。
