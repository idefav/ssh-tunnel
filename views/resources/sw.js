// SSH-Tunnel Service Worker
const CACHE_NAME = 'ssh-tunnel-v1';

// Install event
self.addEventListener('install', function(event) {
  self.skipWaiting();
});

// Activate event
self.addEventListener('activate', function(event) {
  event.waitUntil(self.clients.claim());
});

// Fetch event - network first, this is a management tool so always prefer fresh data
self.addEventListener('fetch', function(event) {
  event.respondWith(fetch(event.request));
});
