import { CheckmarkFilled, InformationFilled, WarningAltFilled } from '@carbon/icons-react';
import type { FC } from 'react';

import type { DataQualityNodeStatus } from '@/apis/core-api/data-quality';
import { formatTimestamp } from '@/utils/format';

import { formatCount, nodeHealthState, nodeLabel, type NodeHealthState, type Translate } from '../dashboard-utils';
import styles from '../index.module.scss';

interface NodeHealthStripProps {
  items: DataQualityNodeStatus[];
  selectedUnsId?: number;
  onSelect: (unsId: number) => void;
  t: Translate;
}

const stateIcon = (state: NodeHealthState) => {
  if (state === 'healthy') return <CheckmarkFilled size={18} />;
  if (state === 'warming') return <InformationFilled size={18} />;
  return <WarningAltFilled size={18} />;
};

const NodeHealthStrip: FC<NodeHealthStripProps> = ({ items, selectedUnsId, onSelect, t }) => (
  <section className={styles.panel}>
    <div className={styles['panel-heading']}>
      <div>
        <h2>{t('dataQuality.dashboard.nodeHealth')}</h2>
        <p>{t('dataQuality.dashboard.nodeHealthDesc')}</p>
      </div>
      <div className={styles.legend}>
        {(['healthy', 'warning', 'critical', 'warming'] as NodeHealthState[]).map((state) => (
          <span key={state} className={styles[`legend-${state}`]}>
            <i />
            {t(`dataQuality.dashboard.health.${state}`)}
          </span>
        ))}
      </div>
    </div>

    {items.length > 0 ? (
      <div className={styles['node-strip']}>
        {items.map((node) => {
          const state = nodeHealthState(node);
          return (
            <button
              key={node.unsId}
              type="button"
              className={`${styles['node-card']} ${styles[`node-${state}`]}`}
              aria-pressed={node.unsId === selectedUnsId}
              onClick={() => onSelect(node.unsId)}
            >
              <span className={styles['node-card-top']}>
                <strong title={nodeLabel(node)}>{nodeLabel(node)}</strong>
                <i className={styles['node-state-icon']}>{stateIcon(state)}</i>
              </span>
              <span className={styles['node-namespace']} title={node.namespace || undefined}>
                {node.namespace || `#${node.unsId}`}
              </span>
              <span className={styles['node-card-meta']}>
                <span>
                  {t('dataQuality.dashboard.receivedShort')}
                  <strong>{formatCount(node.receivedCount)}</strong>
                </span>
                <span>
                  {t('dataQuality.dashboard.lastIngestedShort')}
                  <strong>{formatTimestamp(node.lastIngestedAt) || '-'}</strong>
                </span>
              </span>
            </button>
          );
        })}
      </div>
    ) : (
      <div className={styles['panel-empty']}>{t('dataQuality.dashboard.noMonitoredNodes')}</div>
    )}
  </section>
);

export default NodeHealthStrip;
