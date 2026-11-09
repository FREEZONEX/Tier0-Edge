import type { ConnectBasis, ConnectStreamCard } from './connect-model';

export const SOURCE_CARD_HEIGHT = 124;
export const SOURCE_STREAM_ROW_HEIGHT = 36;
export const SOURCE_STREAM_LIST_HEIGHT = 180;
export const SOURCE_STREAM_LIST_PADDING = 8;

export function indexSourceStreams(streams: ConnectStreamCard[], basis: ConnectBasis) {
  const index = new Map<string, ConnectStreamCard[]>();
  const seen = new Set<string>();
  for (const stream of streams) {
    const id = String(stream.id);
    if (seen.has(id)) continue;
    seen.add(id);
    for (const sourceId of new Set(stream[basis]?.sources.map((reference) => reference.id))) {
      const list = index.get(sourceId) || [];
      list.push(stream);
      index.set(sourceId, list);
    }
  }
  return index;
}

export function sourceCardLayout(count: number, expanded: boolean, readable: boolean, canCreate: boolean) {
  const bodyHeight =
    expanded && readable
      ? SOURCE_STREAM_LIST_PADDING +
        Math.min(SOURCE_STREAM_LIST_HEIGHT, (Math.max(1, count) + (canCreate ? 1 : 0)) * SOURCE_STREAM_ROW_HEIGHT)
      : 0;
  return { height: SOURCE_CARD_HEIGHT + bodyHeight, bodyHeight };
}

export function sourceGroupLayoutKey(sourceIds: readonly number[], heights: readonly number[], collapsed: boolean) {
  // Equal dimensions do not imply equal handles after filtering or reordering Sources.
  return JSON.stringify([collapsed, sourceIds, heights]);
}

export function sourceGroupHeight(heights: number[], collapsed: boolean) {
  const frame = 50;
  const spacing = 12;
  return collapsed
    ? frame
    : frame +
        spacing * 2 +
        heights.reduce((sum, height) => sum + height, 0) +
        Math.max(0, heights.length - 1) * spacing;
}
