import { Collapse, Input, InputNumber, Select, Alert } from 'antd';
import { useTranslate } from '@/hooks';
import { WORKFLOW_LIMITS, type WorkflowAction } from './program-workflow';
import styles from './workflow.module.scss';

type Paging = Extract<WorkflowAction, { kind: 'source' }>['pagination'];
export default function WorkflowPagination({
  value,
  method,
  onChange,
}: {
  value?: Paging;
  method: string;
  onChange: (value: Paging) => void;
}) {
  const t = useTranslate();
  const patch = (next: Partial<NonNullable<Paging>>) => value && onChange({ ...value, ...next });
  return (
    <Collapse
      ghost
      items={[
        {
          key: 'pagination',
          label: t('Ingest.pagination'),
          children: (
            <div className={styles['form-stack']}>
              <Select
                aria-label={t('Ingest.pagination')}
                value={value?.mode || 'none'}
                disabled={method !== 'GET'}
                options={['none', 'cursor', 'page'].map((value) => ({ value, label: t(`Ingest.paging.${value}`) }))}
                onChange={(mode) =>
                  onChange(
                    mode === 'none'
                      ? undefined
                      : {
                          mode: mode as 'cursor' | 'page',
                          itemsPath: 'items',
                          nextPath: 'nextCursor',
                          parameter: mode === 'page' ? 'page' : 'cursor',
                          sizeParameter: 'limit',
                          maxPages: 10,
                          pageSize: 100,
                          startPage: 1,
                        }
                  )
                }
              />
              {value && (
                <>
                  {(
                    [
                      'itemsPath',
                      'parameter',
                      'sizeParameter',
                      ...(value.mode === 'cursor' ? (['nextPath'] as const) : []),
                    ] as const
                  ).map((key) => (
                    <label className={styles.field} key={key}>
                      <span>{t(`Ingest.paging.${key}`)}</span>
                      <Input
                        aria-label={t(`Ingest.paging.${key}`)}
                        value={value[key]}
                        onChange={(e) => patch({ [key]: e.target.value })}
                      />
                    </label>
                  ))}
                  {(['pageSize', 'maxPages', ...(value.mode === 'page' ? (['startPage'] as const) : [])] as const).map(
                    (key) => (
                      <label className={styles.field} key={key}>
                        <span>{t(`Ingest.paging.${key}`)}</span>
                        <InputNumber
                          aria-label={t(`Ingest.paging.${key}`)}
                          min={key === 'startPage' ? 0 : 1}
                          max={
                            key === 'maxPages'
                              ? WORKFLOW_LIMITS.sourceCalls
                              : key === 'pageSize'
                                ? WORKFLOW_LIMITS.records
                                : undefined
                          }
                          precision={0}
                          value={value[key]}
                          onChange={(n) => patch({ [key]: n ?? 0 })}
                        />
                      </label>
                    )
                  )}
                  <Alert type="info" message={t('Ingest.paging.limitHint')} />
                </>
              )}
            </div>
          ),
        },
      ]}
    />
  );
}
