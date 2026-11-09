// Native validation broker. MQTT 3.1.1; sessions/retained messages are in memory.
// Product authentication and topic ACLs remain owned by Backend.
import { Aedes } from 'aedes';
import net from 'node:net';
import http from 'node:http';
import { timingSafeEqual } from 'node:crypto';
import { WebSocketServer, createWebSocketStream } from 'ws';

const env = process.env;
for (const key of ['MQTT_BACKEND_PASSWORD', 'EMQX_API_SECRET', 'NATIVE_BACKEND_URL']) {
  if (!env[key]) throw new Error(`Missing ${key}`);
}
const same = (a, b) => {
  const x = Buffer.from(a || ''); const y = Buffer.from(b || '');
  return x.length === y.length && timingSafeEqual(x, y);
};
async function decision(path, data) {
  try {
    const response = await fetch(`${env.NATIVE_BACKEND_URL}/api/core/mqtt/${path}`, {
      method: 'POST', headers: { 'content-type': 'application/json' },
      body: JSON.stringify(data), signal: AbortSignal.timeout(4000),
    });
    return response.ok && (await response.json()).result === 'allow';
  } catch { return false; }
}
const broker = await Aedes.createBroker();
broker.authenticate = (client, username, password, callback) => {
  client.nativeUsername = username || '';
  client.nativeInternal = username === 'backend' && same(password, env.MQTT_BACKEND_PASSWORD);
  if (client.nativeInternal) return callback(null, true);
  decision('auth', { username: username || '', password: password?.toString() || '', clientid: client.id, peerhost: '127.0.0.1', protoName: 'MQTT' })
    .then(allow => callback(null, allow));
};
const acl = (client, topic, action) => client?.nativeInternal ? Promise.resolve(true) : decision('acl', {
  username: client?.nativeUsername || '', clientid: client?.id || '', topic, action,
});
broker.authorizePublish = (client, packet, callback) => {
  acl(client, packet.topic, 'publish').then(allow => callback(allow ? null : new Error('publish denied')));
};
broker.authorizeSubscribe = (client, sub, callback) => {
  acl(client, sub.topic, 'subscribe').then(allow => callback(null, allow ? sub : null));
};
// Avoid logging usernames, credentials or business payloads.
broker.on('clientError', () => {});
broker.on('connectionError', () => {});
const tcp = net.createServer(broker.handle);
tcp.listen(Number(env.NATIVE_MQTT_PORT), '127.0.0.1');
const admin = http.createServer((req, res) => {
  if (req.url === '/readyz' && req.method === 'GET') { res.end('ready'); return; }
  const expected = `Basic ${Buffer.from(`${env.EMQX_API_KEY}:${env.EMQX_API_SECRET}`).toString('base64')}`;
  if (!same(req.headers.authorization, expected)) { res.writeHead(401).end(); return; }
  if (req.method === 'DELETE' && req.url.startsWith('/api/v5/clients/')) {
    let id;
    try { id = decodeURIComponent(req.url.slice('/api/v5/clients/'.length)); } catch { res.writeHead(400).end(); return; }
    const client = broker.clients[id];
    if (!client) { res.writeHead(404).end(); return; }
    client.close(() => res.writeHead(204).end()); return;
  }
  res.writeHead(404).end();
});
const ws = new WebSocketServer({ server: admin, path: '/mqtt', maxPayload: 1024 * 1024 });
ws.on('connection', socket => broker.handle(createWebSocketStream(socket)));
admin.listen(Number(env.NATIVE_MQTT_WS_PORT), '127.0.0.1');
const stop = () => { tcp.close(); ws.close(); admin.close(); broker.close(() => process.exit(0)); };
process.on('SIGINT', stop); process.on('SIGTERM', stop);
