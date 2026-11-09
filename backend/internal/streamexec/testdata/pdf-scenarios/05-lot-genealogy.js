// Traversal uses actual fromLot/toLot edges only, never a material-number inference.
module.exports = (api) => {
  const edges = api.uns.use({ collectionId: '00000000-0000-4000-8000-000000000041', access: 'read' });
  const output = api.uns.use({ collectionId: '00000000-0000-4000-8000-000000000042', access: 'read-write' });
  api.stream.onRequest({ id: 'trace' }, async (context) => {
    const p = context.input;
    if (!(p.rootLot && ['forward', 'backward'].includes(p.direction) && Number.isInteger(p.maxNodes) && p.maxNodes > 0 && p.maxNodes <= 100 && Number.isInteger(p.maxPages) && p.maxPages > 0 && p.maxPages <= 8)) throw new Error('Invalid bounded traversal parameters');
    const key = p.rootLot + ':' + p.direction;
    const saved = await api.stream.checkpoint.get();
    if (saved && saved.key !== key) throw new Error('Checkpoint belongs to a different traversal');
    const state = saved || { key, queue: [p.rootLot], seen: [], links: [], missing: [], cycles: [], cursor: null };
    const field = p.direction === 'forward' ? 'data.fromLot' : 'data.toLot';
    const target = p.direction === 'forward' ? 'toLot' : 'fromLot';
    function reaches(start, wanted, excludedEdge) {
      const pending = [start]; const visited = new Set();
      while (pending.length) {
        const current = pending.pop();
        if (current === wanted) return true;
        if (visited.has(current)) continue;
        visited.add(current);
        for (const edge of state.links) {
          if (edge.id === excludedEdge) continue;
          const from = p.direction === 'forward' ? edge.fromLot : edge.toLot;
          const to = p.direction === 'forward' ? edge.toLot : edge.fromLot;
          if (from === current) pending.push(to);
        }
      }
      return false;
    }
    let pages = 0;
    while (state.queue.length && pages < p.maxPages) {
      const lot = state.queue[0];
      if (state.seen.length >= p.maxNodes && !state.seen.includes(lot)) throw new Error('Traversal node budget exceeded');
      const batch = await edges.query({ kind: 'record', filters: [{ field, op: 'eq', value: lot }], limit: 50, ...(state.cursor ? { cursor: state.cursor } : {}) });
      pages++;
      if (!batch.items.length && !state.cursor) state.missing.push(lot);
      for (const row of batch.items) {
        if (!row.data.fromLot || !row.data.toLot || !row.data.relationship) { state.missing.push('edge:' + row.id); continue; }
        if (!state.links.some((link) => link.id === row.id)) state.links.push({ id: row.id, ...row.data });
        const next = row.data[target];
        if (reaches(next, lot, row.id)) state.cycles.push({ edgeId: row.id, lot: next });
        if (!state.seen.includes(next) && !state.queue.includes(next)) state.queue.push(next);
      }
      if (batch.hasMore) {
        if (!batch.nextCursor || batch.nextCursor === state.cursor) throw new Error('Invalid continuation');
        state.cursor = batch.nextCursor;
      } else {
        state.cursor = null; state.queue.shift(); if (!state.seen.includes(lot)) state.seen.push(lot);
      }
    }
    const complete = state.queue.length === 0;
    const result = { rootLot: p.rootLot, direction: p.direction, complete, links: state.links, missing: state.missing, cycles: state.cycles, simulated: p.simulated === true };
    const id = 'trace:' + key;
    const existing = await output.query({ kind: 'record', recordIds: [id], limit: 1 });
    await output.write({ recordId: id, expectedRevision: existing.items[0]?.revision ?? 0, record: { ...(existing.items[0]?.data || {}), derived: result } });
    await api.stream.checkpoint.set(complete ? null : state);
    return result;
  });
};
