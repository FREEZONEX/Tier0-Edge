import { useId, type RefObject } from 'react';
import { useTranslate } from '@/hooks';
import type { ConnectRelation, ConnectStreamCard, ConnectTarget } from './connect-model';
import { useConnectCables } from './use-connect-cables';
import styles from './connect-workspace.module.scss';

export default function ConnectCables({
  stage,
  relations,
  parentByKey,
  streams,
  targets,
  selected,
  focusedResource,
  onSelect,
  onEdit,
}: {
  stage: RefObject<HTMLDivElement>;
  relations: ConnectRelation[];
  parentByKey: ReadonlyMap<string, string>;
  streams: ConnectStreamCard[];
  targets: ConnectTarget[];
  selected?: string;
  focusedResource?: string;
  onSelect: (id: string) => void;
  onEdit: (id: number, editable: boolean) => void;
}) {
  const t = useTranslate();
  const marker = useId();
  const { cables, canvasClip } = useConnectCables(stage, relations, parentByKey);
  const clip = `${marker}-canvas`;
  return (
    <svg className={styles.cables} aria-label={t('UnsConnect.legend')}>
      <defs>
        <clipPath id={clip} clipPathUnits="userSpaceOnUse">
          <rect {...canvasClip} />
        </clipPath>
        <marker
          id={marker}
          markerWidth="8"
          markerHeight="8"
          refX="7"
          refY="4"
          orient="auto"
          markerUnits="userSpaceOnUse"
        >
          <path d="M 0 0 L 8 4 L 0 8 Z" fill="var(--ui-theme-color)" />
        </marker>
      </defs>
      {cables.map(({ relation, path, anchor }) => {
        const key = relation.kind === 'read' ? relation.to : relation.from;
        const stream = streams.find((item) => `stream:${item.id}` === key);
        if (!stream) return null;
        const active =
          selected === relation.id ||
          focusedResource === anchor ||
          focusedResource === (relation.kind === 'read' ? relation.from : relation.to);
        const resource = targets.find(
          (target) => target.key === (relation.kind === 'read' ? relation.from : relation.to)
        );
        const label = `${stream.name} · ${t(`UnsConnect.relation.${relation.kind}`)} · ${resource?.name || resource?.id || ''}`;
        return (
          <g key={relation.id} className={active ? styles['cable-active'] : undefined}>
            <path d={path} className={styles['cable-line']} markerEnd={`url(#${marker})`} />
            <path
              d={path}
              className={styles['cable-hit']}
              clipPath={`url(#${clip})`}
              role="button"
              tabIndex={0}
              aria-label={label}
              aria-pressed={selected === relation.id}
              onClick={() => onSelect(relation.id)}
              onDoubleClick={() => onEdit(stream.id, stream.editable)}
              onKeyDown={(event) => {
                if (event.key === 'Enter' || event.key === ' ') {
                  event.preventDefault();
                  onSelect(relation.id);
                }
              }}
            >
              <title>{label}</title>
            </path>
          </g>
        );
      })}
    </svg>
  );
}
