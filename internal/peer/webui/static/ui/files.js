// ui/files.js
// ----------
// File transfer UI: fetch existing uploads, manage new uploads with progress
// reporting, and surface download buttons + notifications when transfers
// finish.

import { subscribe, getState, setTransfers, upsertTransfer, pushNotification } from '../state.js';
import { renderTransferList } from '../components/transferList.js';

let activeFilter = 'all';

export function initFilesUI() {
  const container = document.getElementById('file-list');
  if (!container) return;
  const filterButtons = document.querySelectorAll('[data-filter]');
  filterButtons.forEach((btn) => {
    btn.addEventListener('click', () => {
      activeFilter = btn.dataset.filter || 'all';
      renderTransferList(container, getState().transfers, { filter: mapFilter(activeFilter), onRetry: retryTransfer });
    });
  });

  subscribe('transfers', (evt) => {
    renderTransferList(container, evt.detail, { filter: mapFilter(activeFilter), onRetry: retryTransfer });
  });
  renderTransferList(container, getState().transfers, { filter: mapFilter(activeFilter), onRetry: retryTransfer });
  bootstrapTransfers();
}

function mapFilter(filter) {
  if (filter === 'uploads' || filter === 'downloads') return filter;
  return 'all';
}

async function bootstrapTransfers() {
  try {
    const res = await fetch('/api/files', { headers: buildAuthHeaders() });
    if (!res.ok) return;
    const entries = await res.json();
    const enriched = entries.map(enrichTransfer);
    setTransfers(enriched);
  } catch (err) {
    console.error('files bootstrap failed', err);
  }
}

function enrichTransfer(entry) {
  const { auth } = getState();
  return {
    ...entry,
    status: entry.status || 'complete',
    direction: entry.uploader === auth.username ? 'uploads' : 'downloads',
    downloadUrl: `/api/files/${encodeURIComponent(entry.id)}?username=${encodeURIComponent(auth.username)}&token=${encodeURIComponent(auth.token)}`,
    progress: entry.status === 'complete' ? 100 : entry.progress || 0,
  };
}

export function uploadFile(file) {
  if (!file) return;
  const { auth } = getState();
  const tempId = `local-${Date.now()}`;
  upsertTransfer({
    id: tempId,
    name: file.name,
    size: file.size,
    uploader: auth.username,
    status: 'uploading',
    progress: 0,
    direction: 'uploads',
  });

  const xhr = new XMLHttpRequest();
  xhr.open('POST', '/api/files');
  xhr.setRequestHeader('Authorization', `Bearer ${auth.token}`);

  xhr.upload.onprogress = (evt) => {
    if (!evt.lengthComputable) return;
    const pct = Math.round((evt.loaded / evt.total) * 100);
    upsertTransfer({ id: tempId, progress: pct, status: 'uploading' });
  };

  xhr.onerror = () => {
    upsertTransfer({ id: tempId, status: 'error' });
  };

  xhr.onload = () => {
    if (xhr.status >= 200 && xhr.status < 300) {
      const meta = enrichTransfer(JSON.parse(xhr.responseText));
      upsertTransfer({ ...meta, progress: 100, status: 'complete', id: meta.id });
      pushNotification('system', {
        title: 'Upload complete',
        text: `${meta.name} is ready to download`,
        timestamp: new Date().toISOString(),
      });
    } else {
      upsertTransfer({ id: tempId, status: 'error' });
    }
  };

  const form = new FormData();
  form.append('file', file);
  xhr.send(form);
}

function retryTransfer(entry) {
  if (entry.direction === 'downloads' && entry.downloadUrl) {
    window.open(entry.downloadUrl, '_blank');
    return;
  }
  // Upload retries would require re-selecting the file; prompt the user.
  alert('Please select the file again to retry the upload.');
}

function buildAuthHeaders() {
  const { auth } = getState();
  return { Authorization: `Bearer ${auth.token}` };
}
