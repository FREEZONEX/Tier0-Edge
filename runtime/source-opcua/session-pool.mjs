import { createHash } from 'node:crypto';

const unavailable = () => Object.assign(new Error('SourceStream.protocolFailed'), { code: 'SourceStream.protocolFailed' });

function canonical(value) {
  if (Array.isArray(value)) return value.map(canonical);
  if (value && typeof value === 'object') return Object.fromEntries(Object.keys(value).sort().map((key) => [key, canonical(value[key])]));
  return value;
}

function keyOf(body) {
  const { name, description, ...definition } = body.definition;
  // A Stream's monitored Node subset and sampling interval are lease scope,
  // not Source connection identity. Authorization remains on each operation.
  if (definition.opcua) {
    const { nodeIds, samplingIntervalMs, ...connection } = definition.opcua;
    definition.opcua = connection;
  }
  const revision = body.configRevision ?? 0;
  if (!Number.isSafeInteger(revision) || revision < 0) throw unavailable();
  // Credentials stay inside the trusted protocol process. Never log this key.
  return createHash('sha256').update(JSON.stringify(canonical({ revision, definition, credential: body.credential ?? {} }))).digest('hex');
}

function awaitSignal(promise, signal) {
  if (signal.aborted) return Promise.reject(unavailable());
  return new Promise((resolve, reject) => {
    const abort = () => reject(unavailable());
    signal.addEventListener('abort', abort, { once: true });
    promise.then(resolve, reject).finally(() => signal.removeEventListener('abort', abort));
  });
}

/** Source owns the session; individual script requests borrow bounded leases.
 * Client subscriptions hold leases but do not block serialized short operations.
 * Closing an old generation never evicts or disconnects a newer session.
 */
export class SourceSessionPool {
  constructor({ connect, idleMs = 60000, limit = 128 }) {
    this.connect = connect;
    this.idleMs = idleMs;
    this.limit = limit;
    this.entries = new Map();
    this.closed = false;
    this.probeId = 0;
    this.generation = 0;
  }

  invalidate(entry) {
    if (entry.closed || entry.invalidated) return;
    entry.invalidated = true;
    clearTimeout(entry.timer);
    if (this.entries.get(entry.owner) === entry) this.entries.delete(entry.owner);
    // A watch otherwise waits forever for the lost session's next data event,
    // preventing its Go owner from reconnecting. Cancel only watch leases: an
    // ordinary operation may already have a side effect and must finish once.
    for (const controller of entry.watches) controller.abort();
    // A lost cached connection must not be handed to the next operation. Let
    // existing leases finish before cleanup; never re-dispatch their effects.
    if (entry.users === 0) void this.retire(entry);
  }

  async retire(entry) {
    if (!entry.closed) {
      entry.closed = true;
      entry.controller.abort();
      clearTimeout(entry.timer);
      if (this.entries.get(entry.owner) === entry) this.entries.delete(entry.owner);
    }
    entry.closing ||= entry.ready.then((connection) => connection.close()).catch(() => {});
    await entry.closing;
  }

  async releaseSource(sourceId) {
    await Promise.all([...this.entries.values()].filter((entry) => entry.sourceId === sourceId).map((entry) => this.retire(entry)));
  }

  async close() {
    this.closed = true;
    await Promise.all([...this.entries.values()].map((entry) => this.retire(entry)));
  }

  async run(body, signal, action, { watch = false } = {}) {
    if (this.closed || signal.aborted) throw unavailable();
    const owner = body.sourceId > 0 ? `${body.sourceId}:client` : `0:client:probe-${++this.probeId}`;
    const key = keyOf(body);
    let entry = this.entries.get(owner);
    while (entry && (entry.key !== key || entry.closed)) {
      await this.retire(entry);
      entry = this.entries.get(owner);
    }
    if (!entry) {
      if (this.entries.size >= this.limit) throw unavailable();
      entry = { owner, key, sourceId: body.sourceId, generation: ++this.generation, controller: new AbortController(), watches: new Set(), users: 0, tail: Promise.resolve(), closed: false, invalidated: false };
      // A single-flight promise is installed before asynchronous connection work.
      entry.ready = Promise.resolve().then(() => this.connect(body, entry.controller.signal, {
        generation: entry.generation, invalidate: () => this.invalidate(entry),
      }));
      this.entries.set(owner, entry);
    }
    entry.users++;
    clearTimeout(entry.timer);
    const watchController = watch ? new AbortController() : undefined;
    if (watchController) entry.watches.add(watchController);
    const combined = AbortSignal.any([signal, entry.controller.signal, ...(watchController ? [watchController.signal] : [])]);
    const abort = () => { if (!watch) void this.retire(entry); };
    signal.addEventListener('abort', abort, { once: true });
    let unlock;
    try {
      const connection = await awaitSignal(entry.ready, combined);
      if (!watch) {
        const previous = entry.tail;
        const current = new Promise((resolve) => { unlock = resolve; });
        // A cancelled waiter still stays behind its predecessor; releasing its
        // barrier early must not let later requests overlap the active operation.
        entry.tail = previous.catch(() => {}).then(() => current);
        await awaitSignal(previous, combined);
      }
      if (combined.aborted || entry.closed || entry.invalidated) throw unavailable();
      return await action(connection.session, combined);
    } catch (error) {
      if (!watch || !signal.aborted) this.invalidate(entry);
      throw error;
    } finally {
      unlock?.();
      if (watchController) entry.watches.delete(watchController);
      signal.removeEventListener('abort', abort);
      entry.users--;
      if (entry.invalidated) {
        if (entry.users === 0) await this.retire(entry);
      } else if ((watch || body.sourceId === 0 || combined.aborted) && entry.users === 0) await this.retire(entry);
      else if (!entry.closed && entry.users === 0) {
        entry.timer = setTimeout(() => { void this.retire(entry); }, this.idleMs);
        entry.timer.unref();
      }
    }
  }
}
