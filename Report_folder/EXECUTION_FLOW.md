# Peer Runtime Main Execution Flow

```mermaid
flowchart TD
    A[Peer start] --> B[Bootstrap register + fetch peers]
    B --> C[Dial scheduler enqueues targets]
    C --> D[ConnManager dials/listens via goroutines]
    D --> E[Incoming JSON forwarded to handleIncoming]
    E --> F{Message source?}
    F -->|CLI/TUI/Web input| G[processLine parses chat/DM/commands]
    G --> H[ConnManager.Broadcast floods message]
    H --> I[Ack tracker + history buffer update]
    F -->|Remote peers| J[processIncoming validates AuthToken]
    J --> K[Directory refresh + msgCache dedupe]
    K --> H
    H --> L[Optional persistExternal POST to auth server]
    L --> M[Auth server stores history/JWT]
    H --> N[WebBridge pushes over WebSocket]
    N --> O[Web UI renders + Service Worker caches files]
```

- Nodes follow the six bullets in REPORT.md §2 “主要執行流程”.
- Decision `F` branches between local UI input and remote peer traffic, converging at the broadcast layer.
- Optional steps (persistExternal, WebBridge) illustrate the async goroutines that keep UI responsive.
