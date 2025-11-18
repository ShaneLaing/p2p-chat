// ui/notifications.js
// -------------------
// Slide-over drawer + SSE bridge. Routes notifications into the store (system
// vs mention stacks) and mirrors them in the UI + browser Notification API.

import { subscribe, getState, pushNotification, setNotificationStack } from '../state.js';
import { renderNotificationList } from '../components/notificationList.js';

let eventSource;

export function initNotificationsUI() {
  const openBtn = document.getElementById('notifications-toggle');
  const closeBtn = document.getElementById('notifications-close');
  const drawer = document.getElementById('notifications');
  const list = document.getElementById('notifications-list');
  const tabButtons = document.querySelectorAll('[data-stack]');

  if (!drawer || !list) {
    return;
  }

  const render = () => {
    const stack = getState().ui.notificationStack;
    const records = getState().notifications[stack] || [];
    renderNotificationList(list, records);
  };

  subscribe('notifications', render);
  subscribe('ui', (evt) => {
    if (evt.detail.notificationStack) {
      render();
    }
  });
  render();

  const toggle = (show) => drawer.classList.toggle('open', show);
  openBtn?.addEventListener('click', () => toggle(true));
  closeBtn?.addEventListener('click', () => toggle(false));

  tabButtons.forEach((btn) =>
    btn.addEventListener('click', () => {
      setNotificationStack(btn.dataset.stack);
      render();
    }),
  );

  initNotificationStream();
}

function initNotificationStream() {
  const { auth } = getState();
  const proto = location.protocol === 'https:' ? 'https' : 'http';
  const url = `${proto}://${location.host}/events?username=${encodeURIComponent(auth.username)}&token=${encodeURIComponent(auth.token)}`;
  if (eventSource) {
    eventSource.close();
  }
  eventSource = new EventSource(url);
  eventSource.onmessage = (evt) => {
    try {
      const payload = JSON.parse(evt.data);
      if (payload.kind === 'notification' && payload.notification) {
        routeNotification(payload.notification);
      }
    } catch (err) {
      console.error('sse parse failed', err);
    }
  };
  eventSource.onerror = () => {
    eventSource.close();
    setTimeout(() => initNotificationStream(), 3000);
  };
}

function routeNotification(notification) {
  const stack = notification.level === 'mention' || notification.level === 'dm' ? 'mentions' : 'system';
  pushNotification(stack, notification);
  maybeNotifyBrowser(notification);
}

function maybeNotifyBrowser(notification) {
  if (!('Notification' in window)) return;
  const { settings } = getState();
  if (!settings.desktopNotifications) return;
  if (Notification.permission === 'default') {
    Notification.requestPermission();
    return;
  }
  if (Notification.permission !== 'granted') return;
  new Notification(notification.text || 'New notification', {
    body: notification.from ? `${notification.from}: ${notification.text}` : notification.text,
  });
}
