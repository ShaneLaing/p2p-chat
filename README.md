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
- A running Postgres instance (default DSN: `postgres://postgres:113550057@localhost:5432/p2p_local_server_backend`). Override with `DATABASE_URL` when starting `cmd/auth`.
- Modern browser for the web UI.

## Quick Start
1. **Bootstrap database (first run only):** create the database described above. The auth server auto-migrates `users` and `messages` tables.
2. **Run the auth service:**
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
- **Login screen:** talks to `cmd/auth` `/login` and `/register`, stores the JWT + username in `localStorage`, then redirects to `/chat`.
- **Chat surface:** WebSocket bridge subscribes to message/system/presence/history events; composer supports commands or direct DMs by filling the target field; sidebar lists online users with activity indicators.
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
- `go build ./...` and `go test ./...` to validate changes.
- Run `gofmt ./...` before committing.
- The repo intentionally stays dependency-light: chi, pgx, gorilla/websocket, and `golang-jwt` cover all external needs.

## Helpful Tips
- Use distinct `--web-addr` ports per peer so each serves an isolated browser UI.
- Point multiple peers at the same auth server to share login state and cloud history.
- When running CLI-only peers, obtain a token from the auth server (or reuse one from localStorage) and pass `--username/--token` to keep the mesh identity consistent with the authenticated one.
