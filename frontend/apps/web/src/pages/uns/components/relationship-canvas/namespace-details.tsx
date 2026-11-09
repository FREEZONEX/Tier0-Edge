import { useMemo } from 'react';
import { Tooltip } from 'antd';
import { useTranslate } from '@/hooks';
import HelpTooltip from '@/components/help-tooltip';
import { RowIcon } from './group-node';
import { namespaceRelations, type NamespaceLink } from './namespace-relations';
import type { GraphRow, Relation } from './model';
import styles from './index.module.scss';

interface Props {
  row: GraphRow;
  relations: Relation[];
  rowMap: Map<string, GraphRow>;
  onSelect: (key: string) => void;
}

export default function NamespaceDetails({ row, relations, rowMap, onSelect }: Props) {
  const t = useTranslate();
  const links = useMemo(() => namespaceRelations(row, [...rowMap.values()], relations), [row, rowMap, relations]);
  const renderPath = (path: NamespaceLink['paths'][number], index: number) => (
    <small className={styles['namespace-path']} key={index}>
      <span>{t(`UnsGraph.${path.kind}`)}</span>
      {path.keys
        .map((key) => {
          const via = rowMap.get(key);
          return via ? `${t(`UnsGraph.${via.kind}`)} · ${via.name}` : t('UnsGraph.unresolved');
        })
        .join(' / ')}
    </small>
  );
  return (
    <>
      {(['collection', 'source', 'stream', 'app'] as const).map((group) => {
        const items = [...links.values()].filter((link) => link.row.group === group);
        if (!items.length) return null;
        return (
          <section className={styles['detail-section']} key={group}>
            <h3 className={styles['preview-section-head']}>
              <span className={styles['namespace-heading']}>
                {t(`UnsGraph.${group}`)}
                {group === 'collection' && <HelpTooltip title={t('UnsGraph.namespaceHint')} />}
              </span>
              <span className={styles['preview-muted']}>{items.length}</span>
            </h3>
            {items.map((link) => (
              <div className={styles['namespace-link']} key={link.row.key}>
                <Tooltip title={<>{link.paths.map(renderPath)}</>} trigger={['hover', 'focus']}>
                  <button onClick={() => onSelect(link.row.key)}>
                    <RowIcon row={link.row} />
                    <span>{link.row.name}</span>
                  </button>
                </Tooltip>
              </div>
            ))}
          </section>
        );
      })}
    </>
  );
}
