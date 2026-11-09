// Caller supplies UTC epoch boundaries calculated from the declared shift timezone.
module.exports = (api) => {
  const production = api.uns.use({ collectionId: '00000000-0000-4000-8000-000000000031', access: 'read' });
  const energy = api.uns.use({ collectionId: '00000000-0000-4000-8000-000000000032', access: 'read' });
  const summaries = api.uns.use({ collectionId: '00000000-0000-4000-8000-000000000033', access: 'read-write' });
  async function pages(handle, options) {
    const rows = []; let cursor;
    for (let page = 0; page < 8; page++) {
      const batch = await handle.query({ ...options, limit: 50, ...(cursor ? { cursor } : {}) });
      rows.push(...batch.items);
      if (!batch.hasMore) return rows;
      if (!batch.nextCursor || batch.nextCursor === cursor) throw new Error('Invalid continuation');
      cursor = batch.nextCursor;
    }
    throw new Error('Shift exceeds page budget; use smaller bounded inputs');
  }
  api.stream.onRequest({ id: 'summarize' }, async (context) => {
    const p = context.input;
    if (!(p.from < p.to && p.shiftId && p.timezone && p.maxGapMs > 0)) throw new Error('Explicit shift boundaries/timezone required');
    const candidates = await pages(production, { kind: 'record', filters: [{ field: 'data.workcenter', op: 'eq', value: p.workcenter }, { field: 'data.time', op: 'gte', value: p.from }, { field: 'data.time', op: 'lt', value: p.to }] });
    const reportIds = [...new Set(candidates.map(({ data }) => {
      if (typeof data.reportId !== 'string' || !data.reportId) throw new Error('Stable report identity required');
      return data.reportId;
    }))];
    if (reportIds.length > 100) throw new Error('Report identity budget exceeded; reduce shift input scope');
    // A correction may move a report outside this time window or workcenter.
    // Resolve all versions of candidate identities before applying the scope.
    const reports = reportIds.length ? await pages(production, { kind: 'record', filters: [{ field: 'data.reportId', op: 'in', value: reportIds }] }) : [];
    const seen = new Map();
    for (const row of reports) {
      const key = row.data.reportId;
      if (!key) throw new Error('Stable report identity required');
      // Source event snapshots have independent UNS revisions. Order them by
      // the native report revision, never by an unrelated record's edit count.
      const nativeVersion = Object.prototype.hasOwnProperty.call(row.data, 'revision');
      const version = nativeVersion ? row.data.revision : row.revision;
      if (!Number.isSafeInteger(version) || version < 1) throw new Error('Invalid report revision');
      const previous = seen.get(key);
      if (previous && previous.nativeVersion !== nativeVersion) throw new Error('Mixed report revision spaces');
      const metrics = (value) => JSON.stringify([value.quantity, value.accepted, value.time, value.workcenter]);
      const versions = previous?.versions || new Map();
      const signature = metrics(row.data);
      if (!nativeVersion && (typeof row.id !== 'string' || !row.id)) throw new Error('Stable record identity required');
      const versionKey = nativeVersion ? version : JSON.stringify([row.id, version]);
      if (versions.has(versionKey) && versions.get(versionKey) !== signature) throw new Error('Conflicting report revision');
      versions.set(versionKey, signature);
      const recordVersions = previous?.recordVersions || new Map();
      if (!nativeVersion && (!recordVersions.has(row.id) || version > recordVersions.get(row.id).version)) recordVersions.set(row.id, { version, signature });
      if (!previous || version > previous.version) seen.set(key, { row, version, nativeVersion, versions, recordVersions });
    }
    let quantity = 0; let accepted = 0; let reportCount = 0;
    for (const group of seen.values()) {
      // Edit counts only order revisions of the same physical Record. Copies
      // must agree on their current metrics unless the source supplies a version.
      if (!group.nativeVersion && new Set([...group.recordVersions.values()].map(item => item.signature)).size > 1) throw new Error('Independent report records require source revisions');
      const { data } = group.row;
      if (data.workcenter !== p.workcenter || !(data.time >= p.from && data.time < p.to)) continue;
      if (!Number.isFinite(data.quantity) || !Number.isFinite(data.accepted)) throw new Error('Invalid production quantity');
      quantity += data.quantity; accepted += data.accepted; reportCount++;
    }
    const points = await pages(energy, { kind: 'sample', recordIds: [p.meterId], timeRange: { from: p.from, to: p.to } });
    let consumption = 0; let resets = 0; let gaps = 0; let previous = null; const ids = new Set();
    for (const point of points) {
      if (ids.has(point.id)) continue; ids.add(point.id);
      if (point.quality !== 'good' || !Number.isFinite(point.data.value)) { previous = null; gaps++; continue; }
      if (previous) {
        if (point.time - previous.time > p.maxGapMs) gaps++;
        else if (point.data.value < previous.data.value) resets++;
        else consumption += point.data.value - previous.data.value;
      }
      previous = point;
    }
    const metrics = { quantity, accepted, consumption, resets, gaps, reportCount, from: p.from, to: p.to, timezone: p.timezone };
    const id = 'shift:' + p.workcenter + ':' + p.shiftId;
    const existing = await summaries.query({ kind: 'record', recordIds: [id], limit: 1 });
    const old = existing.items[0]?.data || {};
    const oldDerived = old.derived;
    // JSON object key ordering is not stable after database decoding.
    const changed = !oldDerived || Object.keys(metrics).some((field) => oldDerived.metrics?.[field] !== metrics[field]);
    if (changed) {
      const version = (oldDerived?.version || 0) + 1;
      const derived = { metrics, version, reason: oldDerived ? 'late-or-corrected-input' : 'initial', simulated: p.simulated === true };
      // Immutable revision records preserve the original confirmed revision; manual fields stay untouched.
      await summaries.write({ recordId: id + ':v' + version, expectedRevision: 0, record: { ...derived, confirmed: false } });
      await summaries.write({ recordId: id, expectedRevision: existing.items[0]?.revision ?? 0, record: { ...old, derived } });
    }
    await api.stream.checkpoint.set({ shiftId: p.shiftId, completedTo: p.to });
    return { metrics, changed, version: (oldDerived?.version || 0) + (changed ? 1 : 0), simulated: p.simulated === true };
  });
};
