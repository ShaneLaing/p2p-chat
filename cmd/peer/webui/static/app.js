import { initState, appendMessage, getState } from './state.js';
import { initTransport } from './ws.js';
import { initThemeControls } from './ui/theme.js';
import { initChatUI } from './ui/chat.js';
import { initFilesUI } from './ui/files.js';
import { initSettingsUI } from './ui/settings.js';
import { initNotificationsUI } from './ui/notifications.js';

// Session bootstrap ---------------------------------------------------------
const username = localStorage.getItem('username');
const token = localStorage.getItem('token');
const authApi = localStorage.getItem('auth_api') || 'http://127.0.0.1:8089';

if (!username || !token) {
  window.location.href = '/';
}

initState({ username, token, authApi });
document.getElementById('me-name').textContent = username;

document.getElementById('logout-btn')?.addEventListener('click', () => {
  localStorage.clear();
  window.location.href = '/';
});

// UI + transport wiring -----------------------------------------------------
initThemeControls();
initChatUI();
initFilesUI();
initSettingsUI();
initNotificationsUI();

initTransport({
  onDisconnect: () => appendMessage({ type: 'system', content: 'WebSocket disconnected' }),
});

prefetchHistory();
registerServiceWorker();

async function prefetchHistory() {
  try {
    const res = await fetch(`${authApi}/history?user=${encodeURIComponent(username)}`, {
      headers: { Authorization: `Bearer ${token}` },
    });
    if (!res.ok) return;
    const records = await res.json();
    const normalized = records.map((record) => ({
      type: record.receiver ? 'dm' : 'chat',
      from: record.sender,
      content: record.content,
      timestamp: record.timestamp,
    }));
    normalized.reverse().forEach((msg) => appendMessage(msg, { prepend: true }));
  } catch (err) {
    console.error('history load failed', err);
  }
}

async function registerServiceWorker() {
  if (!('serviceWorker' in navigator)) return;
  try {
    await navigator.serviceWorker.register('/static/sw.js');
  } catch (err) {
    console.warn('sw register failed', err);
  }
}

// Expose state getter for console debugging.
window.__appState = getState;
