import { MarkerType, Position, type Node, type Edge } from 'reactflow';
import type { GroupData } from './group-node';
import { placeLabel, routePorts, type Obstacle, type Port } from './routing';
import { summaryOffset, type GraphRow, type Relation, type RelationKind } from './model';
import type { Point } from './layout';

export const relationStyles: Record<RelationKind, { color: string; dash?: string }> = {
  reference: { color: 'var(--ui-theme-color)' },
  fields: { color: 'var(--ui-theme-color)', dash: '3 3' },
  write: { color: 'var(--ui-access-blue-text)' },
  read: { color: 'var(--ui-access-blue-text)', dash: '4 3' },
  input: { color: 'var(--ui-access-blue-text)' },
  reuse: { color: 'var(--ui-access-blue-text)', dash: '5 4' },
  grant: { color: 'var(--ui-access-purple-text)', dash: '7 3' },
  navigation: { color: 'var(--ui-theme-color)', dash: '2 5' },
  viaCollection: { color: 'var(--ui-access-purple-text)', dash: '5 5' },
};

interface Connection {
  from: GraphRow;
  to: GraphRow;
  kind: RelationKind;
  relations: Relation[];
}
export function canvasEdges(
  nodes: Node<GroupData>[],
  relations: Relation[],
  rowMap: Map<string, GraphRow>,
  selected: string | undefined,
  related: Set<string>
): Edge[] {
  const byGroup = new Map(nodes.map((node) => [node.id, node]));
  const shown = new Map(nodes.map((node) => [node.id, new Set(node.data.rows.map((row) => row.key))]));
  const connections = new Map<string, Connection>();
  for (const relation of relations) {
    const from = rowMap.get(relation.from),
      to = rowMap.get(relation.to);
    if (!from || !to || !byGroup.has(from.group) || !byGroup.has(to.group)) continue;
    if (!shown.get(from.group)?.has(from.key) || !shown.get(to.group)?.has(to.key)) continue;
    if (selected && (!related.has(from.key) || !related.has(to.key))) continue;
    // 同一对分类只画一条汇总线，聚焦和滚动时也不展开成大量行级线。
    const key = `${from.group}/${to.group}`;
    const connection = connections.get(key);
    if (connection) {
      connection.relations.push(relation);
      if (relation.kind === 'write') connection.kind = 'write';
    } else connections.set(key, { from, to, kind: relation.kind, relations: [relation] });
  }
  const boxes = nodes.map((node) => ({ ...node.position, width: node.width || 360, height: node.height || 468 }));
  const center = (node: Node<GroupData>) => ({
    x: node.position.x + (node.width || 360) / 2,
    y: node.position.y + (node.height || 468) / 2,
  });
  const port = (node: Node<GroupData>, kind: RelationKind, side: Position): Port => {
    const width = node.width || 360,
      height = node.height || 468;
    const offset = summaryOffset(kind);
    return {
      side,
      point: {
        x: node.position.x + (side === Position.Left ? 0 : side === Position.Right ? width : width / 2 + offset),
        y: node.position.y + (side === Position.Top ? 0 : side === Position.Bottom ? height : height / 2 + offset),
      },
    };
  };
  const ordered = [...connections.values()].sort((a, b) => {
    const distance = (connection: Connection) => {
      const from = center(byGroup.get(connection.from.group)!),
        to = center(byGroup.get(connection.to.group)!);
      return Math.hypot(from.x - to.x, from.y - to.y);
    };
    return distance(a) - distance(b);
  });
  const reserved: [Point, Point][] = [];
  const labels: Obstacle[] = [];
  const edges: Edge[] = [];
  for (const connection of ordered) {
    const source = byGroup.get(connection.from.group)!,
      target = byGroup.get(connection.to.group)!;
    const from = center(source),
      to = center(target);
    const vertical =
      Math.abs(to.y - from.y) / ((source.height || 468) + (target.height || 468)) >
      Math.abs(to.x - from.x) / ((source.width || 360) + (target.width || 360));
    const sourceSide = vertical
      ? to.y > from.y
        ? Position.Bottom
        : Position.Top
      : to.x > from.x
        ? Position.Right
        : Position.Left;
    const targetSide = vertical
      ? sourceSide === Position.Bottom
        ? Position.Top
        : Position.Bottom
      : sourceSide === Position.Right
        ? Position.Left
        : Position.Right;
    const path = routePorts(
      port(source, connection.kind, sourceSide),
      port(target, connection.kind, targetSide),
      boxes,
      reserved
    );
    if (!path.points.length) continue;
    const style = relationStyles[connection.kind];
    const id = `relation:${connection.relations[0].id}`;
    const count = connection.relations.length;
    const label = placeLabel(path, String(count), boxes, labels);
    edges.push({
      id,
      source: source.id,
      target: target.id,
      type: 'outer',
      sourceHandle: `summary:${connection.kind}:${sourceSide}:source`,
      targetHandle: `summary:${connection.kind}:${targetSide}:target`,
      data: {
        ...path,
        label,
        relationIds: connection.relations.map((relation) => relation.id),
        kinds: [...new Set(connection.relations.map((relation) => relation.kind))],
        summary: true,
      },
      // 间接关联不画箭头，避免暗示 App 直接调用或授权 Stream。
      markerEnd:
        connection.kind === 'viaCollection'
          ? undefined
          : { type: MarkerType.ArrowClosed, color: style.color, width: 12, height: 12 },
      label: count,
      labelStyle: { fill: style.color, fontSize: 12, fontWeight: 600 },
      labelBgStyle: { fill: 'var(--ui-bg-color)', stroke: style.color, strokeOpacity: 0.25 },
      style: { stroke: style.color, strokeWidth: selected ? 1.8 : 1.5, strokeDasharray: style.dash, opacity: 0.72 },
      zIndex: 0,
    });
  }
  return edges;
}
