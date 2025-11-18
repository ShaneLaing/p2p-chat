// components/composer.js
// -----------------------
// Encapsulates the chat composer UI (emoji button, textarea, file picker, send
// button). The module emits callbacks when the user submits text, selects files,
// or opens the emoji grid so parent modules can wire business logic.

/**
 * Mounts the composer controls inside the provided container.
 * @param {HTMLElement} container
 * @param {object} hooks
 * @param {(text: string) => void} hooks.onSubmit
 * @param {(file: File) => void} hooks.onFile
 * @param {() => void} hooks.onEmojiToggle
 * @returns {{ textarea: HTMLTextAreaElement, fileInput: HTMLInputElement, emojiButton: HTMLButtonElement, sendButton: HTMLButtonElement }}
 */
export function mountComposerControls(container, { onSubmit, onFile, onEmojiToggle }) {
  container.innerHTML = '';

  const emojiButton = document.createElement('button');
  emojiButton.type = 'button';
  emojiButton.id = 'emoji-btn';
  emojiButton.className = 'ghost composer-icon';
  emojiButton.textContent = '😊';
  emojiButton.addEventListener('click', onEmojiToggle);

  const textarea = document.createElement('textarea');
  textarea.id = 'input';
  textarea.className = 'composer-textarea';
  textarea.rows = 2;
  textarea.placeholder = 'Type a message or /command';

  const fileLabel = document.createElement('label');
  fileLabel.className = 'file-btn composer-icon';
  fileLabel.textContent = '📎';
  const fileInput = document.createElement('input');
  fileInput.type = 'file';
  fileInput.id = 'file-input';
  fileInput.hidden = true;
  fileInput.addEventListener('change', (evt) => {
    const file = evt.target.files?.[0];
    if (file) onFile?.(file);
    evt.target.value = '';
  });
  fileLabel.appendChild(fileInput);

  const sendButton = document.createElement('button');
  sendButton.type = 'button';
  sendButton.id = 'send-btn';
  sendButton.className = 'primary composer-send';
  sendButton.textContent = 'Send';
  sendButton.addEventListener('click', () => {
    const text = textarea.value.trim();
    if (text) {
      onSubmit?.(text);
    }
  });

  container.append(emojiButton, textarea, fileLabel, sendButton);

  container.closest('form')?.addEventListener('submit', (evt) => {
    evt.preventDefault();
    const text = textarea.value.trim();
    if (text) {
      onSubmit?.(text);
    }
  });

  return { textarea, fileInput, emojiButton, sendButton };
}
