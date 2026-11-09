import { ArrowRight, DataQualityDefinition, WarningAltFilled } from '@carbon/icons-react';
import type { FC } from 'react';

import type { DataQualityNodeStatus } from '@/apis/core-api/data-quality';
import ComButton from '@/components/com-button';
import { formatTimestamp } from '@/utils/format';

import { formatPercent, nodeHealthState, nodeIngestionSuccess, nodeLabel, type Translate } from '../dashboard-utils';
import styles from '../index.module.scss';

interface NodeProfileProps {
  node?: DataQualityNodeStatus;
  onOpenUns: (node: DataQualityNodeStatus) => void;
  t: Translate;
}

const NodeProfile: FC<NodeProfileProps> = ({ node, onOpenUns, t }) => {
  if (!node) {
    return (
      <section className={`${styles.panel} ${styles['profile-panel']}`}>
        <div className={styles['panel-heading']}>
          <h2>{t('dataQuality.dashboard.nodeProfile')}</h2>
        </div>
        <div className={styles['panel-empty']}>{t('dataQuality.dashboard.selectNode')}</div>
      </section>
    );
  }

  const health = nodeHealthState(node);
  const ingestionSuccess = nodeIngestionSuccess(node);

  return (
    <section className={`${styles.panel} ${styles['profile-panel']}`}>
      <div className={styles['panel-heading']}>
        <h2>{t('dataQuality.dashboard.nodeProfile')}</h2>
      </div>

      <div className={styles['profile-title']}>
        <span className={styles[`profile-icon-${health}`]}>
          {health === 'healthy' ? <DataQualityDefinition size={24} /> : <WarningAltFilled size={24} />}
        </span>
        <div>
          <strong>{nodeLabel(node)}</strong>
          <span title={node.namespace || undefined}>{node.namespace || `#${node.unsId}`}</span>
        </div>
      </div>

      <div className={styles['profile-score-row']}>
        <div>
          <span>{t('dataQuality.dashboard.nodeIngestionSuccess')}</span>
          <strong>{ingestionSuccess == null ? '-' : formatPercent(ingestionSuccess)}</strong>
        </div>
        <div>
          <span>{t('dataQuality.dashboard.unrecoveredRules')}</span>
          <strong>{node.openRules?.length || 0}</strong>
        </div>
      </div>
      <div className={styles['profile-progress']} aria-hidden="true">
        <i style={{ width: `${ingestionSuccess || 0}%` }} />
      </div>

      <dl className={styles['profile-details']}>
        <div>
          <dt>{t('dataQuality.lastIngested')}</dt>
          <dd>{formatTimestamp(node.lastIngestedAt) || '-'}</dd>
        </div>
        <div>
          <dt>{t('dataQuality.collectionState')}</dt>
          <dd>{t(`dataQuality.collectionState.${node.collectionState}`)}</dd>
        </div>
        <div>
          <dt>{t('dataQuality.deepState')}</dt>
          <dd>{t(`dataQuality.deepState.${node.deepState}`)}</dd>
        </div>
      </dl>

      <ComButton type="primary" block onClick={() => onOpenUns(node)}>
        {t('dataQuality.dashboard.openUnsNode')}
        <ArrowRight size={16} />
      </ComButton>
    </section>
  );
};

export default NodeProfile;
