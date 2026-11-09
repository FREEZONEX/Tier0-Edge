// Threshold/duration/gap values are explicit test parameters, NOT plant standards.
module.exports = (api) => {
  const observations = api.uns.use({ collectionId: '00000000-0000-4000-8000-000000000011', access: 'read' });
  const state = api.uns.use({ collectionId: '00000000-0000-4000-8000-000000000012', access: 'read-write' });
  api.stream.onRequest({ id: 'evaluate' }, async (context) => {
    const p = context.input;
    if (!(p.from < p.to && p.durationMs > 0 && p.maxGapMs > 0 && Number.isFinite(p.threshold))) throw new Error('Invalid anomaly parameters');
    const saved = await state.query({ kind: 'record', recordIds: ['state:' + p.deviceId], limit: 1 });
    const old = saved.items[0]?.data || {};
    const previous = old.derived || { lastTime: null, startedAt: null, activeId: null };
    const current = { ...previous }; const events = []; let cursor; let exhausted = false; let sampleCount = 0;
    const diagnostics = { badQuality: 0, missingGap: 0, stopped: 0, unknownRunning: 0, alreadyProcessed: 0 };
    for (let page = 0; page < 8; page++) {
      const batch = await observations.query({ kind: 'sample', recordIds: [p.deviceId], timeRange: { from: p.from, to: p.to }, limit: 50, ...(cursor ? { cursor } : {}) });
      for (const sample of batch.items) {
        sampleCount++;
        if (current.lastTime !== null && sample.time <= current.lastTime) { diagnostics.alreadyProcessed++; continue; }
        const gap = current.lastTime !== null && sample.time - current.lastTime > p.maxGapMs;
        const good = sample.quality === 'good' && Number.isFinite(sample.data.value);
        const stopped = sample.data.running === false;
        const unknownRunning = typeof sample.data.running !== 'boolean';
        const unknown = gap || !good || unknownRunning;
        if (!good) diagnostics.badQuality++;
        if (gap) diagnostics.missingGap++;
        if (stopped) diagnostics.stopped++;
        if (unknownRunning) diagnostics.unknownRunning++;
        if (unknown || stopped) {
          if (current.activeId) events.push({ id: current.activeId, status: 'interrupted', reason: stopped ? 'stopped' : gap ? 'missing-samples' : !good ? 'bad-quality' : 'unknown-running-state', start: current.startedAt, end: sample.time });
          current.startedAt = null; current.activeId = null;
        } else if (sample.data.value > p.threshold) {
          if (current.startedAt === null) current.startedAt = sample.time;
          if (!current.activeId && sample.time - current.startedAt >= p.durationMs) {
            current.activeId = 'anomaly:' + p.deviceId + ':' + current.startedAt;
            events.push({ id: current.activeId, status: 'active', start: current.startedAt, end: null });
          }
        } else {
          if (current.activeId) events.push({ id: current.activeId, status: 'recovered', start: current.startedAt, end: sample.time });
          current.startedAt = null; current.activeId = null;
        }
        current.lastTime = sample.time;
      }
      if (!batch.hasMore) { exhausted = true; break; }
      if (!batch.nextCursor || batch.nextCursor === cursor) throw new Error('Invalid continuation');
      cursor = batch.nextCursor;
    }
    if (!exhausted) throw new Error('Window exceeds page budget');
    const trailingGap = current.lastTime !== null && p.to - current.lastTime > p.maxGapMs;
    if ((sampleCount === 0 && p.to - p.from > p.maxGapMs) || trailingGap) {
      diagnostics.missingGap++;
      if (current.activeId) events.push({ id: current.activeId, status: 'interrupted', reason: 'missing-samples', start: current.startedAt, end: current.lastTime + p.maxGapMs });
      current.startedAt = null; current.activeId = null;
    }
    // Coalesce the status of one anomaly ID; do not submit duplicate writes in one transaction.
    const latest = new Map();
    for (const event of events) latest.set(event.id, event);
    for (const event of latest.values()) {
      const existing = await state.query({ kind: 'record', recordIds: [event.id], limit: 1 });
      await state.write({ recordId: event.id, expectedRevision: existing.items[0]?.revision ?? 0, record: { ...(existing.items[0]?.data || {}), derived: { ...event, simulated: p.simulated === true } } });
    }
    await state.write({ recordId: 'state:' + p.deviceId, expectedRevision: saved.items[0]?.revision ?? 0, record: { ...old, derived: current } });
    await api.stream.checkpoint.set({ completedTo: p.to });
    // Older newly arrived observations require an explicit recompute from a prior state;
    // alreadyProcessed is never reported as a freshly evaluated normal observation.
    return { events, state: current, diagnostics, missingWindow: sampleCount === 0, simulated: p.simulated === true };
  });
};
