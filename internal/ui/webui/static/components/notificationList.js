// components/notificationList.js
// --------------------------------
// Renders stacked notification cards for the slide-over drawer. The component
// is intentionally dumb: pass it the stack ("system" | "mentions") and the
// array of entries, it will handle DOM reconciliation.

/**
 * @param {HTMLElement} container
 * @param {Array<object>} items
 */
export function renderNotificationList(container, items = []) {
  container.innerHTML = '';
  if (!items.length) {
    const empty = document.createElement('p');
    empty.className = 'text-muted';
    empty.textContent = 'No notifications yet.';
    container.appendChild(empty);
    return;
  }
  items.forEach((item) => {
    const card = document.createElement('article');
    card.className = 'notification-item';
    const heading = document.createElement('div');
    heading.className = 'notification-title';
    heading.textContent = item.title || item.level?.toUpperCase() || 'Notice';
    const body = document.createElement('p');
    body.textContent = item.text || '';
    const meta = document.createElement('small');
    const ts = item.timestamp ? new Date(item.timestamp) : new Date();
    meta.textContent = ts.toLocaleTimeString();
    card.append(heading, body, meta);
    container.appendChild(card);
  });
}
