import { initState, getState } from '../../state.js';
import { applySettingChange } from '../settings.js';

// DOM/localStorage stubs.
const memoryStorage = new Map();
const feedbackNode = {
  textContent: '',
  classList: {
    add() {},
    remove() {},
  },
};

globalThis.document = {
  body: { dataset: {} },
  getElementById: (id) => (id === 'settings-feedback' ? feedbackNode : null),
};

globalThis.localStorage = {
  getItem: (key) => (memoryStorage.has(key) ? memoryStorage.get(key) : null),
  setItem: (key, value) => memoryStorage.set(key, value),
  removeItem: (key) => memoryStorage.delete(key),
};

globalThis.Notification = { permission: 'denied', requestPermission: () => Promise.resolve('granted') };

initState({ username: 'tester', token: 'token', authApi: 'http://example' });
applySettingChange('desktopNotifications', false);
console.assert(getState().settings.desktopNotifications === false, 'desktopNotifications toggle should sync to state');

applySettingChange('deviceLabel', 'Office Rig');
console.assert(getState().settings.deviceLabel === 'Office Rig', 'device label should persist');

console.log('ui/settings tests passed');
