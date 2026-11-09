import { InputNumber, Select } from 'antd';
import type { ReactNode } from 'react';
import { useTranslate } from '@/hooks';
import HelpTooltip from '@/components/help-tooltip';
import { validWorkflowInterval, WORKFLOW_LIMITS, type WorkflowTrigger } from './program-workflow';
import styles from './workflow.module.scss';

export default function WorkflowRunSettings({
  value,
  onChange,
  readOnly,
  children,
}: {
  value: WorkflowTrigger;
  onChange: (value: WorkflowTrigger) => void;
  readOnly: boolean;
  children?: ReactNode;
}) {
  const t = useTranslate();
  const invalidInterval = value.kind === 'interval' && !validWorkflowInterval(value.intervalMs);
  return (
    <section className={styles['run-settings']} aria-label={t('Workflow.runMode')}>
      <div className={styles['run-mode-row']}>
        <span className={styles['run-mode-label']}>
          {t('Workflow.runMode')}
          <HelpTooltip title={t(`Workflow.runHint.${value.kind}`)} />
        </span>
        <Select
          aria-label={t('Workflow.runMode')}
          value={value.kind}
          disabled={readOnly}
          options={['request', 'interval', 'source-event'].map((kind) => ({
            value: kind,
            label: t(`Workflow.runMode.${kind}`),
          }))}
          onChange={(kind) => onChange({ ...value, kind: kind as WorkflowTrigger['kind'] })}
        />
      </div>
      {value.kind === 'interval' && (
        <label className={styles['interval-setting']}>
          <span>{t('Workflow.intervalSeconds')}</span>
          <InputNumber
            aria-label={t('Workflow.intervalSeconds')}
            min={WORKFLOW_LIMITS.minIntervalMs / 1000}
            max={WORKFLOW_LIMITS.maxIntervalMs / 1000}
            precision={0}
            disabled={readOnly}
            value={value.intervalMs / 1000}
            status={invalidInterval ? 'error' : undefined}
            onChange={(seconds) => onChange({ ...value, intervalMs: (seconds ?? 0) * 1000 })}
          />
          {invalidInterval && <span role="alert">{t('Workflow.issue.interval')}</span>}
        </label>
      )}
      {children && <div className={styles['run-settings-extra']}>{children}</div>}
    </section>
  );
}
