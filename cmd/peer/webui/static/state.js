// state.js
// ---------
// Lightweight reactive store (no framework). Modules subscribe to named keys
// and receive `{ detail }` payloads similar to DOM events, but implemented via
// a tiny pub/sub to keep the code runnable in both browser and Node (tests).

const listeners = new Map();

const defaultState = () => ({
  auth: { username: '', token: '', authApi: '' },
  peers: [],
  messages: [],
  notifications: {
    system: [],
    mentions: [],
  },
  transfers: [],
  settings: {
    theme: 'dark',
    desktopNotifications: true,
    pushSubscription: null,
    deviceLabel: 'This Browser',
  },
  ui: {
    activePanel: 'chat',
    notificationStack: 'system',
  },
});

const state = defaultState();

function emit(key, payload) {
  const handlers = listeners.get(key);
  if (!handlers) return;
  handlers.forEach((handler) => handler({ detail: payload }));
}

export function initState({ username, token, authApi }) {
  state.auth = { username, token, authApi };
  const storedTheme = typeof localStorage !== 'undefined' ? localStorage.getItem('theme') : null;
  if (storedTheme) {
    state.settings.theme = storedTheme;
  }
  emit('auth', state.auth);
  emit('settings', state.settings);
  emit('ui', state.ui);
}

export function subscribe(key, handler) {
  if (!listeners.has(key)) {
    listeners.set(key, new Set());
  }
  const set = listeners.get(key);
  set.add(handler);
  return () => set.delete(handler);
}

export function getState() {
  return state;
}

export function appendMessage(msg, { prepend = false } = {}) {
  if (prepend) {
    state.messages.unshift(msg);
  } else {
    state.messages.push(msg);
  }
  emit('messages', state.messages);
}

export function replaceHistory(history) {
  state.messages = history.slice();
  emit('messages', state.messages);
}

export function setPeers(peers) {
  state.peers = peers;
  emit('peers', state.peers);
}

export function pushNotification(stack, payload) {
  const list = state.notifications[stack];
  if (!list) {
    state.notifications[stack] = [];
  }
  state.notifications[stack].unshift(payload);
  emit('notifications', state.notifications);
}

export function clearNotifications(stack) {
  if (stack) {
    state.notifications[stack] = [];
  } else {
    state.notifications.system = [];
    state.notifications.mentions = [];
  }
  emit('notifications', state.notifications);
}

export function setTheme(theme) {
  state.settings.theme = theme;
  if (typeof localStorage !== 'undefined') {
    localStorage.setItem('theme', theme);
  }
  emit('settings', state.settings);
}

export function updateSettings(patch) {
  state.settings = { ...state.settings, ...patch };
  emit('settings', state.settings);
}

export function setActivePanel(panel) {
  state.ui.activePanel = panel;
  emit('ui', state.ui);
}

export function setNotificationStack(stack) {
  state.ui.notificationStack = stack;
  emit('ui', state.ui);
}

export function upsertTransfer(entry) {
  const idx = state.transfers.findIndex((item) => item.id === entry.id);
  if (idx >= 0) {
    state.transfers[idx] = { ...state.transfers[idx], ...entry };
  } else {
    state.transfers.unshift(entry);
  }
  emit('transfers', state.transfers);
}

export function setTransfers(entries) {
  state.transfers = entries.slice();
  emit('transfers', state.transfers);
}
