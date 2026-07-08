// beacon service worker — makes the app launch instantly and survive going
// offline (falling back to the last-cached shell + localStorage snapshot).
// Bump CACHE to force clients onto a new shell after a deploy.
const CACHE = 'beacon-v1';
const SHELL = [
  '/', '/style.css', '/app.js', '/manifest.webmanifest',
  '/icon.svg', '/apple-touch-icon.png', '/icon-192.png', '/icon-512.png',
  '/fonts/archivo-latin.woff2', '/fonts/jetbrains-mono-latin.woff2',
];

self.addEventListener('install', (e) => {
  e.waitUntil(caches.open(CACHE).then((c) => c.addAll(SHELL)).then(() => self.skipWaiting()));
});

self.addEventListener('activate', (e) => {
  e.waitUntil(
    caches.keys()
      .then((keys) => Promise.all(keys.filter((k) => k !== CACHE).map((k) => caches.delete(k))))
      .then(() => self.clients.claim())
  );
});

self.addEventListener('fetch', (e) => {
  const req = e.request;
  if (req.method !== 'GET') return;
  const url = new URL(req.url);
  if (url.origin !== self.location.origin) return;
  // Live data must never be served stale from the cache.
  if (url.pathname.startsWith('/api/') || url.pathname === '/healthz') return;

  // The document: network-first so new versions land, cache as offline fallback.
  if (req.mode === 'navigate') {
    e.respondWith(
      fetch(req)
        .then((r) => { caches.open(CACHE).then((c) => c.put('/', r.clone())); return r; })
        .catch(() => caches.match('/'))
    );
    return;
  }

  // Static assets: cache-first, populating the cache on first miss.
  e.respondWith(
    caches.match(req).then((hit) => hit || fetch(req).then((r) => {
      if (r.ok) { const copy = r.clone(); caches.open(CACHE).then((c) => c.put(req, copy)); }
      return r;
    }))
  );
});
