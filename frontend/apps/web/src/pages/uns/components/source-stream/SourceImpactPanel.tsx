import { useSourceStreamApi } from '@/apis/core-api/source-stream-scope';
import { useEffect, useState, type FC } from 'react';
import { Alert, Empty, Spin } from 'antd';
import { type SourceImpact } from '@/apis/core-api/source-stream';
import { useTranslate } from '@/hooks';
import ComButton from '@/components/com-button';
import TopicLinks from './TopicLinks';
import { useIntegrationRoute } from './IntegrationTabs';
import styles from './index.module.scss';

const SourceImpactPanel: FC<{ sourceId: number }> = ({ sourceId }) => {
  const sourceStreamApi = useSourceStreamApi();
  const t = useTranslate();
  const { go } = useIntegrationRoute();
  const [data, setData] = useState<SourceImpact>();
  const [failed, setFailed] = useState(false);
  useEffect(() => {
    let alive = true;
    sourceStreamApi
      .impact(sourceId)
      .then((result) => {
        if (alive) setData(result);
      })
      .catch(() => {
        if (alive) setFailed(true);
      });
    return () => {
      alive = false;
    };
  }, [sourceId]);
  return (
    <Spin spinning={!data && !failed}>
      <section className={styles.card}>
        <h3>{t('SourceStream.impact')}</h3>
        {failed ? (
          <Alert type="error" message={t('SourceStream.loadFailed')} />
        ) : (
          data && (
            <>
              <p>{t('SourceStream.impactHint', { count: data.count })}</p>
              {!data.count && <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={t('SourceStream.noObjects')} />}
              {data.streams.map((stream) => (
                <div className={styles['impact-row']} key={stream.id}>
                  <ComButton type="link" onClick={() => go({ view: 'stream', sourceId, streamId: stream.id })}>
                    {stream.name}
                  </ComButton>
                  <TopicLinks
                    ids={data.topics.filter((item) => item.streamId === stream.id).map((item) => item.topicId)}
                  />
                </div>
              ))}
            </>
          )
        )}
      </section>
    </Spin>
  );
};
export default SourceImpactPanel;
