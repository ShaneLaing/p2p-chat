// ws.js
// -----
// Handles WebSocket lifecycle + event fan-out. Messages feed the chat store,
// peer lists, notifications, and transfer updates.

import { appendMessage, replaceHistory, setPeers, getState, pushNotification, upsertTransfer } from './state.js';

let socket;

/**
 * Opens the WebSocket connection using the stored credentials. Automatically
 * reconnects when the socket closes unexpectedly.
 */
export function initTransport({ onDisconnect }) {
  const { auth } = getState();
  const proto = location.protocol === 'https:' ? 'wss' : 'ws';
  const url = `${proto}://${location.host}/ws?username=${encodeURIComponent(auth.username)}&token=${encodeURIComponent(auth.token)}`;
  socket = new WebSocket(url);

  socket.addEventListener('message', (evt) => handleEvent(evt));
  socket.addEventListener('close', () => {
    onDisconnect?.();
    // naive reconnect with exponential-ish backoff could be added later
    setTimeout(() => initTransport({ onDisconnect }), 2000);
  });
}

function handleEvent(evt) {
  try {
    const payload = JSON.parse(evt.data);
    switch (payload.kind) {
      case 'message':
        appendMessage(payload.message);
        break;
      case 'system':
        appendMessage({ type: 'system', content: payload.text, timestamp: new Date().toISOString() });
        break;
      case 'peers':
        setPeers(payload.users || []);
        break;
      case 'history':
        replaceHistory(payload.history || []);
        break;
      case 'notification':
        if (payload.notification) {
          const stack = payload.notification.level === 'mention' || payload.notification.level === 'dm' ? 'mentions' : 'system';
          pushNotification(stack, payload.notification);
        }
        break;
      case 'file':
        if (payload.file) {
          const { auth } = getState();
          upsertTransfer({
            ...payload.file,
            direction: payload.file.uploader === auth.username ? 'uploads' : 'downloads',
            downloadUrl: `/api/files/${encodeURIComponent(payload.file.id)}?username=${encodeURIComponent(auth.username)}&token=${encodeURIComponent(auth.token)}`,
            status: 'complete',
            progress: 100,
          });
        }
        break;
      default:
        break;
    }
  } catch (err) {
    console.error('ws decode failed', err);
  }
}

export function sendLine(text) {
  if (!socket || socket.readyState !== WebSocket.OPEN) {
    appendMessage({ type: 'system', content: 'WebSocket offline' });
    return;
  }
  socket.send(text);
}
