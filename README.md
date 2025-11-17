# p2p-chat

A minimal peer-to-peer chat playground written in Go 1.21+. Each peer keeps both a TCP server (for inbound peers) and a TCP client (for outbound dials) and floods chat messages across its mesh. A lightweight HTTP bootstrap server tracks which peers are online.

## Features
- Bootstrap HTTP server with `/register` and `/peers` endpoints (step 2 in `req.md`).
- TCP peer process that both listens and dials other peers (step 4/5).
- AES-256-GCM message encryption when `--secret` is supplied (TCP payloads are encrypted before writing to the socket).
- Deduplicated flooding using per-message IDs to avoid loops.
- CLI UX niceties (steps 9-11): nicknames, `/peers`, `/history`, `/quit`, bounded history buffer, ANSI-colored timestamps/names (disable via `--no-color`).
- Periodic bootstrap polling so newly joined peers eventually form a mesh.

## Getting Started

```powershell
cd d:\NYCU_subjects\114-1\GO\Project\p2p-chat
go run ./cmd/bootstrap --addr=:8000
```

Open other terminals for peers:

```powershell
cd d:\NYCU_subjects\114-1\GO\Project\p2p-chat
# Alice on port 9001 with encryption secret
go run ./cmd/peer --port=9001 --nick=Alice --secret=topsecret

# Bob on port 9002, same secret so they can talk
go run ./cmd/peer --port=9002 --nick=Bob --secret=topsecret
```

Type messages into any peer terminal; they will appear on the others. Peers without the same `--secret` are unable to decrypt the payloads.

### Useful Flags
- `--bootstrap`: bootstrap base URL (`http://host:port`).
- `--listen`: custom `host:port` instead of `--port`.
- `--secret`: shared passphrase enabling AES-256-GCM encryption.
- `--nick`: nickname displayed next to each message.
- `--history`: number of locally stored messages used by `/history`.
- `--poll`: how often to refresh the peer list from the bootstrap server.
- `--no-color`: disable ANSI colors in CLI output.

### CLI Commands
- `/peers` – show currently connected sockets.
- `/history` – dump the local history buffer (default 200 entries).
- `/quit` – exit the peer cleanly.

## Development Notes
- Run `gofmt ./...` after making changes.
- Both binaries are standalone; no extra dependencies beyond the Go toolchain.
- Encryption derives a 256-bit key from the shared secret using `scrypt` and per-message random nonces.
