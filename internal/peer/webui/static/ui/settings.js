// ui/settings.js
// -------------
// Renders the settings panel (profile snippets, notification toggles, device
// labels). Updates propagate through the centralized store so other modules can
// respond (theme controls, notification pipeline, etc.).

import { subscribe, getState, updateSettings } from '../state.js';

const SETTINGS_SCHEMA = [
  {
    key: 'desktopNotifications',
    label: 'Desktop notifications',
    description: 'Allow browser Notification API alerts for mentions and files.',
    type: 'toggle',
  },
  {
    key: 'deviceLabel',
    label: 'Device label',
    description: 'Displayed in the linked-devices list.',
    type: 'text',
    placeholder: 'e.g. Studio Laptop',
  },
];

let feedbackTimeout;

export function initSettingsUI() {
  const grid = document.getElementById('settings-grid');
  if (!grid) return;
  const render = (settings) => renderSettings(grid, settings);
  subscribe('settings', (evt) => render(evt.detail));
  render(getState().settings);
}

function renderSettings(container, settings) {
  container.innerHTML = '';
  SETTINGS_SCHEMA.forEach((field) => {
    const card = document.createElement('section');
    card.className = 'setting-card';
    const title = document.createElement('h3');
    title.textContent = field.label;
    const description = document.createElement('p');
    description.className = 'text-muted';
    description.textContent = field.description;

    if (field.type === 'toggle') {
      const toggle = document.createElement('button');
      toggle.className = `toggle ${settings[field.key] ? 'is-on' : ''}`;
      toggle.type = 'button';
      toggle.textContent = settings[field.key] ? 'On' : 'Off';
      toggle.addEventListener('click', () => {
        const next = !settings[field.key];
        applySettingChange(field.key, next);
      });
      card.append(title, description, toggle);
    } else if (field.type === 'text') {
      const input = document.createElement('input');
      input.type = 'text';
      input.value = settings[field.key] || '';
      input.placeholder = field.placeholder || '';
      input.addEventListener('change', () => applySettingChange(field.key, input.value.trim()));
      card.append(title, description, input);
    }

    container.appendChild(card);
  });

  const feedback = document.createElement('div');
  feedback.id = 'settings-feedback';
  feedback.className = 'settings-feedback';
  container.appendChild(feedback);
}

export function applySettingChange(key, value) {
  updateSettings({ [key]: value });
  if (key === 'desktopNotifications' && value) {
    requestNotificationPermission();
  }
  showFeedback('Settings saved');
}

function showFeedback(message) {
  const footer = document.getElementById('settings-feedback');
  if (!footer) return;
  footer.textContent = message;
  footer.classList.add('visible');
  clearTimeout(feedbackTimeout);
  feedbackTimeout = setTimeout(() => footer.classList.remove('visible'), 2000);
}

function requestNotificationPermission() {
  if (!('Notification' in window)) return;
  if (Notification.permission === 'denied') return;
  if (Notification.permission === 'granted') return;
  Notification.requestPermission();
}
