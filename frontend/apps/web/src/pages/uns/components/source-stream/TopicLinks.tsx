import { useEffect, useState, type FC } from 'react';
import { metadataApi } from '@/apis/core-api/uns-metadata';
import ComButton from '@/components/com-button';
import { useTranslate } from '@/hooks';
import { useIntegrationRoute } from './IntegrationTabs';
import { TopicLink } from './resource-links';
import styles from './index.module.scss';

const TopicLinks: FC<{ ids: number[]; metadataId?: string }> = ({ ids, metadataId }) => {
  const { go } = useIntegrationRoute();
  const t = useTranslate();
  const [metadataName, setMetadataName] = useState('');
  const [metadataFailed, setMetadataFailed] = useState(false);
  useEffect(() => {
    let alive = true;
    setMetadataName('');
    setMetadataFailed(false);
    if (metadataId) {
      metadataApi
        .view(metadataId)
        .then(({ meta }) => {
          if (alive) setMetadataName(meta.data.name);
        })
        .catch(() => {
          if (alive) setMetadataFailed(true);
        });
    }
    return () => {
      alive = false;
    };
  }, [metadataId]);
  return (
    <div className={styles['topic-links']}>
      {metadataId &&
        (metadataFailed ? (
          <span>
            {t('SourceStream.referenceUnavailable')} · {metadataId}
          </span>
        ) : (
          <ComButton type="link" onClick={() => go({ view: 'metadata', metaId: metadataId, detail: 'data' })}>
            {metadataName || metadataId}
          </ComButton>
        ))}
      {[...new Set(ids)].map((id) => (
        <TopicLink key={id} id={id} />
      ))}
    </div>
  );
};
export default TopicLinks;
