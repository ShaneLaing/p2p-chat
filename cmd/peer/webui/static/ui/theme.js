// ui/theme.js
// -------------
// Theme controls appear in both the sidebar and the top bar. Toggling either
// button updates the shared store so the rest of the UI stays in sync.

import { subscribe, setTheme, getState } from '../state.js';

// Shared list so we can reflect the active theme on both toggle buttons.
const toggleButtons = [];

export function initThemeControls() {
  const candidates = [
    document.getElementById('theme-toggle'),
    document.getElementById('theme-toggle-inline'),
  ].filter(Boolean);

  toggleButtons.splice(0, toggleButtons.length, ...candidates);

  toggleButtons.forEach((btn) => {
    // Keep event binding tiny so both header + sidebar stay in sync.
    btn.addEventListener('click', cycleTheme);
  });

  subscribe('settings', (evt) => syncThemeUI(evt.detail.theme));
  syncThemeUI(getState().settings.theme);
}

function syncThemeUI(theme) {
  applyTheme(theme);
  toggleButtons.forEach((btn) => {
    btn.setAttribute('aria-pressed', (theme === 'dark').toString());
    btn.dataset.activeTheme = theme;
  });
}

export function applyTheme(theme) {
  if (typeof document === 'undefined') return;
  // Write to both <body> and <html> so :root[data-theme] selectors react instantly.
  if (document.body) {
    document.body.dataset.theme = theme;
  }
  const root = document.documentElement || document.body;
  if (root) {
    root.dataset.theme = theme;
  }
}

export function cycleTheme() {
  const next = getState().settings.theme === 'dark' ? 'light' : 'dark';
  setTheme(next); // Emits and persists to localStorage via state.js
  return next;
}
