import { Collapse, Input } from 'antd';
import { Add, Close } from '@carbon/icons-react';
import { useTranslate } from '@/hooks';
import ComButton from '@/components/com-button';
import type { WorkflowAction } from './program-workflow';
import styles from './workflow.module.scss';

export default function WorkflowRequestOptions({
  action,
  onChange,
}: {
  action: Extract<WorkflowAction, { kind: 'source' }>;
  onChange: (patch: Partial<WorkflowAction>) => void;
}) {
  const t = useTranslate();
  return (
    <Collapse
      ghost
      items={(['query', 'headers'] as const).map((kind) => {
        const pairs = action[kind] ?? [];
        const title = t(`Ingest.${kind}`);
        return {
          key: kind,
          label: `${title}${pairs.length ? ` (${pairs.length})` : ''}`,
          children: (
            <div className={styles['form-stack']}>
              {pairs.map((pair, index) => (
                <div className={styles['inline-row']} key={index}>
                  <Input
                    aria-label={`${title} ${index + 1} ${t('Ingest.key')}`}
                    placeholder={t('Ingest.key')}
                    value={pair.key}
                    onChange={(event) =>
                      onChange({
                        [kind]: pairs.map((row, i) => (i === index ? { ...row, key: event.target.value } : row)),
                      })
                    }
                  />
                  <Input
                    aria-label={`${title} ${index + 1} ${t('Ingest.value')}`}
                    placeholder={t('Ingest.value')}
                    value={pair.value}
                    onChange={(event) =>
                      onChange({
                        [kind]: pairs.map((row, i) => (i === index ? { ...row, value: event.target.value } : row)),
                      })
                    }
                  />
                  <ComButton
                    type="text"
                    aria-label={`${t('common.delete')} ${title} ${index + 1}`}
                    icon={<Close />}
                    onClick={() => onChange({ [kind]: pairs.filter((_, i) => i !== index) })}
                  />
                </div>
              ))}
              <ComButton icon={<Add />} onClick={() => onChange({ [kind]: [...pairs, { key: '', value: '' }] })}>
                {t('common.add')}
              </ComButton>
            </div>
          ),
        };
      })}
    />
  );
}
