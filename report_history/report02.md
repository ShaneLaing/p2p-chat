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

**自動化檢查**

- `gofmt -w cmd/peer/web_bridge.go cmd/peer/history_store.go cmd/peer/files_store.go` 確保 Go 程式碼風格一致。
- `go build ./...` 驗證三個執行檔（bootstrap/peer/auth）與模組皆可成功編譯。
- `node cmd/peer/webui/static/ui/__tests__/theme.test.mjs` 檢查狀態儲存與主題切換邏輯。
- `node cmd/peer/webui/static/ui/__tests__/settings.test.mjs` 驗證設定面板的狀態同步與通知鎖。

**手動 QA**

- 啟動 bootstrap/auth/兩個 peer 的 web 模式後登入，使用 **Send** 按鈕送出訊息並確認 CLI/TUI/WS 均收到。
- 在 sidebar 切換 light/dark 主題後重新整理，確保 `state.js` 持久化資料生效。
- 於 Settings 變更通知/裝置標籤，觀察 toast 與 store 反饋同步。
- 從 Files 面板拖曳檔案，上傳進度條與其他 peer 的通知抽屜皆應更新；下載檔案須帶 JWT 查詢字串。
- 對另一位使用者發出 @mention，允許瀏覽器通知後確認抽屜堆疊與 OS toast 同步；檢查 `sw.js` 已快取殼層。

**後續建議**

- 為 BoltDB store、ConnManager、新的 `files_store` 與通知橋接撰寫 Go 單元測試。
- 以 Playwright 或 Cypress 增補 web E2E（登入、傳訊息、上傳檔案、通知）。
- 以 docker-compose 自動化啟動 bootstrap/auth/peer，以利 CI 重現。

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

- 環境：Go 1.21+、（可選）Postgres、（可選）瀏覽器。
- 流程：
  1. 啟動 Postgres（如需雲端歷史），並在終端設定 `DATABASE_URL`（例如 `postgres://user:pass@host:5432/p2p_chat`；PowerShell 可用 `$env:DATABASE_URL = "..."` 或 `setx DATABASE_URL "..."`）。若略過此步，auth 服務將以無資料庫模式啟動並回應 `503`。
  2. `go run ./cmd/auth`（自動遷移；若未設定 `DATABASE_URL` 會提示無法存取資料庫）`go run ./cmd/bootstrap --addr=:8000`
  3. 啟動多個 peer（示例）：
	 ```powershell
	 go run ./cmd/peer --port=9001 --secret=supersecret --web --web-addr 127.0.0.1:8081
	 go run ./cmd/peer --port=9002 --secret=supersecret --web --web-addr 127.0.0.1:8082
	 ```
     預設會在 `p2p-data/<host>-<port>/` 下建立專屬資料夾並放入 `history.db`、`files.db` 與上傳檔案，無需手動準備資料夾；若需統一放置到其他磁碟，新增 `--data-dir D:\mesh-data` 即可。
-  4. 透過 `GET /healthz` 監控 auth 服務的資料庫狀態：回傳 200 表示 Postgres 可用，503 則代表 `DATABASE_URL` 缺失或無法連線。
-  5. 每條 REST 請求都會輸出 JSON log（含 route/method/status/duration/stateless_mode/client），同時在記憶體內維護 `auth_requests_total/login_attempts_total/...` 計數器；重新啟動會歸零，搭配 README 的「Auth Troubleshooting」段落可加速除錯。
- 可用簡單批次檔或 systemd 管理。

16. 使用指南（Usage Guide）

---

- CLI 與 TUI：在終端輸入文字或 `/command`。
- Web：開啟 `http://<web-addr>` → 登入 → 在 UI 中輸入文字。
- 常見 Config：`--secret`, `--auth-api`, `--bootstrap`, `--data-dir`, `--history-db`, `--files-db`, `--files-dir`, `--web-addr`。
- 輸入/輸出：文字訊息採 JSON 封包 `{type, from, content, timestamp}`；REST 回應為 JSON 或文字錯誤。
- Demo：使用 README 中的 quick-start 指令即可重現兩人對話。

17. 未來改善（Future Work）

---

- 已知限制：
  - Mesh 連線數成長後，泛洪成本上升。
  - 自動化測試覆蓋率仍有限，尚未引入 CI。
  - Push API 目前只作為 service worker scaffold，尚未串接實際推播服務。
- 技術債：`cmd/peer/main.go` 過於龐大；需拆模組並加 interface。
- 改善方向：
  - 導入真正的 gossip/overlay protocol。
  - 增加離線訊息與多裝置同步。
  - 將瀏覽器通知延伸為真正的 Web Push，並加入更進階的檔案快取/續傳機制。

18. UI Revamp（Architecture & Flow, Commit 5）

---

- **Layout Shell:** `app.html` 取代舊的 `chat.html`，Layout/SideNav/TopBar/Drawer 皆以註解標示，模組（`ui/chat.js`, `ui/files.js`, `ui/settings.js`, `ui/notifications.js`, `ui/theme.js`）以命名節點掛載，`app.css` 在 `:root[data-theme=*]` 宣告亮/暗色系並標記事件列、進度條、設定卡片等複雜選擇器。
- **Modular JS:** `app.js` 只負責啟動輕量 reactive store（`state.js`）、WebSocket dispatcher（`ws.js`）與 UI/Component 模組（例如 `components/messageBubble.js`, `components/composer.js`, `components/notificationList.js`, `components/transferList.js`）。每個檔案開頭都有功能說明與 export 註解。
- **Theme & Settings:** 雙主題按鈕呼叫 `ui/theme.js` → `state.js`，以 `data-theme` 即時套用 CSS 變數；`ui/settings.js` 則提供通知開關、裝置暱稱與即時回饋（含 Notification API 權限提示）。
- **Files & Notifications:** `/api/files` 與前端 `ui/files.js` 整合，上傳使用 `XMLHttpRequest` 回報進度、下載自動掛 JWT Query。檔案完成後除了 WebSocket `kind:"file"` 事件外，也推送 SSE `notification` 以更新 Notification Drawer。
- **Service Worker & Browser APIs:** 新增 `sw.js` 緩存 layout shell 並處理 push scaffold；Notification Drawer 同步 SSE/WS，並在允許時觸發 browser-level toast。
- **Tests & Docs:** 加入 Node-based stub 測試（`ui/__tests__/theme.test.mjs`、`ui/__tests__/settings.test.mjs`），README/REPORT 也更新以描述模組化 UI、Service Worker 與 QA 流程。
- **Auth Onboarding Banner:** 登入頁面會先呼叫 `/healthz`，若發現資料庫停用則在 Step 1 卡片頂端顯示醒目的提示，避免使用者被 503 訊息嚇到。
