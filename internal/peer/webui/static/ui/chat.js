// ui/chat.js
// ----------
// Chat workspace wiring: navigation, presence, timeline rendering, and composer
// behavior (emoji picker, file uploads, send button invocation).

import { subscribe, getState, appendMessage, setActivePanel } from '../state.js';
import { sendLine } from '../ws.js';
import { uploadFile } from './files.js';
import { mountComposerControls } from '../components/composer.js';
import { createMessageBubble } from '../components/messageBubble.js';

const COMMANDS = [
  { label: 'Peers', cmd: '/peers' },
  { label: 'History', cmd: '/history' },
  { label: 'Stats', cmd: '/stats' },
];

const QUICK_EMOJIS = ['😀', '😎', '🔥', '🎉', '👍', '❤️'];

export function initChatUI() {
  mountNav();
  mountCommandButtons();
  mountPresence();
  mountMessages();
  mountComposer();
}

function mountNav() {
  const navButtons = document.querySelectorAll('.nav-item[data-panel]');
  navButtons.forEach((btn) => {
    btn.addEventListener('click', () => {
      const panel = btn.dataset.panel;
      setActivePanel(panel);
      reflectActivePanel(panel);
    });
  });
  subscribe('ui', (evt) => reflectActivePanel(evt.detail.activePanel));
  reflectActivePanel(getState().ui.activePanel);
}

function reflectActivePanel(active) {
  document.querySelectorAll('.panel').forEach((section) => {
    const isActive = section.id === `panel-${active}`;
    section.classList.toggle('is-active', isActive);
    section.setAttribute('aria-hidden', (!isActive).toString());
  });
}

function mountCommandButtons() {
  const bar = document.getElementById('command-buttons');
  bar.innerHTML = '';
  COMMANDS.forEach(({ label, cmd }) => {
    const button = document.createElement('button');
    button.className = 'command-button';
    button.textContent = label;
    button.addEventListener('click', () => sendLine(cmd));
    bar.appendChild(button);
  });
}

function mountPresence() {
  const container = document.getElementById('presence');
  subscribe('peers', (evt) => {
    reflectPresence(container, evt.detail);
  });
  reflectPresence(container, getState().peers);
}

function reflectPresence(container, peers = []) {
  container.innerHTML = '';
  peers.forEach((peer) => {
    const badge = document.createElement('span');
    badge.className = 'pill';
    badge.textContent = `${peer.online ? '🟢' : '⚪'} ${peer.name || peer.addr}`;
    container.appendChild(badge);
  });
}

function mountMessages() {
  const list = document.getElementById('messages');
  subscribe('messages', (evt) => renderMessages(list, evt.detail));
  renderMessages(list, getState().messages);
}

function renderMessages(list, messages = []) {
  list.innerHTML = '';
  const currentUser = getState().auth.username;
  messages.forEach((msg) => {
    const bubble = createMessageBubble(msg, currentUser);
    list.appendChild(bubble);
  });
  list.scrollTop = list.scrollHeight;
}

function mountComposer() {
  const targetField = document.getElementById('target');
  const emojiPanel = document.getElementById('emoji-panel');
  let resizeComposer;
  const { textarea } = mountComposerControls(document.getElementById('composer-root'), {
    onSubmit: (text) => {
      dispatchMessage({ text, target: targetField.value.trim() });
      textarea.value = '';
      resizeComposer?.();
    },
    onFile: handleFileUpload,
    onEmojiToggle: () => toggleEmojiPanel(textarea, emojiPanel),
  });

  resizeComposer = enforceComposerSizing(textarea);

  populateEmojiPanel(textarea, emojiPanel);
}

function dispatchMessage({ text, target }) {
  if (!text) return;
  if (target) {
    sendLine(`/msg ${target} ${text}`);
  } else {
    sendLine(text);
  }
}

function handleFileUpload(file) {
  if (!file) return;
  const targetField = document.getElementById('target');
  const target = targetField?.value.trim();
  appendMessage({ type: 'system', content: `Uploading ${file.name} (${file.size} bytes)` });
  uploadFile(file, { target });
}

function toggleEmojiPanel(textarea, panel) {
  panel.hidden = !panel.hidden;
  if (!panel.hidden && !panel.childElementCount) {
    populateEmojiPanel(textarea, panel);
  }
}

function populateEmojiPanel(textarea, panel) {
  if (panel.childElementCount) return;
  QUICK_EMOJIS.forEach((emoji) => {
    const btn = document.createElement('button');
    btn.type = 'button';
    btn.className = 'ghost';
    btn.textContent = emoji;
    btn.addEventListener('click', () => {
      textarea.value += emoji;
      textarea.focus();
    });
    panel.appendChild(btn);
  });
}

// Keep the composer textarea height responsive and aligned with the Send button.
function enforceComposerSizing(textarea) {
  if (!textarea) return;
  const min = 52;
  const max = 160;
  const resize = () => {
    textarea.style.height = 'auto';
    const next = Math.min(Math.max(textarea.scrollHeight, min), max);
    textarea.style.height = `${next}px`;
  };
  textarea.style.minHeight = `${min}px`;
  textarea.style.maxHeight = `${max}px`;
  textarea.style.width = '100%';
  textarea.addEventListener('input', resize);
  if (typeof window !== 'undefined') {
    window.addEventListener('resize', resize, { passive: true });
  }
  resize();
  return resize;
}
