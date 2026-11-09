// Read-only effect review: this script never invokes a device or writes commands.
module.exports = (api) => {
  const receipts = api.uns.use({ collectionId: '00000000-0000-4000-8000-000000000021', access: 'read' });
  const observations = api.uns.use({ collectionId: '00000000-0000-4000-8000-000000000022', access: 'read' });
  const output = api.uns.use({ collectionId: '00000000-0000-4000-8000-000000000023', access: 'read-write' });
  async function window(p, from, to) {
    const rows = []; let cursor;
    for (let page = 0; page < 8; page++) {
      const batch = await observations.query({ kind: 'sample', recordIds: [p.deviceId], timeRange: { from, to }, limit: 50, ...(cursor ? { cursor } : {}) });
      rows.push(...batch.items.filter((row) => row.quality === 'good' && row.data.running === true && row.data.condition === p.condition && row.data.workOrderId === p.workOrderId && Number.isFinite(row.data.value)));
      if (!batch.hasMore) return rows;
      if (!batch.nextCursor || batch.nextCursor === cursor) throw new Error('Invalid continuation');
      cursor = batch.nextCursor;
    }
    throw new Error('Window exceeds page budget');
  }
  api.stream.onRequest({ id: 'review' }, async (context) => {
    const p = context.input;
    if (!(p.windowMs > 0 && p.commandId && p.deviceId && p.bootId && p.workOrderId && p.condition && Number.isInteger(p.minimumSamples) && p.minimumSamples > 0)) throw new Error('Invalid comparison parameters');
    const rows = await receipts.query({ kind: 'record', recordIds: [p.commandId], limit: 1 });
    const receipt = rows.items[0]?.data;
    let result;
    if (!receipt || receipt.state !== 'executed' || receipt.deviceId !== p.deviceId || receipt.bootId !== p.bootId || !Number.isSafeInteger(receipt.actualAppliedAt)) {
      result = { status: 'insufficient-evidence', reason: 'no-matching-executed-receipt', commandId: p.commandId };
    } else {
      const before = await window(p, receipt.actualAppliedAt - p.windowMs, receipt.actualAppliedAt);
      const after = await window(p, receipt.actualAppliedAt, receipt.actualAppliedAt + p.windowMs);
      const enough = before.length >= p.minimumSamples && after.length >= p.minimumSamples;
      const mean = (list) => list.reduce((total, row) => total + row.data.value, 0) / list.length;
      result = { commandId: p.commandId, actualAppliedAt: receipt.actualAppliedAt, status: enough ? 'compared' : 'insufficient-evidence', beforeCount: before.length, afterCount: after.length, beforeMean: enough ? mean(before) : null, afterMean: enough ? mean(after) : null, delta: enough ? mean(after) - mean(before) : null, qualityConclusion: 'not-determined' };
    }
    result.simulated = p.simulated === true || receipt?.simulated === true;
    const id = 'effect:' + p.commandId;
    const previous = await output.query({ kind: 'record', recordIds: [id], limit: 1 });
    await output.write({ recordId: id, expectedRevision: previous.items[0]?.revision ?? 0, record: { ...(previous.items[0]?.data || {}), derived: result } });
    return result;
  });
};
