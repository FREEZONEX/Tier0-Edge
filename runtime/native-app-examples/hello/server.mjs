import http from 'node:http';

const base = process.env.APP_BASE_PATH || '';
const server = http.createServer((request, response) => {
  const url = new URL(request.url, 'http://localhost');
  if (url.pathname === '/healthz') return response.end('ok');
  if (url.pathname === `${base}/api/info`) {
    response.setHeader('Content-Type', 'application/json');
    return response.end(JSON.stringify({ appId: process.env.APP_ID, basePath: base, node: process.version }));
  }
  if (url.pathname === `${base}/events`) {
    response.writeHead(200, { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-cache' });
    response.write(`data: ${JSON.stringify({ now: new Date().toISOString() })}\n\n`);
    const timer = setInterval(() => response.write(`data: ${JSON.stringify({ now: new Date().toISOString() })}\n\n`), 1000);
    request.on('close', () => clearInterval(timer));
    return;
  }
  if (url.pathname !== `${base}/`) {
    response.statusCode = 404;
    return response.end('Not found');
  }
  response.setHeader('Content-Type', 'text/html; charset=utf-8');
  response.end(`<!doctype html><html lang="en"><meta charset="utf-8"><title>Native Hello</title>
    <style>body{font:16px system-ui;max-width:720px;margin:48px auto;padding:16px}pre{white-space:pre-wrap}</style>
    <h1>Native Hello</h1><p>One platform port. An independent Node.js process.</p>
    <h2>Runtime</h2><pre id="info"></pre><h2>Live event stream</h2><pre id="clock">Connecting…</pre>
    <script>fetch('./api/info').then(r=>r.json()).then(v=>document.querySelector('#info').textContent=JSON.stringify(v,null,2));
    const events=new EventSource('./events');events.onmessage=e=>document.querySelector('#clock').textContent=e.data;
    window.addEventListener('pagehide',()=>events.close());</script></html>`);
});
server.listen(Number(process.env.PORT), process.env.HOST || '127.0.0.1');
