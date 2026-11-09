import http from 'node:http';
import { readFile } from 'node:fs/promises';
import { randomUUID } from 'node:crypto';

const config = JSON.parse(await readFile(new URL('./mode.json', import.meta.url)));
const page = await readFile(new URL('./index.html', import.meta.url));
const base = process.env.APP_BASE_PATH || '';
const apiHost = process.env.TIER0_API_HOST;
const apiKey = process.env.TIER0_API_KEY;
if (!apiKey || !apiHost) throw new Error('App-scoped OpenAPI credentials are required');

async function api(path, body) {
  const response = await fetch(`${apiHost}/openapi/v2/uns/${path}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'X-API-Key': apiKey },
    body: JSON.stringify(body),
    signal: AbortSignal.timeout(20000),
  });
  const result = await response.json();
  if (!response.ok || ![0, 200].includes(result.code)) {
    const error = new Error(result.msg || `OpenAPI request failed (${response.status})`);
    error.status = response.ok ? 400 : response.status;
    throw error;
  }
  return result.data;
}
async function readBody(request) {
  let size = 0;
  const chunks = [];
  for await (const chunk of request) {
    size += chunk.length;
    if (size > 65536) throw new Error('Request too large');
    chunks.push(chunk);
  }
  return JSON.parse(Buffer.concat(chunks).toString('utf8') || '{}');
}
const server = http.createServer(async (request, response) => {
  const path = new URL(request.url, 'http://localhost').pathname;
  const send = (status, value) => {
    response.writeHead(status, { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' });
    response.end(JSON.stringify(value));
  };
  try {
    if (path === '/healthz') return response.end('ok');
    if (path === `${base}/` && request.method === 'GET') {
      response.setHeader('Content-Type', 'text/html; charset=utf-8');
      return response.end(page);
    }
    if (path === `${base}/api/config` && request.method === 'GET') return send(200, config);
    if (path === `${base}/api/context` && request.method === 'GET') return send(200, await api('apps/context', {}));
    if (path === `${base}/api/metadata` && request.method === 'GET') {
      return send(200, await api('metadata:find', { kind: 'collection', state: 'active', limit: 50 }));
    }
    if (config.mode === 'records' && path === `${base}/api/records` && request.method === 'POST') {
      const body = await readBody(request);
      const context = await api('apps/context', {});
      const grant = context.collections.find((item) => item.collectionId === body.collectionId);
      if (!grant) return send(403, { error: 'Collection is not granted to this App' });
      const prefix = `collections/${encodeURIComponent(grant.collectionId)}/records`;
      if (body.action === 'find') return send(200, await api(`${prefix}:find`, { filter: {}, limit: 30 }));
      if (typeof body.id !== 'string' || !body.id) return send(400, { error: 'Record ID is required' });
      const idempotencyKey = body.idempotencyKey || randomUUID();
      if (body.action === 'insert') {
        return send(200, await api(prefix, { id: body.id, data: body.data, idempotencyKey, returnDocument: 'after' }));
      }
      if (body.action === 'update' || body.action === 'delete') {
        if (!Number.isSafeInteger(body.revision) || body.revision < 1) return send(400, { error: 'Read the current record revision first' });
        const payload = { filter: { _id: body.id }, expectedRevision: body.revision, idempotencyKey };
        if (body.action === 'update') {
          return send(200, await api(`${prefix}:updateOne`, { ...payload, update: { $set: body.data }, returnDocument: 'after' }));
        }
        return send(200, await api(`${prefix}:deleteOne`, { ...payload, returnDocument: 'before' }));
      }
      return send(400, { error: 'Unsupported action' });
    }
    send(404, { error: 'Not found' });
  } catch (error) {
    send(error.status || 400, { error: error.message });
  }
});
server.listen(Number(process.env.PORT), process.env.HOST || '127.0.0.1');
