import { useMemo, useState } from 'react';
import { Button, Pagination } from 'antd';
import { Close, Flow } from '@carbon/icons-react';
import { useTranslate } from '@/hooks';
import { RowIcon } from './group-node';
import type { GraphRow, Relation, RelationKind } from './model';
import styles from './index.module.scss';

interface Props {
  relations: Relation[];
  rowMap: Map<string, GraphRow>;
  onClose: () => void;
  onSelect: (key: string) => void;
}

export default function RelationDetails({ relations, rowMap, onClose, onSelect }: Props) {
  const t = useTranslate();
  const [page, setPage] = useState(1);
  const groups = useMemo(() => {
    const result = new Map<
      string,
      { anchor: GraphRow; kind: RelationKind; entries: { row: GraphRow; detail?: string; via?: string[] }[] }
    >();
    for (const kind of new Set(relations.map((relation) => relation.kind))) {
      const matches = relations.filter((relation) => relation.kind === kind);
      // 一对多和多对一均把重复的参照对象提到组标题。
      const reverse =
        new Set(matches.map((relation) => relation.to)).size < new Set(matches.map((relation) => relation.from)).size;
      for (const relation of matches) {
        const anchor = rowMap.get(reverse ? relation.to : relation.from);
        const row = rowMap.get(reverse ? relation.from : relation.to);
        if (!anchor || !row) continue;
        const key = `${kind}:${anchor.key}`;
        const group = result.get(key) || { anchor, kind, entries: [] };
        group.entries.push({ row, detail: relation.detail, via: relation.via });
        result.set(key, group);
      }
    }
    return [...result.values()];
  }, [relations, rowMap]);
  const total = groups.reduce((count, group) => count + group.entries.length, 0);
  const current = Math.min(page, Math.max(1, Math.ceil(total / 20)));
  let offset = 0;
  return (
    <aside className={styles.details} aria-label={t('UnsGraph.relationDetails')}>
      <header className={styles['preview-title']}>
        <Flow />
        <strong>
          {t('UnsGraph.relationDetails')} · {total}
        </strong>
        <Button type="text" icon={<Close />} aria-label={t('UnsGraph.close')} onClick={onClose} />
      </header>
      <div className={styles['details-body']}>
        {groups.map((group) => {
          const start = offset;
          offset += group.entries.length;
          const entries = group.entries.slice(
            Math.max(0, (current - 1) * 20 - start),
            Math.max(0, current * 20 - start)
          );
          if (!entries.length) return null;
          return (
            <section className={styles['relation-group']} key={`${group.kind}:${group.anchor.key}`}>
              <header>
                <button onClick={() => onSelect(group.anchor.key)}>
                  <RowIcon row={group.anchor} />
                  {group.anchor.name}
                </button>
                <span>
                  {t(`UnsGraph.${group.kind}`)} · {group.entries.length}
                </span>
              </header>
              {entries.map(({ row, detail, via }, index) => (
                <div className={styles['relation-target']} key={`${row.key}:${index}`}>
                  <button onClick={() => onSelect(row.key)}>
                    <RowIcon row={row} />
                    {row.name}
                  </button>
                  {detail && <small>{detail}</small>}
                  {!!via?.length && (
                    <small>
                      {t('UnsGraph.viaCollection')} · {via.length}
                      {via.map((key) => {
                        const collection = rowMap.get(key);
                        return collection ? (
                          <button key={key} onClick={() => onSelect(key)}>
                            {collection.name}
                          </button>
                        ) : null;
                      })}
                    </small>
                  )}
                </div>
              ))}
            </section>
          );
        })}
      </div>
      {total > 20 && (
        <footer className={styles['preview-footer']}>
          <Pagination
            size="small"
            simple={{ readOnly: true }}
            current={current}
            total={total}
            pageSize={20}
            showSizeChanger={false}
            onChange={setPage}
          />
        </footer>
      )}
    </aside>
  );
}
