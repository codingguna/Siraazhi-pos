/* ==========================================================================
   Siraazhi Pro POS — Service Worker (PWA Offline Cache)
   WHAT IT IS:
     A background worker script running inside the browser on client devices.
   HOW IT WORKS:
     Caches static UI assets (HTML, CSS, JS) so mobile staff on tablets or phones
     experience instant startup even if local Wi-Fi has momentary hiccups.
   FLOW:
     1. install: Pre-caches critical frontend assets.
     2. activate: Cleans up obsolete caches from previous builds.
     3. fetch: Serves static assets from cache first, falling back to network;
        API calls (/api/*) always bypass cache to guarantee real-time stock & sales.
   ========================================================================== */

const CACHE_NAME = 'siraazhi-pos-v3';
const STATIC_ASSETS = [
  '/',
  '/index.html',
  '/css/style.css',
  '/css/print.css',
  '/js/app.js',
  '/js/print.js',
  '/js/excel.js',
  '/js/pdf.js',
  '/js/xlsx.full.min.js',
  '/manifest.json'
];

self.addEventListener('install', (event) => {
  event.waitUntil(
    caches.open(CACHE_NAME).then((cache) => {
      return cache.addAll(STATIC_ASSETS);
    })
  );
  self.skipWaiting();
});

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches.keys().then((keys) => {
      return Promise.all(
        keys.filter((key) => key !== CACHE_NAME).map((key) => caches.delete(key))
      );
    })
  );
  self.clients.claim();
});

self.addEventListener('fetch', (event) => {
  const url = new URL(event.request.url);

  // Network-first for all live REST APIs to guarantee accurate inventory & billing
  if (url.pathname.startsWith('/api/')) {
    event.respondWith(fetch(event.request));
    return;
  }

  // Cache-first for static UI assets
  event.respondWith(
    caches.match(event.request).then((cachedResponse) => {
      return cachedResponse || fetch(event.request);
    })
  );
});
