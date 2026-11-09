export interface LayoutPoint {
  x: number;
  y: number;
}
export interface LayoutNode {
  id: string;
  radius: number;
  position?: LayoutPoint;
}
export interface LayoutEdge {
  source: string;
  target: string;
}

export function mergeLayoutPositions(
  previous: Record<string, LayoutPoint> | undefined,
  current: Record<string, LayoutPoint>
): Record<string, LayoutPoint> {
  // Meta and Record queries finish separately. A partial render must not erase
  // the coordinates of nodes which have not loaded again yet.
  return { ...previous, ...current };
}

// 固定初始分布，避免刷新随机跳动；纯数学布局不生成业务关系。
export function initialPositions(nodes: LayoutNode[], focus?: string): Record<string, LayoutPoint> {
  const output: Record<string, LayoutPoint> = {};
  nodes.forEach((node, index) => {
    const angle = index * 2.3999632297;
    const radius = 140 + 54 * Math.sqrt(index);
    output[node.id] =
      node.position ||
      (node.id === focus ? { x: 0, y: 0 } : { x: Math.cos(angle) * radius, y: Math.sin(angle) * radius });
  });
  return output;
}

export function layoutStep(
  nodes: LayoutNode[],
  edges: LayoutEdge[],
  positions: Record<string, LayoutPoint>,
  focus?: string
): Record<string, LayoutPoint> {
  const next = Object.fromEntries(Object.entries(positions).map(([id, point]) => [id, { ...point }]));
  const delta = Object.fromEntries(
    nodes.map((node) => [node.id, { x: -positions[node.id].x * 0.004, y: -positions[node.id].y * 0.004 }])
  );
  for (let i = 0; i < nodes.length; i += 1) {
    for (let j = i + 1; j < nodes.length; j += 1) {
      const a = nodes[i];
      const b = nodes[j];
      const dx = positions[a.id].x - positions[b.id].x || 0.1;
      const dy = positions[a.id].y - positions[b.id].y || 0.1;
      const distance = Math.max(Math.hypot(dx, dy), 1);
      const minimum = a.radius + b.radius + 95;
      const force = Math.min(18, 1000 / (distance * distance) + Math.max(0, minimum - distance) * 0.14);
      delta[a.id].x += (dx / distance) * force;
      delta[a.id].y += (dy / distance) * force;
      delta[b.id].x -= (dx / distance) * force;
      delta[b.id].y -= (dy / distance) * force;
    }
  }
  edges.forEach(({ source, target }) => {
    if (!positions[source] || !positions[target]) return;
    const dx = positions[target].x - positions[source].x;
    const dy = positions[target].y - positions[source].y;
    const distance = Math.max(Math.hypot(dx, dy), 1);
    const force = Math.max(-4, Math.min(8, (distance - 240) * 0.014));
    delta[source].x += (dx / distance) * force;
    delta[source].y += (dy / distance) * force;
    delta[target].x -= (dx / distance) * force;
    delta[target].y -= (dy / distance) * force;
  });
  nodes.forEach(({ id }) => {
    if (id === focus) {
      next[id] = { x: 0, y: 0 };
      return;
    }
    next[id].x += Math.max(-20, Math.min(20, delta[id].x));
    next[id].y += Math.max(-20, Math.min(20, delta[id].y));
  });
  return next;
}

export function recordNodeKey(collectionId: string, recordId: string) {
  return JSON.stringify(['record', collectionId, recordId]);
}

export function numericPaths(value: Record<string, unknown>, prefix = '', depth = 0): string[] {
  if (depth > 5) return [];
  return Object.entries(value).flatMap(([key, item]) => {
    const escaped = key.replace(/[\\.]/g, '\\$&');
    const path = prefix ? `${prefix}.${escaped}` : escaped;
    if (typeof item === 'number' && Number.isFinite(item)) return [path];
    return item && typeof item === 'object' && !Array.isArray(item)
      ? numericPaths(item as Record<string, unknown>, path, depth + 1)
      : [];
  });
}
export function numericValue(value: Record<string, unknown>, path: string): number | null {
  const keys: string[] = [];
  let key = '';
  let escaped = false;
  for (const character of path) {
    if (escaped) {
      key += character;
      escaped = false;
    } else if (character === '\\') escaped = true;
    else if (character === '.') {
      keys.push(key);
      key = '';
    } else key += character;
  }
  keys.push(key);
  const item = keys.reduce<unknown>(
    (current, key) => (current && typeof current === 'object' ? (current as Record<string, unknown>)[key] : undefined),
    value
  );
  return typeof item === 'number' && Number.isFinite(item) ? item : null;
}
