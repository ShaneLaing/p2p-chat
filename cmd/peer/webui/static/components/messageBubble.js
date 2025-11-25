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
      const item = document.createElement('div');
      item.className = 'attachment-item';
      const downloadUrl = buildDownloadURL(attachment);
      if (attachment.mime && attachment.mime.startsWith('image/') && attachment.url) {
        const img = document.createElement('img');
        img.src = attachment.url;
        img.alt = attachment.name || 'Image';
        img.className = 'attachment-preview';
        img.loading = 'lazy';
        img.addEventListener('click', () => window.open(attachment.url, '_blank'));
        item.appendChild(img);
      }
      const chip = document.createElement('a');
      chip.className = 'attachment-chip';
      chip.textContent = attachment.name || 'Attachment';
      if (downloadUrl) {
        chip.href = downloadUrl;
        chip.target = '_blank';
        chip.rel = 'noopener noreferrer';
        chip.setAttribute('download', attachment.name || 'download');
      } else if (attachment.url) {
        chip.href = attachment.url;
        chip.target = '_blank';
        chip.rel = 'noopener noreferrer';
      }
      item.appendChild(chip);
      attachments.appendChild(item);
    });
    bubble.appendChild(attachments);
  }
  return bubble;
}

function buildDownloadURL(attachment) {
  if (!attachment || !attachment.url) return '';
  const url = new URL(attachment.url, window.location.origin);
  url.searchParams.set('download', '1');
  return url.toString();
}
