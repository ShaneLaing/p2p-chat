// sw.js
// -----
// Minimal service worker used for offline shell caching + push notification
// scaffolding. The cache keeps the layout shell responsive even if the peer is
// temporarily offline.

const CACHE_NAME = 'p2p-chat-shell-v1';
const SHELL_ASSETS = ['/static/app.css', '/static/app.js', '/static/app.html'];

self.addEventListener('install', (event) => {
  event.waitUntil(
    caches
      .open(CACHE_NAME)
      .then((cache) => cache.addAll(SHELL_ASSETS))
      .then(() => self.skipWaiting()),
  );
});

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches.keys().then((keys) =>
      Promise.all(
        keys.map((key) => {
          if (key !== CACHE_NAME) {
            return caches.delete(key);
          }
          return undefined;
        }),
      ),
    ),
  );
  self.clients.claim();
});

self.addEventListener('fetch', (event) => {
  if (event.request.method !== 'GET') return;
  event.respondWith(
    caches.match(event.request).then((cached) => cached || fetch(event.request)),
  );
});

self.addEventListener('push', (event) => {
  const data = event.data ? event.data.json() : {};
  const title = data.title || 'P2P Chat';
  const options = { body: data.body || 'New notification' };
  event.waitUntil(self.registration.showNotification(title, options));
});
