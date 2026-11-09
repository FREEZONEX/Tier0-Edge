import { type FC, type ReactNode } from 'react';
import { Descriptions } from 'antd';
import { useTranslate } from '@/hooks';
import type { SourceDocument } from '@/apis/core-api/source-stream';
import styles from './index.module.scss';
import { sourceTypeLabelKey } from './source-types';

const SourceConfigReview: FC<{ document: SourceDocument; authentication?: ReactNode }> = ({
  document,
  authentication,
}) => {
  const t = useTranslate();
  return (
    <div className={styles.section}>
      <Descriptions
        bordered
        column={1}
        size="small"
        items={[
          { key: 'name', label: t('SourceStream.name'), children: document.name },
          { key: 'type', label: t('SourceStream.protocol'), children: t(sourceTypeLabelKey(document)) },
          { key: 'endpoint', label: t('SourceStream.endpoint'), children: document.endpoint },
          ...(authentication
            ? [{ key: 'authentication', label: t('SourceStream.authentication'), children: authentication }]
            : []),
          ...(document.description
            ? [{ key: 'description', label: t('SourceStream.description'), children: document.description }]
            : []),
        ]}
      />
    </div>
  );
};

export default SourceConfigReview;
