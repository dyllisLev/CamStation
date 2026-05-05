const CACHE_NAME = 'camstation-mobile-v1'

self.addEventListener('install', () => {
  self.skipWaiting()
})

self.addEventListener('activate', (event) => {
  event.waitUntil(
    caches.keys().then(keys =>
      Promise.all(keys.filter(k => k !== CACHE_NAME).map(k => caches.delete(k)))
    )
  )
  self.clients.claim()
})

self.addEventListener('fetch', (event) => {
  const url = new URL(event.request.url)

  if (url.pathname.startsWith('/api/') || url.pathname.startsWith('/go2rtc/')) {
    return
  }

  event.respondWith(
    fetch(event.request)
      .then(response => {
        if (
          event.request.method === 'GET' &&
          (url.pathname === '/mobile' ||
           url.pathname === '/mobile.webmanifest' ||
           url.pathname === '/icon-192.png' ||
           url.pathname === '/')
        ) {
          const clone = response.clone()
          caches.open(CACHE_NAME).then(cache => cache.put(event.request, clone))
        }
        return response
      })
      .catch(async () => {
        const cached = await caches.match(event.request)
        if (cached) return cached
        if (event.request.mode === 'navigate') {
          return new Response(
            '<html><body style="background:#09090f;color:#888;display:flex;align-items:center;justify-content:center;height:100vh;margin:0;font-family:sans-serif"><p>서버에 연결할 수 없습니다</p></body></html>',
            { headers: { 'Content-Type': 'text/html' } }
          )
        }
        return new Response('', { status: 503 })
      })
  )
})
