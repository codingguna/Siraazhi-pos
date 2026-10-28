/* ==========================================================================
   Siraazhi Pro POS — Service Worker (PWA Offline Cache)
   WHAT IT IS:
     A background worker script running inside the browser on client devices.
   HOW IT WORKS:
     Implements Network-First strategy for navigation and static assets:
     - On normal F5 / Ctrl+R, immediately fetches the latest version from server.
     - Automatically falls back to offline cache if network is temporarily unreachable.
     - Live REST APIs (/api/*) completely bypass cache for real-time data integrity.
   FLOW:
     1. install: Pre-caches critical frontend assets and skips waiting.
     2. activate: Cleans up obsolete caches from previous builds and claims clients.
     3. fetch: Network-First for navigation & UI assets, falling back to cache if offline.
   ========================================================================== */

const CACHE_NAME = 'siraazhi-pos-v9';
const STATIC_ASSETS = [
  '/',
  '/index.html',
  '/css/style.css',
  '/css/print.css',
  '/js/app.js',
  '/js/print.js',
  '/js/excel.js',
  '/js/pdf.js',
  '/js/qrcode.min.js',
  '/js/xlsx.full.min.js',
  '/manifest.json'
];

self.addEventListener('install', (event) => {
  self.skipWaiting();
});

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches.keys().then((keys) => {
      return Promise.all(
        keys.filter((key) => key !== CACHE_NAME).map((key) => caches.delete(key))
      );
    }).then(() => self.clients.claim())
  );
});

self.addEventListener('fetch', (event) => {
  const url = new URL(event.request.url);

  // 1. Live REST APIs always bypass cache completely
  if (url.pathname.startsWith('/api/')) {
    event.respondWith(fetch(event.request));
    return;
  }

  // 2. Navigation requests (HTML documents) - Network-first with cache fallback
  // Ensures normal F5 / Ctrl+R always loads fresh HTML from server without needing Shift+F5
  if (event.request.mode === 'navigate' || url.pathname === '/' || url.pathname.endsWith('.html')) {
    event.respondWith(
      fetch(event.request)
        .then((response) => {
          if (response && response.status === 200) {
            const clone = response.clone();
            caches.open(CACHE_NAME).then((cache) => cache.put(event.request, clone));
          }
          return response;
        })
        .catch(() => {
          return caches.match(event.request).then((cached) => cached || caches.match('/'));
        })
    );
    return;
  }

  // 3. Static scripts & styles - Network-first with cache fallback
  event.respondWith(
    fetch(event.request)
      .then((networkResponse) => {
        if (networkResponse && networkResponse.status === 200) {
          const clone = networkResponse.clone();
          caches.open(CACHE_NAME).then((cache) => cache.put(event.request, clone));
        }
        return networkResponse;
      })
      .catch(() => caches.match(event.request))
  );
});
