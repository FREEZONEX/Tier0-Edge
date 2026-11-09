import type { FC } from 'react';

import type { DataQualityStatus, DataQualitySummary } from '@/apis/core-api/data-quality';

import { formatCount, type Translate } from '../dashboard-utils';
import styles from '../index.module.scss';

interface QualityOverviewProps {
  status?: DataQualityStatus;
  summary: DataQualitySummary;
  t: Translate;
}

const QualityOverview: FC<QualityOverviewProps> = ({ status, summary, t }) => {
  const totalNodes = Math.max(1, summary.monitoredNodes);
  const ranking = Object.entries(status?.activeRuleCounts || {})
    .filter(([, count]) => count > 0)
    .sort((left, right) => right[1] - left[1])
    .slice(0, 6);
  const maxRuleCount = Math.max(1, ...ranking.map(([, count]) => count));
  const runtimeCounts = [
    { key: 'received', label: t('dataQuality.received'), value: summary.receivedCount },
    { key: 'persisted', label: t('dataQuality.persisted'), value: summary.persistedCount },
    { key: 'dropped', label: t('dataQuality.dropped'), value: summary.dropCount },
    { key: 'evaluationDrops', label: t('dataQuality.evalDrops'), value: status?.evaluationDrops || 0 },
    { key: 'connectorDrops', label: t('dataQuality.connectorEventDrops'), value: status?.connectorEventDrops || 0 },
  ];

  return (
    <div className={styles['overview-grid']}>
      <section className={styles.panel}>
        <div className={styles['panel-heading']}>
          <div>
            <h2>{t('dataQuality.dashboard.healthDistribution')}</h2>
            <p>{t('dataQuality.dashboard.healthDistributionDesc')}</p>
          </div>
        </div>
        <div className={styles['distribution-bar']} aria-label={t('dataQuality.dashboard.healthDistribution')}>
          <i
            className={styles['distribution-healthy']}
            style={{ width: `${(summary.healthyNodes / totalNodes) * 100}%` }}
          />
          <i
            className={styles['distribution-warming']}
            style={{ width: `${(summary.warmingNodes / totalNodes) * 100}%` }}
          />
          <i
            className={styles['distribution-abnormal']}
            style={{ width: `${(summary.abnormalNodes / totalNodes) * 100}%` }}
          />
        </div>
        <div className={styles['distribution-labels']}>
          <span>
            <i className={styles['distribution-healthy']} />
            {t('dataQuality.dashboard.healthy')} <strong>{formatCount(summary.healthyNodes)}</strong>
          </span>
          <span>
            <i className={styles['distribution-warming']} />
            {t('dataQuality.dashboard.health.warming')} <strong>{formatCount(summary.warmingNodes)}</strong>
          </span>
          <span>
            <i className={styles['distribution-abnormal']} />
            {t('dataQuality.dashboard.abnormal')} <strong>{formatCount(summary.abnormalNodes)}</strong>
          </span>
        </div>
        <div className={styles['runtime-counts']}>
          {runtimeCounts.map((item) => (
            <div key={item.key}>
              <span>{item.label}</span>
              <strong>{formatCount(item.value)}</strong>
            </div>
          ))}
        </div>
      </section>

      <section className={styles.panel}>
        <div className={styles['panel-heading']}>
          <div>
            <h2>{t('dataQuality.dashboard.ruleRanking')}</h2>
            <p>{t('dataQuality.dashboard.ruleRankingDesc')}</p>
          </div>
        </div>
        {ranking.length > 0 ? (
          <div className={styles['rule-ranking']}>
            {ranking.map(([rule, count]) => (
              <div key={rule} className={styles['rule-rank-row']}>
                <span title={t(`dataQuality.rule.${rule}`, undefined, rule)}>
                  {t(`dataQuality.rule.${rule}`, undefined, rule)}
                </span>
                <div>
                  <i style={{ width: `${Math.max(4, (count / maxRuleCount) * 100)}%` }} />
                </div>
                <strong>{formatCount(count)}</strong>
              </div>
            ))}
          </div>
        ) : (
          <div className={styles['panel-empty']}>{t('dataQuality.dashboard.noActiveRules')}</div>
        )}
      </section>
    </div>
  );
};

export default QualityOverview;
