// components/messageBubble.js
// ----------------------------
// Stateless renderer that produces a DOM node for a chat message. Consumers
// pass the message payload and the current username so the component can decide
// whether to style the bubble as "mine" vs "theirs" as well as surface
// metadata (timestamp, DM badge, etc.).

/**
 * @param {object} message - message payload from the store.
 * @param {string} currentUser - username of the authenticated user.
 * @returns {HTMLElement}
 */
export function createMessageBubble(message, currentUser) {
  const bubble = document.createElement('article');
  bubble.className = 'message';
  if (message.from && message.from === currentUser) {
    bubble.classList.add('mine');
  }
  if (message.type === 'system') {
    bubble.classList.add('system');
  }
  const meta = document.createElement('div');
  meta.className = 'meta';
  const ts = message.timestamp ? new Date(message.timestamp).toLocaleTimeString() : '';
  if (message.type === 'system') {
    meta.textContent = 'System';
  } else {
    const target = message.to ? ` → ${message.to}` : '';
    meta.textContent = `${message.from || 'unknown'}${target} · ${ts}`;
  }
  const body = document.createElement('div');
  body.className = 'body';
  body.textContent = message.content || '';
  bubble.append(meta, body);
  if (message.attachments?.length) {
    const attachments = document.createElement('div');
    attachments.className = 'attachments';
    message.attachments.forEach((attachment) => {
      const chip = document.createElement('button');
      chip.type = 'button';
      chip.className = 'attachment-chip';
      chip.textContent = attachment.name || 'Attachment';
      chip.addEventListener('click', () => {
        if (attachment.url) {
          window.open(attachment.url, '_blank');
        }
      });
      attachments.appendChild(chip);
    });
    bubble.appendChild(attachments);
  }
  return bubble;
}
