import { Collapse, Descriptions, Empty, Tabs, Tag } from 'antd';
import type { StreamTestResult } from '@/apis/core-api/source-stream';
import { useTranslate } from '@/hooks';
import { TopicLink } from './resource-links';
import styles from './index.module.scss';

export default function StreamTestResultView({ result, input }: { result: StreamTestResult; input: unknown }) {
  const t = useTranslate();
  const targets = result.targets ?? result.result.outputs ?? [];
  const logs = result.result.logs ?? [];
  const json = (value: unknown) => <pre className={styles.code}>{JSON.stringify(value ?? null, null, 2)}</pre>;
  return (
    <section className={styles['test-result']} aria-label={t('SourceStream.testResult')}>
      <div className={styles.row} role="status">
        <strong>{t(result.result.drop ? 'SourceStream.messageDropped' : 'SourceStream.testCompleted')}</strong>
        <span className={styles.muted}>{result.result.durationMs} ms</span>
      </div>
      <Tag>{t('SourceStream.previewOnly')}</Tag>
      <Tabs
        defaultActiveKey="result"
        items={[
          {
            key: 'input',
            label: t('SourceStream.input'),
            children: (
              <>
                {result.input && (
                  <Descriptions
                    size="small"
                    column={1}
                    items={[
                      { key: 'resource', label: t('SourceStream.resourceId'), children: result.input.resourceRef },
                      {
                        key: 'receivedAt',
                        label: t('SourceStream.receivedAt'),
                        children: new Date(result.input.receivedAt).toLocaleString(),
                      },
                    ]}
                  />
                )}
                {json(result.input ? result.input.data : input)}
              </>
            ),
          },
          { key: 'result', label: t('SourceStream.processedResult'), children: json(result.result.data) },
          ...(result.result.checkpoint
            ? [
                {
                  key: 'checkpoint',
                  label: t('SourceStream.checkpointPreview'),
                  children: (
                    <>
                      <Tag>{t('SourceStream.checkpointBuffered')}</Tag>
                      {json(result.result.checkpoint)}
                    </>
                  ),
                },
              ]
            : []),
          {
            key: 'targets',
            label: `${t('SourceStream.outputTopics')} · ${targets.length}`,
            children: targets.length ? (
              <div className={styles.section}>
                {targets.map((target, index) => (
                  <div key={`${target.topicId || ('collectionId' in target && target.collectionId)}-${index}`}>
                    <TopicLink id={target.topicId || ('collectionId' in target && target.collectionId) || ''} />
                    {json(target.data)}
                  </div>
                ))}
              </div>
            ) : (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={t('SourceStream.noTestOutputs')} />
            ),
          },
        ]}
      />
      <Collapse
        ghost
        size="small"
        items={[
          ...(logs.length
            ? [{ key: 'logs', label: `${t('SourceStream.logs')} · ${logs.length}`, children: json(logs) }]
            : []),
          { key: 'raw', label: t('SourceStream.rawResponse'), children: json(result) },
        ]}
      />
    </section>
  );
}
