const username = localStorage.getItem("username");
const token = localStorage.getItem("token");
const AUTH_API = localStorage.getItem("auth_api") || "http://127.0.0.1:8089";

if (!username || !token) {
  window.location.href = "/";
}

const messagesEl = document.getElementById("messages");
const inputEl = document.getElementById("input");
const targetEl = document.getElementById("target");
const userListEl = document.getElementById("user-list");
const buttons = document.querySelectorAll(".command-bar button");
const composer = document.getElementById("composer");
const logoutBtn = document.getElementById("logout-btn");
const meLabel = document.getElementById("me-name");

meLabel.textContent = username;

logoutBtn.addEventListener("click", () => {
  localStorage.removeItem("token");
  localStorage.removeItem("username");
  window.location.href = "/";
});

buttons.forEach((btn) =>
  btn.addEventListener("click", () => send(btn.dataset.cmd))
);

composer.addEventListener("submit", (evt) => {
  evt.preventDefault();
  const text = inputEl.value.trim();
  if (!text) return;
  if (targetEl.value.trim()) {
    send(`/msg ${targetEl.value.trim()} ${text}`);
  } else {
    send(text);
  }
  inputEl.value = "";
});

const proto = location.protocol === "https:" ? "wss" : "ws";
const wsUrl = `${proto}://${location.host}/ws?username=${encodeURIComponent(
  username
)}&token=${encodeURIComponent(token)}`;
const socket = new WebSocket(wsUrl);

socket.addEventListener("message", (evt) => {
  const payload = JSON.parse(evt.data);
  switch (payload.kind) {
    case "message":
      renderMessage(payload.message);
      break;
    case "system":
      renderSystem(payload.text);
      break;
    case "peers":
      updateUsers(payload.users || []);
      break;
    case "history":
      (payload.history || []).forEach((msg) => renderMessage(msg, { prepend: true }));
      break;
    default:
      break;
  }
});

socket.addEventListener("close", () => {
  renderSystem("WebSocket disconnected");
});

function renderMessage(msg, opts = {}) {
  if (!msg) return;
  const bubble = document.createElement("div");
  bubble.className = "bubble";
  const mine = msg.from === username;
  bubble.classList.add(mine ? "mine" : "theirs");
  if (msg.type === "dm") {
    bubble.classList.add("dm");
  }
  const meta = document.createElement("div");
  meta.className = "meta";
  const ts = msg.timestamp ? new Date(msg.timestamp) : new Date();
  const timeLabel = ts instanceof Date && !isNaN(ts) ? ts.toLocaleTimeString() : "";
  const dmLabel = msg.type === "dm" ? " (DM)" : "";
  meta.textContent = `[${timeLabel}] ${msg.from || "unknown"}${dmLabel}`;
  const body = document.createElement("div");
  body.className = "body";
  body.textContent = msg.content;
  bubble.append(meta, body);
  if (opts.prepend) {
    messagesEl.prepend(bubble);
  } else {
    messagesEl.append(bubble);
    messagesEl.scrollTop = messagesEl.scrollHeight;
  }
}

function renderSystem(text) {
  const bubble = document.createElement("div");
  bubble.className = "bubble system";
  const body = document.createElement("div");
  body.className = "body";
  body.textContent = text;
  bubble.append(body);
  messagesEl.append(bubble);
  messagesEl.scrollTop = messagesEl.scrollHeight;
}

function updateUsers(users) {
  userListEl.innerHTML = "";
  users.forEach((user) => {
    const li = document.createElement("li");
    const name = user.name || user.addr;
    const status = user.online ? "🟢" : "⚪";
    li.textContent = `${status} ${name}`;
    userListEl.append(li);
  });
}

function send(text) {
  if (socket.readyState !== WebSocket.OPEN) {
    renderSystem("Connection not ready");
    return;
  }
  socket.send(text);
}

async function loadHistory() {
  try {
    const res = await fetch(
      `${AUTH_API}/history?user=${encodeURIComponent(username)}`,
      {
        headers: { Authorization: `Bearer ${token}` },
      }
    );
    if (!res.ok) {
      return;
    }
    const records = await res.json();
    records.reverse().forEach((record) => {
      renderMessage(
        {
          type: record.receiver ? "dm" : "chat",
          from: record.sender,
          content: record.content,
          timestamp: record.timestamp,
        },
        { prepend: true }
      );
    });
  } catch (err) {
    console.error("history load failed", err);
  }
}

loadHistory();
