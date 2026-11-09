import { BaseEdge, type EdgeProps } from 'reactflow';
import { useTranslate } from '@/hooks';
import type { RelationKind } from './model';
import { roundedPath, type RoutedPath } from './routing';

// 路径由避障算法生成；不再把不同关系强制绕到同一条外侧通道。
export default function OuterEdge({
  id,
  data,
  style,
  markerEnd,
  label,
  labelStyle,
  labelBgStyle,
}: EdgeProps<RoutedPath & { relationIds: string[]; kinds: RelationKind[] }>) {
  const t = useTranslate();
  return (
    <g>
      <title>{`${(data?.kinds || []).map((kind) => t(`UnsGraph.${kind}`)).join(' / ')} · ${data?.relationIds.length || 0}`}</title>
      <BaseEdge
        id={id}
        path={roundedPath(data?.points || [])}
        style={style}
        markerEnd={markerEnd}
        label={label}
        labelX={data?.label.x}
        labelY={data?.label.y}
        labelStyle={labelStyle}
        labelBgStyle={labelBgStyle}
        labelBgPadding={[7, 3]}
        labelBgBorderRadius={10}
        interactionWidth={18}
      />
    </g>
  );
}
