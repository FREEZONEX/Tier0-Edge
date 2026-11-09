import { useState } from 'react';
import { Button, Pagination, Tooltip } from 'antd';
import { Close } from '@carbon/icons-react';
import { useTranslate } from '@/hooks';
import { RowIcon } from './group-node';
import NamespaceDetails from './namespace-details';
import AppEntry from './app-entry';
import type { GraphRow, Relation } from './model';
import styles from './index.module.scss';

interface Props {
  row: GraphRow;
  relations: Relation[];
  rowMap: Map<string, GraphRow>;
  onClose: () => void;
  onSelect: (key: string) => void;
  onMakeCore: () => void;
}

export default function ObjectDetails({ row, relations, rowMap, onClose, onSelect, onMakeCore }: Props) {
  const t = useTranslate();
  const [page, setPage] = useState(1);
  const direct = relations.filter(
    (relation) =>
      relation.from === row.key || relation.to === row.key || rowMap.get(relation.from)?.parentKey === row.key
  );
  const current = Math.min(page, Math.max(1, Math.ceil(direct.length / 20)));
  const visible = direct.slice((current - 1) * 20, current * 20);
  return (
    <aside className={styles.details} aria-label={t('UnsGraph.details')}>
      <header className={styles['preview-title']}>
        <RowIcon row={row} />
        <Tooltip title={`${row.name} · ${row.kind} · ${row.id}`}>
          <strong tabIndex={0}>{row.name}</strong>
        </Tooltip>
        <Button type="text" icon={<Close size={18} />} aria-label={t('UnsGraph.close')} onClick={onClose} />
      </header>
      <div className={styles['details-body']}>
        {row.kind === 'app' && row.meta && <AppEntry key={row.key} app={row.meta} />}
        <section className={styles['detail-section']}>
          <Button size="small" onClick={onMakeCore}>
            {t('UnsGraph.makeCore')}
          </Button>

          {(row.meta?.data.adapter || row.kind === 'binding') && (
            <dl className={styles['detail-fields']}>
              {row.meta?.data.adapter && (
                <div>
                  <dt>{t('UnsGraph.adapter')}</dt>
                  <dd>{row.meta.data.adapter}</dd>
                </div>
              )}
              {row.kind === 'binding' && (
                <div>
                  <dt>{t('UnsGraph.release')}</dt>
                  <dd>
                    {row.release ?? '—'} ·{' '}
                    {row.enabled === undefined ? '—' : t(row.enabled ? 'UnsGraph.enabled' : 'UnsGraph.disabled')}
                  </dd>
                </div>
              )}
            </dl>
          )}
        </section>
        {(['recordSchema', 'sampleSchema'] as const).map((field) => {
          const schema = row.meta?.data[field];
          return (
            schema && (
              <section className={styles['detail-section']} key={field}>
                <h3>
                  {field === 'recordSchema' ? 'Record' : 'Sample'} {t('UnsGraph.schema')}
                </h3>
                <dl className={styles['detail-fields']}>
                  {Object.entries(schema.properties || {}).map(([name, definition]) => (
                    <div key={name}>
                      <dt>{name}</dt>
                      <dd>{definition.type || '—'}</dd>
                    </div>
                  ))}
                </dl>
              </section>
            )
          );
        })}
        {row.group === 'namespace' ? (
          <NamespaceDetails row={row} relations={relations} rowMap={rowMap} onSelect={onSelect} />
        ) : direct.length ? (
          <section className={styles['detail-section']}>
            <h3>{t('UnsGraph.explicitRelations')}</h3>
            {[...new Set(visible.map((relation) => relation.kind))].map((kind) => (
              <div key={kind}>
                <h3>{t(`UnsGraph.${kind}`)}</h3>
                {visible
                  .filter((relation) => relation.kind === kind)
                  .map((relation) => {
                    const other = rowMap.get(
                      relation.from === row.key || rowMap.get(relation.from)?.parentKey === row.key
                        ? relation.to
                        : relation.from
                    );
                    return (
                      <div className={styles['detail-relation']} key={relation.id}>
                        {other ? (
                          <Tooltip title={relation.detail}>
                            <button onClick={() => onSelect(other.key)}>{other.name}</button>
                          </Tooltip>
                        ) : (
                          <span>{t('UnsGraph.unresolved')}</span>
                        )}
                      </div>
                    );
                  })}
              </div>
            ))}
            {direct.length > 20 && (
              <Pagination
                size="small"
                simple={{ readOnly: true }}
                current={current}
                total={direct.length}
                pageSize={20}
                showSizeChanger={false}
                onChange={setPage}
              />
            )}
          </section>
        ) : row.kind === 'app' ? (
          <section className={styles['detail-section']}>
            <p className={styles.hint}>{t('UnsGraph.appNoRelations')}</p>
          </section>
        ) : null}
      </div>
    </aside>
  );
}
