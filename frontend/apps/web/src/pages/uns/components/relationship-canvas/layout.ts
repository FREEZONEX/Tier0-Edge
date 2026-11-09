import type { Group } from './model';

export interface Point {
  x: number;
  y: number;
}
export interface PanelSize {
  width: number;
  height: number;
}
export interface PanelLink {
  from: Group;
  to: Group;
  weight: number;
}
export type PanelPositions = Partial<Record<Group, Point>>;

function permutations<T>(values: T[]): T[][] {
  if (values.length < 2) return [values];
  return values.flatMap((value, index) =>
    permutations(values.filter((_, other) => index !== other)).map((rest) => [value, ...rest])
  );
}

function clearance(a: PanelSize, b: PanelSize, dx: number, dy: number, gap = 110) {
  const length = Math.hypot(dx, dy) || 1;
  return Math.min(
    ((a.width / 2 + b.width / 2 + gap) * length) / Math.max(Math.abs(dx), 0.01),
    ((a.height / 2 + b.height / 2 + gap) * length) / Math.max(Math.abs(dy), 0.01)
  );
}

function crossing(a: Point, b: Point, c: Point, d: Point) {
  const cross = (p: Point, q: Point, r: Point) => (q.x - p.x) * (r.y - p.y) - (q.y - p.y) * (r.x - p.x);
  return cross(a, b, c) * cross(a, b, d) < 0 && cross(c, d, a) * cross(c, d, b) < 0;
}

/** 按关系与面板尺寸做带碰撞约束的加权布局；面板增多时用启发式环序限制交互开销。 */
export function layoutPanels(
  ids: Group[],
  core: Group,
  sizes: Record<Group, PanelSize>,
  links: PanelLink[],
  aspect = 1.6
): PanelPositions {
  const others = ids.filter((id) => id !== core);
  // 少量分类用紧凑对齐布局。无直接关系的分类也不应被力布局推到远处。
  if (ids.length <= 3) {
    const ordered = [...others.slice(0, 1), core, ...others.slice(1)];
    const horizontal = aspect >= 1.1;
    const maxWidth = Math.max(...ids.map((id) => sizes[id].width));
    const maxHeight = Math.max(...ids.map((id) => sizes[id].height));
    let offset = 80;
    return Object.fromEntries(
      ordered.map((id) => {
        const point = horizontal
          ? { x: offset, y: 80 + (maxHeight - sizes[id].height) / 2 }
          : { x: 80 + (maxWidth - sizes[id].width) / 2, y: offset };
        offset += (horizontal ? sizes[id].width : sizes[id].height) + 80;
        return [id, point];
      })
    );
  }
  const weights = new Map(links.map((link) => [[link.from, link.to].sort().join('/'), 1 + Math.log1p(link.weight)]));
  let best: Record<string, Point> = { [core]: { x: 0, y: 0 } };
  let bestScore = Infinity;
  const priority = [...others].sort(
    (a, b) =>
      links.filter((link) => link.from === b || link.to === b).reduce((sum, link) => sum + link.weight, 0) -
      links.filter((link) => link.from === a || link.to === a).reduce((sum, link) => sum + link.weight, 0)
  );
  const orders =
    others.length < 5
      ? permutations(others)
      : priority.flatMap((_, index) => {
          const rotated = [...priority.slice(index), ...priority.slice(0, index)];
          return [rotated, [...rotated].reverse(), [rotated[0], rotated[2], rotated[4], rotated[1], rotated[3]]];
        });
  for (const order of orders)
    for (const rotation of [0, Math.PI / 4, Math.PI / 2]) {
      const centers: Record<string, Point> = { [core]: { x: 0, y: 0 } };
      order.forEach((id, index) => {
        const angle = (index * Math.PI * 2) / Math.max(order.length, 1) + rotation;
        const dx = Math.cos(angle),
          dy = Math.sin(angle);
        const radius = clearance(sizes[core], sizes[id], dx, dy, 160);
        centers[id] = { x: dx * radius, y: dy * radius };
      });
      for (let step = 0; step < 100; step += 1) {
        const forces: Record<string, Point> = Object.fromEntries(ids.map((id) => [id, { x: 0, y: 0 }]));
        for (let i = 0; i < ids.length; i += 1)
          for (let j = i + 1; j < ids.length; j += 1) {
            const a = ids[i],
              b = ids[j];
            const dx = centers[b].x - centers[a].x,
              dy = centers[b].y - centers[a].y;
            const distance = Math.hypot(dx, dy) || 1;
            const target = clearance(sizes[a], sizes[b], dx, dy);
            const weight = weights.get([a, b].sort().join('/')) || 0;
            const spring = weight
              ? (distance - target) * Math.min(weight, 4) * 0.04
              : -Math.max(0, target + 70 - distance) * 0.04;
            forces[a].x += (dx / distance) * spring;
            forces[a].y += (dy / distance) * spring;
            forces[b].x -= (dx / distance) * spring;
            forces[b].y -= (dy / distance) * spring;
            const overlapX = (sizes[a].width + sizes[b].width) / 2 + 110 - Math.abs(dx);
            const overlapY = (sizes[a].height + sizes[b].height) / 2 + 110 - Math.abs(dy);
            if (overlapX > 0 && overlapY > 0) {
              const axis = overlapX < overlapY ? 'x' : 'y';
              const direction = Math.sign(axis === 'x' ? dx : dy) || 1;
              const push = (axis === 'x' ? overlapX : overlapY) * 0.5;
              forces[a][axis] -= direction * push;
              forces[b][axis] += direction * push;
            }
          }
        const centroid = ids.reduce(
          (sum, id) => ({ x: sum.x + centers[id].x / ids.length, y: sum.y + centers[id].y / ids.length }),
          { x: 0, y: 0 }
        );
        for (const id of others) {
          centers[id].x += Math.max(-24, Math.min(24, forces[id].x - centroid.x * 0.025));
          centers[id].y += Math.max(-24, Math.min(24, forces[id].y - centroid.y * 0.025));
        }
      }
      // 按当前画布比例展宽/压缩，避免在宽屏上生成狭长布局导致字体被过度缩小。
      const relaxed = panelBounds(ids, centers, sizes, true);
      const stretch = Math.max(0.65, Math.min(1.8, Math.sqrt((aspect * relaxed.height) / relaxed.width)));
      for (const id of others) {
        centers[id].x *= stretch;
        centers[id].y /= stretch;
      }
      // 力的平衡不能替代硬间距约束；路由至少需要独立的进出线通道。
      for (let pass = 0; pass < 60; pass += 1) {
        let moved = false;
        for (let i = 0; i < ids.length; i += 1)
          for (let j = i + 1; j < ids.length; j += 1) {
            const a = ids[i],
              b = ids[j];
            const dx = centers[b].x - centers[a].x,
              dy = centers[b].y - centers[a].y;
            const overlapX = (sizes[a].width + sizes[b].width) / 2 + 120 - Math.abs(dx);
            const overlapY = (sizes[a].height + sizes[b].height) / 2 + 120 - Math.abs(dy);
            if (overlapX > 0 && overlapY > 0) {
              const axis = overlapX < overlapY ? 'x' : 'y';
              const push = (axis === 'x' ? overlapX : overlapY) + 0.5;
              const direction = Math.sign(axis === 'x' ? dx : dy) || 1;
              if (a !== core) centers[a][axis] -= direction * push * (b === core ? 1 : 0.5);
              if (b !== core) centers[b][axis] += direction * push * (a === core ? 1 : 0.5);
              moved = true;
            }
          }
        if (!moved) break;
      }
      const bounds = panelBounds(ids, centers, sizes, true);
      let score =
        ((bounds.x + bounds.width / 2) ** 2 + (bounds.y + bounds.height / 2) ** 2) * 4 +
        bounds.width * bounds.height * 0.012 +
        Math.log(bounds.width / bounds.height / aspect) ** 2 * 400000;
      for (const link of links)
        score +=
          Math.hypot(centers[link.from].x - centers[link.to].x, centers[link.from].y - centers[link.to].y) ** 2 *
          (weights.get([link.from, link.to].sort().join('/')) || 1) *
          0.025;
      for (let i = 0; i < links.length; i += 1)
        for (let j = i + 1; j < links.length; j += 1) {
          const a = links[i],
            b = links[j];
          if (
            new Set([a.from, a.to, b.from, b.to]).size === 4 &&
            crossing(centers[a.from], centers[a.to], centers[b.from], centers[b.to])
          )
            score += 120000;
        }
      for (let i = 0; i < ids.length; i += 1)
        for (let j = i + 1; j < ids.length; j += 1) {
          const a = ids[i],
            b = ids[j];
          if (
            Math.abs(centers[a].x - centers[b].x) < (sizes[a].width + sizes[b].width) / 2 + 80 &&
            Math.abs(centers[a].y - centers[b].y) < (sizes[a].height + sizes[b].height) / 2 + 80
          )
            score += 10000000;
        }
      if (score < bestScore) {
        bestScore = score;
        best = centers;
      }
    }
  const bounds = panelBounds(ids, best, sizes, true);
  return Object.fromEntries(
    ids.map((id) => [
      id,
      {
        x: Math.round(best[id].x - sizes[id].width / 2 - bounds.x + 100),
        y: Math.round(best[id].y - sizes[id].height / 2 - bounds.y + 100),
      },
    ])
  );
}

export function panelBounds(
  ids: Group[],
  positions: Record<string, Point>,
  sizes: Record<Group, PanelSize>,
  centers = false
) {
  const xs = ids.map((id) => positions[id].x - (centers ? sizes[id].width / 2 : 0));
  const ys = ids.map((id) => positions[id].y - (centers ? sizes[id].height / 2 : 0));
  const x = Math.min(...xs),
    y = Math.min(...ys);
  return {
    x,
    y,
    width: Math.max(...ids.map((id, index) => xs[index] + sizes[id].width)) - x,
    height: Math.max(...ids.map((id, index) => ys[index] + sizes[id].height)) - y,
  };
}
