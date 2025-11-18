import { initState, getState } from '../../state.js';
import { applyTheme, cycleTheme } from '../theme.js';

// Minimal DOM/localStorage stubs for Node.
globalThis.document = { body: { dataset: {} } };
const memoryStorage = new Map();
globalThis.localStorage = {
  getItem: (key) => (memoryStorage.has(key) ? memoryStorage.get(key) : null),
  setItem: (key, value) => memoryStorage.set(key, value),
  removeItem: (key) => memoryStorage.delete(key),
};

initState({ username: 'tester', token: 'token', authApi: 'http://example' });
applyTheme('light');
console.assert(document.body.dataset.theme === 'light', 'applyTheme should set body dataset');

const next = cycleTheme();
console.assert(getState().settings.theme === next, 'cycleTheme should persist in state');

console.log('ui/theme tests passed');
