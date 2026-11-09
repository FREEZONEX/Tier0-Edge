import type { Point } from './layout';

export interface Obstacle {
  x: number;
  y: number;
  width: number;
  height: number;
}
export interface RoutedPath {
  points: Point[];
  label: Point;
}
export interface Port {
  point: Point;
  side: 'left' | 'right' | 'top' | 'bottom';
}
export function placeLabel(path: RoutedPath, text: string, obstacles: Obstacle[], placed: Obstacle[]): Point {
  const width = [...text].reduce((sum, character) => sum + (character.charCodeAt(0) > 127 ? 12 : 7), 16);
  const height = 28;
  const segments = path.points
    .slice(1)
    .map((point, index) => [path.points[index], point] as [Point, Point])
    .sort(([a, b], [c, d]) => length(c, d) - length(a, b));
  const overlaps = (box: Obstacle, other: Obstacle) =>
    box.x < other.x + other.width + 6 &&
    box.x + box.width + 6 > other.x &&
    box.y < other.y + other.height + 6 &&
    box.y + box.height + 6 > other.y;
  for (const [a, b] of segments)
    for (const fraction of [0.5, 0.25, 0.75, 0.15, 0.85]) {
      const point = { x: a.x + (b.x - a.x) * fraction, y: a.y + (b.y - a.y) * fraction };
      const box = { x: point.x - width / 2, y: point.y - height / 2, width, height };
      if ([...obstacles, ...placed].some((other) => overlaps(box, other))) continue;
      placed.push(box);
      return point;
    }
  placed.push({ x: path.label.x - width / 2, y: path.label.y - height / 2, width, height });
  return path.label;
}
const same = (a: Point, b: Point) => a.x === b.x && a.y === b.y;
const length = (a: Point, b: Point) => Math.abs(a.x - b.x) + Math.abs(a.y - b.y);

function blocked(a: Point, b: Point, obstacles: Obstacle[]) {
  return obstacles.some((box) =>
    a.y === b.y
      ? a.y > box.y && a.y < box.y + box.height && Math.max(a.x, b.x) > box.x && Math.min(a.x, b.x) < box.x + box.width
      : a.x > box.x && a.x < box.x + box.width && Math.max(a.y, b.y) > box.y && Math.min(a.y, b.y) < box.y + box.height
  );
}

/** 小型正交可见网格：避开全部面板，对已用通道和过多折弯增加成本。 */
export function routePorts(source: Port, target: Port, boxes: Obstacle[], reserved: [Point, Point][] = []): RoutedPath {
  const escape = (port: Port): Point => ({
    x: port.point.x + (port.side === 'left' ? -28 : port.side === 'right' ? 28 : 0),
    y: port.point.y + (port.side === 'top' ? -28 : port.side === 'bottom' ? 28 : 0),
  });
  const start = escape(source),
    end = escape(target);
  const obstacles = boxes.map((box) => ({
    x: box.x - 18,
    y: box.y - 18,
    width: box.width + 36,
    height: box.height + 36,
  }));
  const xs = new Set([start.x, end.x]),
    ys = new Set([start.y, end.y]);
  for (const box of boxes)
    for (const offset of [28, 48, 68]) {
      xs.add(box.x - offset);
      xs.add(box.x + box.width + offset);
      ys.add(box.y - offset);
      ys.add(box.y + box.height + offset);
    }
  const xValues = [...xs].sort((a, b) => a - b),
    yValues = [...ys].sort((a, b) => a - b);
  const points = yValues.flatMap((y) => xValues.map((x) => ({ x, y })));
  const startIndex = points.findIndex((point) => same(point, start));
  const endIndex = points.findIndex((point) => same(point, end));
  const allowed = points.map(
    (point) =>
      !obstacles.some(
        (box) => point.x > box.x && point.x < box.x + box.width && point.y > box.y && point.y < box.y + box.height
      )
  );
  const distances = new Map<string, number>([[`${startIndex}:0`, 0]]);
  const previous = new Map<string, string>();
  type SearchState = { index: number; direction: number; cost: number; score: number };
  const pending: SearchState[] = [];
  // 拖动面板时会实时重算路径；小顶堆避免每个搜索步骤都重新排序整个队列。
  const enqueue = (state: SearchState) => {
    pending.push(state);
    let index = pending.length - 1;
    while (index > 0) {
      const parent = Math.floor((index - 1) / 2);
      if (pending[parent].score <= pending[index].score) break;
      [pending[parent], pending[index]] = [pending[index], pending[parent]];
      index = parent;
    }
  };
  const dequeue = () => {
    const first = pending[0],
      last = pending.pop()!;
    if (pending.length) {
      pending[0] = last;
      let index = 0;
      while (index * 2 + 1 < pending.length) {
        let child = index * 2 + 1;
        if (child + 1 < pending.length && pending[child + 1].score < pending[child].score) child += 1;
        if (pending[index].score <= pending[child].score) break;
        [pending[index], pending[child]] = [pending[child], pending[index]];
        index = child;
      }
    }
    return first;
  };
  enqueue({ index: startIndex, direction: 0, cost: 0, score: length(start, end) });
  let finalKey: string | undefined;
  while (pending.length) {
    const current = dequeue();
    const key = `${current.index}:${current.direction}`;
    if (current.cost !== distances.get(key)) continue;
    if (current.index === endIndex) {
      finalKey = key;
      break;
    }
    const column = current.index % xValues.length;
    const row = Math.floor(current.index / xValues.length);
    const candidates = [
      column > 0 ? current.index - 1 : -1,
      column < xValues.length - 1 ? current.index + 1 : -1,
      row > 0 ? current.index - xValues.length : -1,
      row < yValues.length - 1 ? current.index + xValues.length : -1,
    ];
    for (const index of candidates) {
      if (index < 0 || !allowed[index] || blocked(points[current.index], points[index], obstacles)) continue;
      const a = points[current.index],
        b = points[index];
      const direction = a.y === b.y ? 1 : 2;
      const overlap = reserved.some(
        ([c, d]) =>
          (direction === 1 &&
            c.y === d.y &&
            Math.abs(c.y - a.y) < 12 &&
            Math.min(Math.max(a.x, b.x), Math.max(c.x, d.x)) > Math.max(Math.min(a.x, b.x), Math.min(c.x, d.x))) ||
          (direction === 2 &&
            c.x === d.x &&
            Math.abs(c.x - a.x) < 12 &&
            Math.min(Math.max(a.y, b.y), Math.max(c.y, d.y)) > Math.max(Math.min(a.y, b.y), Math.min(c.y, d.y)))
      );
      const cost =
        current.cost +
        length(a, b) * (overlap ? 2.8 : 1) +
        (current.direction && current.direction !== direction ? 32 : 0);
      const nextKey = `${index}:${direction}`;
      if (cost < (distances.get(nextKey) ?? Infinity)) {
        distances.set(nextKey, cost);
        previous.set(nextKey, key);
        enqueue({ index, direction, cost, score: cost + length(b, end) });
      }
    }
  }
  if (!finalKey) return { points: [], label: start }; // 重叠的手动布局暂不连线，避免伪造穿卡片路径。
  const path: Point[] = [];
  for (let key: string | undefined = finalKey; key; key = previous.get(key))
    path.unshift(points[Number(key.split(':')[0])]);
  const combined = [source.point, ...path, target.point];
  const simplified: Point[] = [];
  for (const point of combined) {
    if (simplified.length && same(simplified[simplified.length - 1], point)) continue;
    while (simplified.length > 1) {
      const a = simplified[simplified.length - 2],
        b = simplified[simplified.length - 1];
      if ((a.x === b.x && b.x === point.x) || (a.y === b.y && b.y === point.y)) simplified.pop();
      else break;
    }
    simplified.push(point);
  }
  let longest = 0,
    label = start;
  for (let index = 1; index < simplified.length; index += 1) {
    const a = simplified[index - 1],
      b = simplified[index];
    reserved.push([a, b]);
    if (length(a, b) > longest) {
      longest = length(a, b);
      label = { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 };
    }
  }
  return { points: simplified, label };
}

export function roundedPath(points: Point[]) {
  if (!points.length) return '';
  let path = `M ${points[0].x},${points[0].y}`;
  for (let index = 1; index < points.length; index += 1) {
    const corner = points[index],
      next = points[index + 1],
      previous = points[index - 1];
    if (!next) {
      path += ` L ${corner.x},${corner.y}`;
      break;
    }
    const radius = Math.min(8, length(previous, corner) / 2, length(corner, next) / 2);
    const before = {
      x: corner.x + Math.sign(previous.x - corner.x) * radius,
      y: corner.y + Math.sign(previous.y - corner.y) * radius,
    };
    const after = {
      x: corner.x + Math.sign(next.x - corner.x) * radius,
      y: corner.y + Math.sign(next.y - corner.y) * radius,
    };
    path += ` L ${before.x},${before.y} Q ${corner.x},${corner.y} ${after.x},${after.y}`;
  }
  return path;
}
