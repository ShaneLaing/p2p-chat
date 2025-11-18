# p2p-chat

Fully implementing stages 1–36 of the spec, this project delivers a secure mesh-style P2P chat system plus an authenticated web experience. Everything is written in Go 1.21+, and the repo currently contains three executables:

- `cmd/bootstrap`: lightweight registry that exposes `/register` and `/peers`.
- `cmd/peer`: the actual encrypted P2P node with CLI, TUI, and embedded web UI bridges.
- `cmd/auth`: Postgres-backed auth + history service that issues JWTs to both the CLI and the browser.

## Architecture Overview
- **Core mesh (req 1–12):** peers register with the bootstrap server, open TCP listeners, and flood JSON chat payloads with deduplication, retry/ack tracking, and AES-256-GCM when a shared `--secret` is present.
- **Enhanced UX (req 13–24):** nicknames, history, `/msg` DMs, persistence to BoltDB, `/block` lists, metrics, optional rich-text TUI UI, and the embedded web bridge that mirrors events over WebSockets.
- **Auth & Web (req 25–36):** dedicated REST auth server (chi + Postgres + JWT), login/registration screen, full chat web UI with presence, DM targeting, history fetch, and server-side handshake validation so only authenticated users appear on the mesh.

## Prerequisites
- Go 1.21+
- (Optional) A running Postgres instance. Provide its connection string via `DATABASE_URL` (for example, `postgres://user:pass@localhost:5432/p2p_chat`). When it is unset, the auth service still boots but responds with `503 Service Unavailable` for register/login/history endpoints.
- Modern browser for the web UI.

## Quick Start
1. **Bootstrap database (first run only):** create the database described above. The auth server auto-migrates `users` and `messages` tables.
2. **Run the auth service:**
	Set `DATABASE_URL` in the same PowerShell session if you want persistence:
	```powershell
	$env:DATABASE_URL = "postgres://user:pass@localhost:5432/p2p_chat"  # session only
	setx DATABASE_URL "postgres://user:pass@localhost:5432/p2p_chat"     # persist for future shells
	```
	Then start the server (it will log a warning and serve without persistence if you skip the variable):
	```powershell
	cd d:\NYCU_subjects\114-1\GO\Project\p2p-chat
	go run ./cmd/auth
	```
3. **Run the bootstrap server:**
	```powershell
	cd d:\NYCU_subjects\114-1\GO\Project\p2p-chat
	go run ./cmd/bootstrap --addr=:8000
	```
4. **Run two peers (CLI mode shown; add `--web` to serve the browser UI):**
	```powershell
	# Alice
	go run ./cmd/peer --port=9001 --nick=Alice --secret=topsecret --web --web-addr 127.0.0.1:8081

	# Bob
	go run ./cmd/peer --port=9002 --nick=Bob --secret=topsecret --web --web-addr 127.0.0.1:8082
	```
	Each peer automatically stores its history and file metadata under `p2p-data/<host>-<port>/` (for example, `p2p-data/127-0-0-1-9001/`). Use `--data-dir <path>` if you want to place those per-peer folders somewhere else, or provide explicit `--history-db/--files-db/--files-dir` paths.
5. **Open the browser UI:** visit `http://127.0.0.1:8081`, register/login via the auth server, then chat in the modern UI while the peer relays messages on the mesh.

The peer automatically persists your outbound messages to `cmd/auth` via `/messages`, whereas the web client also requests `/history` to prefill recent conversations.

## Peer Flags (excerpt)
- `--bootstrap` – URL of the bootstrap registry (default `http://127.0.0.1:8000`).
- `--port` / `--listen` – inbound TCP port; `--listen` accepts `host:port`.
- `--secret` – shared password enabling AES-GCM encryption.
- `--nick` – display name; use `/nick` at runtime to change.
- `--username` / `--token` – skip the web login by supplying existing JWT credentials.
- `--auth-api` – URL of the auth server when persisting history (default `http://127.0.0.1:8089`).
- `--tui` – enable the fullscreen terminal UI instead of the CLI stream.
- `--web` / `--web-addr` – serve the embedded login + chat web apps.
- `--history-db` – BoltDB path for local archival backing `/load`/`/save`.
- `--files-dir` / `--files-db` – on-disk directory + BoltDB metadata store for uploads.
- `--data-dir` – base directory used to auto-create per-peer folders (only applied when leaving the other file flags at their defaults).

## CLI / TUI Commands
- `/peers` – show live connections plus scheduler targets.
- `/history` – dump the in-memory buffer (size set by `--history`).
- `/save <path>` / `/load [N]` – write or replay persisted BoltDB history.
- `/msg <target> <text>` – direct message by nickname or address.
- `/nick <name>` – change display name and broadcast a handshake.
- `/stats` – view sent/seen/ack metrics.
- `/block <who>` / `/unblock <who>` / `/blocked` – manage in-memory block list.
- `/quit` – exit gracefully.

## Web Experience
- **Onboarding:** the multi-step `index.html` flow gathers credentials and workspace (bootstrap/auth presets) before redirecting to `/chat`.
- **Layout shell:** `app.html` + `app.css` define the `AppLayout` (SideNav, TopBar, panels, notifications drawer). Each region is documented inline, and the SPA is split into ES modules (`state.js`, `ws.js`, `ui/*`, `components/*`).
- **Chat panel:** `ui/chat.js` renders threaded messages via `components/messageBubble.js`, exposes the DM field, emoji grid, drag/drop zone, and the explicit **Send** button powered by the central store.
- **Files panel:** `ui/files.js` talks to `/api/files` for uploads/downloads, tracks progress bars, and publishes notifications + WS events so every peer sees new transfers.
- **Settings panel:** `ui/settings.js` offers device labels, notification toggles, and reuses the store to broadcast changes (with inline feedback/toasts).
- **Theme + Service Worker:** `ui/theme.js` keeps the dark/light toggle in sync across the sidebar + header, while `sw.js` pre-caches the shell and seeds push-notification plumbing.
- **Notifications:** the drawer listens to both SSE (mentions/system) and WebSocket file events, feeds Browser Notifications when allowed, and stacks alerts into `system` vs `mentions` tabs.
- **Session enforcement:** `/ws` upgrades only when the provided token validates with `authutil` and optionally informs the peer (via `onSession`) so CLI/TUI output stays in sync.

## Auth API
The auth server (chi + pgx) exposes:

| Method | Path       | Description                                  |
| ------ | ---------- | -------------------------------------------- |
| POST   | `/register`| Store a bcrypt-hashed user (unique username) |
| POST   | `/login`   | Verify password, return JWT + username       |
| POST   | `/messages`| Authenticated peers persist outbound content |
| GET    | `/history` | Authenticated fetch of recent chat/dm events |

JWTs are signed via `internal/authutil` and validated both at the WebSocket boundary and inside peer handshakes so impersonation attempts are rejected.

## Development Notes
- `go build ./...` and `go test ./...` to validate Go changes; run the lightweight UI checks with `node cmd/peer/webui/static/ui/__tests__/theme.test.mjs` and `node cmd/peer/webui/static/ui/__tests__/settings.test.mjs`.
- Run `gofmt ./...` before committing.
- The repo intentionally stays dependency-light: chi, pgx, gorilla/websocket, and `golang-jwt` cover all external needs; the browser UI is written in plain ES modules (no bundler required).

## Build & QA Checklist
Run these steps before publishing a change to ensure Go, UI, and browser features stay aligned:

```powershell
cd d:\NYCU_subjects\114-1\GO\Project\p2p-chat
gofmt -w cmd\peer\web_bridge.go cmd\peer\history_store.go cmd\peer\files_store.go
go build ./...
node cmd/peer/webui/static/ui/__tests__/theme.test.mjs
node cmd/peer/webui/static/ui/__tests__/settings.test.mjs
```

Manual verification (each item maps to the commit 5 blueprint requirements):
- Toggle the light/dark theme from both the sidebar and header, confirm the palette updates instantly, and refresh to ensure persistence via `state.js`.
- Send a chat using the **Send** button and ensure the WebSocket echo plus BoltDB history update; verify the composer textarea stays aligned with the button at various viewport widths.
- Open Settings to flip notification/device toggles and observe toasts/snackbars acknowledging the change.
- Upload a file via the Files panel, watch transfer progress, and confirm all connected peers see the notification drawer entry.
- Allow browser notifications once, then trigger a mention to verify both the drawer stack and the OS-level toast fire while the service worker caches the shell.
- Inspect the side nav, top bar, chat panel, and notifications drawer to ensure divider borders remain visible in both light and dark themes.

## Helpful Tips
- Use distinct `--web-addr` ports per peer so each serves an isolated browser UI.
- Point multiple peers at the same auth server to share login state and cloud history.
- When running CLI-only peers, obtain a token from the auth server (or reuse one from localStorage) and pass `--username/--token` to keep the mesh identity consistent with the authenticated one.
- Per-peer history/files now land in `p2p-data/<host>-<port>/` by default; delete that folder to reset a peer or set `--data-dir` to relocate the storage root.
