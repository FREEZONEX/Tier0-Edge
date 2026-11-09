import type { FC } from 'react';

import type { DataQualitySummary } from '@/apis/core-api/data-quality';

import { formatCount, formatPercent, type Translate } from '../dashboard-utils';
import styles from '../index.module.scss';

interface QualitySummaryProps {
  summary: DataQualitySummary;
  t: Translate;
}

const QualitySummary: FC<QualitySummaryProps> = ({ summary, t }) => {
  const metrics = [
    { key: 'monitored', label: t('dataQuality.dashboard.monitored'), value: formatCount(summary.monitoredNodes) },
    {
      key: 'healthy',
      label: t('dataQuality.dashboard.healthy'),
      value: formatCount(summary.healthyNodes),
      tone: 'healthy',
    },
    {
      key: 'abnormal',
      label: t('dataQuality.dashboard.abnormal'),
      value: formatCount(summary.abnormalNodes),
      tone: 'critical',
    },
    {
      key: 'success',
      label: t('dataQuality.dashboard.ingestionSuccess'),
      value: formatPercent(summary.ingestionSuccessPercent),
    },
    {
      key: 'drops',
      label: t('dataQuality.dashboard.ingestionDrops'),
      value: formatCount(summary.dropCount),
      tone: summary.dropCount > 0 ? 'critical' : undefined,
    },
  ];

  return (
    <section className={styles['summary-strip']} aria-label={t('dataQuality.dashboard.summary')}>
      {metrics.map((metric) => (
        <div key={metric.key} className={styles['summary-item']}>
          <span className={styles['summary-label']}>{metric.label}</span>
          <strong className={metric.tone ? styles[`tone-${metric.tone}`] : undefined}>{metric.value}</strong>
        </div>
      ))}
    </section>
  );
};

export default QualitySummary;
