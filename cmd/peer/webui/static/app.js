const messages = document.getElementById("messages");
const input = document.getElementById("input");
const target = document.getElementById("target");
const peers = document.getElementById("peer-list");
const buttons = document.querySelectorAll(".buttons button");

const proto = location.protocol === "https:" ? "wss" : "ws";
const socket = new WebSocket(`${proto}://${location.host}/ws`);

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
      updatePeers(payload.peers || []);
      break;
    case "history":
      (payload.history || []).forEach(renderMessage);
      break;
    default:
      break;
  }
});

socket.addEventListener("close", () => {
  renderSystem("websocket disconnected");
});

function renderMessage(msg) {
  if (!msg) return;
  const wrapper = document.createElement("div");
  wrapper.className = "message";
  if (msg.type === "dm") {
    wrapper.classList.add("dm");
  }
  const meta = document.createElement("div");
  meta.className = "meta";
  const ts = new Date(msg.timestamp).toLocaleTimeString();
  const label = msg.type === "dm" ? " (DM)" : "";
  meta.textContent = `[${ts}] ${msg.from || "unknown"}${label}`;
  const body = document.createElement("div");
  body.className = "body";
  body.textContent = msg.content;
  wrapper.append(meta, body);
  messages.append(wrapper);
  messages.scrollTop = messages.scrollHeight;
}

function renderSystem(text) {
  const wrapper = document.createElement("div");
  wrapper.className = "message system";
  const body = document.createElement("div");
  body.className = "body";
  body.textContent = text;
  wrapper.append(body);
  messages.append(wrapper);
  messages.scrollTop = messages.scrollHeight;
}

function updatePeers(list) {
  peers.innerHTML = "";
  list.forEach((peer) => {
    const li = document.createElement("li");
    li.textContent = peer;
    peers.append(li);
  });
}

buttons.forEach((btn) =>
  btn.addEventListener("click", () => {
    send(btn.dataset.cmd);
  })
);

document.getElementById("composer").addEventListener("submit", (evt) => {
  evt.preventDefault();
  const text = input.value.trim();
  if (!text) return;
  if (target.value.trim()) {
    send(`/msg ${target.value.trim()} ${text}`);
  } else {
    send(text);
  }
  input.value = "";
});

function send(text) {
  if (socket.readyState !== WebSocket.OPEN) {
    renderSystem("socket not ready");
    return;
  }
  socket.send(text);
}
