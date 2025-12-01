# r1
BUG 檢視 & 說明

Login flow regressions (commits 9c50a34, eacdb17, 8e48ff2) – Early post–ver.01 builds had fragile auth onboarding: the SPA didn’t surface bootstrap/auth presets, and failed requests left the page stuck. Multiple patches hardened the flow (cmd/peer/webui/static/index.html, login.js, styles.css), added health probes, and aligned CLI flags (main.go).
Peer runtime coupling (commit 8d605d3) – main.go mixed CLI parsing, networking, and storage and made it error-prone to change. The “Peer Runtime Refactor” split runtime concerns (cmd/peer/runtime.go, internal/authserver/*) to eliminate initialization races and duplicate goroutine management bugs.
Entrypoint bloat / stale assets (commit 9d6c224) – Legacy files under peer referenced removed helpers causing VS Code build errors. Moving everything into internal/peer/* plus a minimal main.go fixed undefined symbol issues and restored go test [Project](http://_vscodecontentref_/6)..
修改方式

Modularization – Created internal/bootstrap/{config,app,handlers}.go and internal/peer/{config,app,runtime,...} so both binaries now follow the same “Load config → NewApp → Start → WaitForShutdown” pattern (main.go, main.go).
Auth tightening – main.go now funnels all HTTP setup through server.go, with middleware (middleware.go) enforcing JWT validation and metrics instrumentation (metrics.go, README additions).
UI resilience – Refreshed SPA stack (cmd/peer/webui/static/*) with service worker (sw.js), themeable CSS, modular JS components, and richer login messaging. Added test stubs (ui/__tests__/*.mjs) to lock behavior.
性能提升

Connection management – The refactor introduced dedicated dial scheduler/ack tracker types inside peer to decouple gossip, bootstrap polling, and ack loops, reducing redundant dials and improving throughput under load.
Auth observability – Metrics counters plus structured logging (README “Auth Troubleshooting”) highlight latency and stateless vs persistent mode, helping identify slow DB interactions faster.
Build hygiene – Added .golangci.yml and VS Code lint task so regressions (unchecked IO, formatting) surface early, shrinking debugging time.
功能增添

Full Web UI (commit cd97090) – Shipping a complete browser experience: drag/drop uploads (files.js), DM threads (chat.js), notification drawer, offline caching, plus matching CLI/TUI polish.
Cloud persistence – File/history stores under p2p-data/<peer> with BoltDB adapters (history.go, files.go) and REST hooks so messages survive restarts.
Auth onboarding upgrades – Health checks, workspace recovery, login/register UI improvements, and JWT issuance hardened across commits 9c50a34, eacdb17, 8e48ff2.
Bootstrap rework – New internal package powers /register and /peers with graceful shutdown, unlocking future scaling.
待解決問題

Lint blockers – Running GOTOOLCHAIN=go1.21.9 [golangci-lint.exe](http://_vscodecontentref_/20) run [Project](http://_vscodecontentref_/21). still reports unchecked errors (runtime.go, web.go, ui_tui.go, handlers.go) and missing package comments. These need code edits before CI can enforce linting.
DB connectivity pains – Current auth exits if DATABASE_URL is misconfigured; reported TLS/password failures show the need for clearer defaults (e.g., guiding users to add sslmode=disable or fall back to stateless mode automatically).
Embedded binaries – Large BoltDB artifacts under p2p-data remain tracked; consider moving them to .gitignore to prevent repo bloat.
未來展望

Finish lint cleanup & add CI – Resolve errcheck/gofmt/revive findings and hook golangci-lint into GitHub Actions so future commits stay clean.
Improve auth robustness – Implement retry/backoff plus optional DATABASE_URL auto-detection, and expose /healthz in CMD logs to simplify ops.
Scalability features – With peer runtime modularized, it’s ready for per-component testing (scheduler, ack tracker) and advanced features like peer reputation or encrypted DM attachments (per original requirement backlog).
User-facing enhancements – Extend the SPA to surface metrics/webhooks built in Stage 35–36 (presence, DM history), and document CLI parity so both surfaces match.
Release packaging – Now that internal packages exist, create tagged releases (ver.02, ver.03…) summarizing the above milestones for stakeholders.

# r2
Commit cd970905 (2025-11-18 14:10)

BUG 檢視&說明：早期 web UI 只能靠簡陋的 app.js/chat.html 呈現，CLI/TUI 亦缺乏檔案/歷史存取，導致瀏覽器與命令列頻繁不同步。
修改方式：大幅翻新 cmd/peer/webui/static/*，拆出 composer.js、messageBubble.js 等元件，加入 state.js、sw.js，並重寫 cmd/peer/web_bridge.go 以串接 WebSocket、通知與檔案 API；同時新增 cmd/peer/files_store.go、history_store.go 及 CLI/TUI 顯示調整。
性能提升：Browser 資源改為模組化載入與 service worker 快取，減少重繪與重載；本地 BoltDB (p2p-data/.../files.db, history.db) 使反覆查詢改為 O(1) 存取。
功能增添：導入完整 web app 版面 (app.html, app.css)，支援通知抽屜、檔案傳輸、主題切換與 UI 測試；啟用 /files 儲存與 /history 回放。
待解決問題：UI 尚未與認證串連、CLI/TUI 與 web 還缺乏共用 runtime；缺少 lint/測試流程。
未來展望：持續將 peer runtime 模組化，強化認證流程，並把新版 UI 納入自動化測試。

# r3
Commit 9c50a341 (2025-11-18 15:43)

BUG：Auth server 啟動時沒有清楚的 DB fallback，main.go 對 peers 列表過度噪音，web CSS 在多視窗下排版錯亂。
修改方式：擴充 main.go（DB 初始化、錯誤輸出），調整 main.go 命令列輸出與 peers.go 資料結構，並重新整理 app.css。
性能：精簡 peers 列表計算邏輯，減少 UI repaint；auth server 以集中配置改善啟動時間。
功能：README/REPORT 加入新旗標說明，peer CLI 增加更多指令提示；web UI 新增多節點配色。
待解決：Auth 與 web login 仍未實作使用者流程，資料庫路徑還在 peer。
展望：後續 commit 7/feat 將補齊登入 UI 與 server 驗證，並導入多節點資料夾 (127-0-0-1-9003) 做為佈署基礎。

# r4
Commit eacdb17c (“commit 7”, 2025-11-18 16:21)

BUG：登入 UI 沒有提示、Auth server 缺少登入 API；既有 README/REPORT 也未記載。
修改：main.go 新增登入/註冊端點骨架；webui/index.html、login.js、styles.css 加上登入表單、狀態提示；文件同步更新。
性能：以精簡 JS 控制 login flow，減少重新整理。
功能：提供最基本的登入介面與 API 勾點，使後續 JWT 整合有介面可用。
待解決：尚未串接實際 REST 驗證與 token 儲存，Auth server 也缺監控。
展望：下一版 (feat commit) 著重在 instrument auth + 完整 onboarding。

# r5
Commit 8e48ff29 (“feat: instrument auth service…”, 2025-11-18 17:43)

BUG：先前登入/註冊失敗時沒有可視化訊息，Auth server 無 metrics/health 橋接。
修改：main.go 新增 httplog、CORS、JWT 驗證與錯誤處理；login.js/styles.css 提供多步驟 onboarding、健康檢查與錯誤提示；README/REPORT 加入操作手冊。
性能：儀表化 (metrics、structured log) 方便排查慢查詢；UI 透過 debounce 與狀態管理減少重複 fetch。
功能：健康檢查 banner、Token 儲存、工作區切換、登入後 redirect 等完整 onboarding 體驗。
待解決：Auth runtime 仍與 peer 緊耦合、缺獨立套件；peer 未分層。
展望：著手「Peer Runtime Refactor」把 auth 邏輯抽至 authserver 並減少 peer 負擔。

# r6
Commit 8d605d39 (“Peer Runtime Refactor”, 2025-11-18 23:44)

BUG：main.go 內含所有 goroutine/存取邏輯，導致循環引入與變更風險；auth server 端點散落 auth.
修改：刪除 300+ 行的 main.go/main.go，新建 cmd/peer/runtime.go 與 internal/authserver/{server,handlers,middleware,metrics}.go，將 handler/metrics/middleware集中。
性能：runtime loop 分拆 (runInboundHandler, runBootstrapLoop 等) 後，併發更清晰且易於調校，reduces duplicate dials。
功能：正式引入 authserver 套件，提供標準 chi router + middleware pipeline。
待解決：仍舊缺 peer 封裝、CLI 檔案/歷史仍在 peer，bootstrap 也未模組化。
展望：繼續把 peer 及 bootstrap 邏輯搬進 internal 套件，準備為 lint/CI 做鋪陳（實現在下一個 commit）。

# r7
Commit 9d6c224b (“peer main fixed”, 2025-11-25 10:56)

BUG：VS Code 問題清單顯示無法解析 cmd/peer/*.go 舊檔案、bootstrap 不能優雅關閉、lint 工具缺失。
修改：
新增 .golangci.yml、tasks.json 與 .gitignore，提供 lint 任務與工具安裝指引（README）。
internal/bootstrap/{config,app,handlers}.go 建立 Config/App/Handler 結構，main.go 瘦身為啟動殼。
全面搬遷 peer runtime 到 internal/peer/*，main.go 只剩 LoadConfig/NewApp/Start/WaitForShutdown；舊檔（ack_tracker 等）刪除或移址。
性能：瘦身後的 entrypoints 讓 build/test 更快，internal packages 也便於 future caching；lint pipeline早期警示未關閉的 io 資源。
功能：peer.App/peer.Config/peer.runtime 齊備，支援 CLI/TUI/Web 同時啟動與 bootstrap graceful shutdown；web 靜態資源搬至 webui 供 embed.
待解決：golangci-lint 仍回報 unchecked IO、缺 package comments 等，需要後續 commits 處理；auth 遇到錯誤會直接退出，仍需更好的 stateless fallback。
展望：整合 lint/CI、補齊 errcheck/gofmt，並計畫將 peer 功能進一步單元測試化；同時補強 auth server 對 DATABASE_URL 的容錯與文件說明。
