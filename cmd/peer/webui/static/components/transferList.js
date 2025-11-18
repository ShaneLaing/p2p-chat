// components/transferList.js
// ---------------------------
// Displays uploads/downloads with progress bars, status chips, and retry hooks.

/**
 * @param {HTMLElement} container
 * @param {Array<object>} transfers
 * @param {object} options
 * @param {string} options.filter - 'uploads' | 'downloads' | 'all'
 * @param {(transfer: object) => void} [options.onRetry]
 */
export function renderTransferList(container, transfers = [], { filter = 'all', onRetry } = {}) {
  container.innerHTML = '';
  const subset = transfers.filter((item) => {
    if (filter === 'all') return true;
    return item.direction === filter;
  });
  if (!subset.length) {
    const empty = document.createElement('p');
    empty.className = 'text-muted';
    empty.textContent = 'No transfers yet.';
    container.appendChild(empty);
    return;
  }
  subset.forEach((item) => {
    const row = document.createElement('div');
    row.className = 'file-row';
    const meta = document.createElement('div');
    meta.innerHTML = `
      <strong>${item.name}</strong>
      <div class="text-muted">${formatBytes(item.size)} · ${item.uploader || 'peer'} · ${item.status || 'pending'}</div>
    `;

    const actions = document.createElement('div');

    if (item.status === 'error') {
      const retry = document.createElement('button');
      retry.className = 'pill';
      retry.textContent = 'Retry';
      retry.addEventListener('click', () => onRetry?.(item));
      actions.appendChild(retry);
    } else if (item.downloadUrl) {
      const download = document.createElement('button');
      download.className = 'pill';
      download.textContent = 'Download';
      download.addEventListener('click', () => {
        window.open(item.downloadUrl, '_blank');
      });
      actions.appendChild(download);
    }

    const progress = document.createElement('div');
    progress.className = 'transfer-progress';
    const bar = document.createElement('div');
    bar.style.width = `${Math.min(item.progress ?? 0, 100)}%`;
    progress.appendChild(bar);

    row.append(meta, progress, actions);
    container.appendChild(row);
  });
}

function formatBytes(size = 0) {
  if (!size) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB'];
  const order = Math.min(Math.floor(Math.log(size) / Math.log(1024)), units.length - 1);
  return `${(size / Math.pow(1024, order)).toFixed(1)} ${units[order]}`;
}
