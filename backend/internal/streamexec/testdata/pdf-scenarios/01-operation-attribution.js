// Bind these collection UUIDs to your isolated acceptance Namespace before deployment.
// Event times and all window boundaries are milliseconds; simulated input must be marked.
module.exports = (api) => {
  const observations = api.uns.use({ collectionId: '00000000-0000-4000-8000-000000000001', access: 'read' });
  const operations = api.uns.use({ collectionId: '00000000-0000-4000-8000-000000000002', access: 'read' });
  const output = api.uns.use({ collectionId: '00000000-0000-4000-8000-000000000003', access: 'read-write' });
  async function pages(handle, options) {
    const rows = []; let cursor;
    for (let page = 0; page < 8; page++) {
      const result = await handle.query({ ...options, limit: 50, ...(cursor ? { cursor } : {}) });
      rows.push(...result.items);
      if (!result.hasMore) return rows;
      if (!result.nextCursor || result.nextCursor === cursor) throw new Error('Invalid continuation');
      cursor = result.nextCursor;
    }
    throw new Error('Window exceeds page budget; reduce the requested time slice');
  }
  api.stream.onRequest({ id: 'attribute' }, async (context) => {
    const p = context.input;
    if (!(Number.isSafeInteger(p.from) && Number.isSafeInteger(p.to) && p.from < p.to)) throw new Error('Invalid window');
    const samples = await pages(observations, { kind: 'sample', timeRange: { from: p.from, to: p.to }, recordIds: [p.deviceId] });
    const ops = await pages(operations, { kind: 'record', filters: [{ field: 'data.deviceId', op: 'eq', value: p.deviceId }] });
    const results = [];
    // A late arrival is attributed by source event time, never by receivedAt.
    for (const sample of samples) {
      const time = sample.time;
      const trusted = Number.isSafeInteger(time) && sample.data.timeTrusted !== false;
      const matches = trusted ? ops.filter(({ data }) => Number.isSafeInteger(data.start) && Number.isSafeInteger(data.end) && data.start <= time && time < data.end) : [];
      const result = {
        sampleId: sample.id, deviceId: p.deviceId, time, receivedAt: sample.receivedAt,
        status: !trusted ? 'pending-review' : matches.length === 1 ? 'attributed' : 'pending-review',
        reason: !trusted ? 'untrusted-time' : matches.length === 0 ? 'missing-operation' : matches.length > 1 ? 'overlapping-operations' : null,
        operationId: matches.length === 1 ? matches[0].id : null,
        candidateIds: matches.map((row) => row.id), quality: sample.quality,
        sourceId: sample.sourceId, streamId: sample.streamId, simulated: p.simulated === true,
      };
      const id = 'attribution:' + sample.id;
      const previous = await output.query({ kind: 'record', recordIds: [id], limit: 1 });
      await output.write({ recordId: id, expectedRevision: previous.items[0]?.revision ?? 0, record: { ...(previous.items[0]?.data || {}), derived: result } });
      results.push(result);
    }
    await api.stream.checkpoint.set({ completedFrom: p.from, completedTo: p.to });
    return { results, simulated: p.simulated === true };
  });
};
