import { type FC } from 'react';
import { Input, InputNumber, Select } from 'antd';
import { useTranslate } from '@/hooks';
import type { Source, StreamDocument, StreamCollection, Protocol } from '@/apis/core-api/source-stream';
import SourceRegisterRanges from './SourceRegisterRanges';
import StreamResourceBrowser from './StreamResourceBrowser';
import styles from './index.module.scss';

export function initialCollection(protocol: Protocol): StreamCollection {
  switch (protocol) {
    case 'usb-hid':
      return {};
    case 'http-api':
    case 'oauth2':
    case 'edge-sql':
    case 'file-read':
    case 'open-protocol':
      return {}; // HTTP API Sources are used by independent script Streams.
    case 'mqtt':
      return { mqtt: { qos: 1 } };
    case 'opcua':
      return { opcua: { samplingIntervalMs: 1000 } };
    case 'modbus-tcp':
      return { modbus: { pollIntervalMs: 1000, ranges: [] } };
  }
}
const StreamCollectionEditor: FC<{
  source: Source;
  document: StreamDocument;
  readOnly: boolean;
  onChange: (patch: Partial<StreamDocument>) => void;
}> = ({ source, document: doc, readOnly, onChange }) => {
  const t = useTranslate();
  // Show legacy Source scope without changing the saved document. An explicit
  // editor change moves only that collection configuration into the Stream.
  const connection = source.document;
  const collection =
    doc.collection ??
    (source.protocol === 'mqtt'
      ? {
          mqtt: {
            qos: connection.mqtt?.qos ?? 1,
            requestTopic: connection.mqtt?.requestTopic,
            replyTopic: connection.mqtt?.replyTopic,
            replyTopicField: connection.mqtt?.replyTopicField,
            correlationField: connection.mqtt?.correlationField,
          },
        }
      : source.protocol === 'opcua'
        ? {
            opcua: { samplingIntervalMs: connection.opcua?.samplingIntervalMs ?? 1000 },
          }
        : source.protocol === 'modbus-tcp'
          ? {
              modbus: {
                pollIntervalMs: connection.modbus?.pollIntervalMs ?? 1000,
                ranges: connection.modbus?.ranges ?? [],
              },
            }
          : initialCollection(source.protocol));
  const update = (patch: Partial<StreamCollection>) => onChange({ collection: { ...collection, ...patch } });
  const refs = doc.nativeResourceRefs?.length
    ? doc.nativeResourceRefs
    : doc.nativeResourceRef
      ? [doc.nativeResourceRef]
      : [];
  const options = (source.resources || [])
    .filter((r) => r.models.includes(doc.model))
    .map((r) => ({ value: r.id, label: r.id }));
  return (
    <div className={styles.editor}>
      {doc.model !== 'request-reply' || source.protocol !== 'mqtt' ? (
        <label className={styles.field}>
          <span>
            {source.protocol === 'mqtt'
              ? t('SourceStream.topicFilters')
              : source.protocol === 'opcua'
                ? t('SourceStream.nodeIds')
                : t('SourceStream.registerRanges')}
          </span>
          <Select
            mode="tags"
            value={refs}
            disabled={readOnly}
            options={options}
            placeholder={
              source.protocol === 'mqtt'
                ? 'mqtt:factory/line1/#'
                : source.protocol === 'opcua'
                  ? 'opcua:ns=2;s=Temperature'
                  : 'modbus:temperature'
            }
            onChange={(values) => {
              const selected = doc.model === 'publish' ? values : values.slice(-1);
              onChange(
                doc.model === 'publish'
                  ? { nativeResourceRef: '', nativeResourceRefs: selected, collection }
                  : { nativeResourceRef: selected[0] || '', nativeResourceRefs: undefined, collection }
              );
            }}
          />
          {!readOnly && source.protocol === 'opcua' && (
            <StreamResourceBrowser
              key={source.id}
              source={source}
              model={doc.model}
              onSelect={(id) =>
                onChange(
                  doc.model === 'publish'
                    ? { nativeResourceRef: '', nativeResourceRefs: [...new Set([...refs, id])], collection }
                    : { nativeResourceRef: id, nativeResourceRefs: undefined, collection }
                )
              }
            />
          )}
        </label>
      ) : null}
      {collection.mqtt && (
        <>
          <label className={styles.field}>
            <span>{t('SourceStream.qos')}</span>
            <Select
              value={collection.mqtt.qos}
              disabled={readOnly}
              options={[0, 1, 2].map((value) => ({ value, label: 'QoS ' + value }))}
              onChange={(qos) => update({ mqtt: { ...collection.mqtt!, qos } })}
            />
          </label>
          {doc.model === 'request-reply' &&
            (['requestTopic', 'replyTopic', 'replyTopicField', 'correlationField'] as const).map((key) => (
              <label key={key} className={styles.field}>
                <span>{t('SourceStream.' + key)}</span>
                <Input
                  value={collection.mqtt?.[key] || ''}
                  readOnly={readOnly}
                  onChange={(event) =>
                    onChange({
                      nativeResourceRef: 'mqtt-request-reply',
                      collection: { mqtt: { ...collection.mqtt!, [key]: event.target.value } },
                    })
                  }
                />
              </label>
            ))}
        </>
      )}
      {collection.opcua && (
        <label className={styles.field}>
          <span>{t('SourceStream.samplingInterval')}</span>
          <InputNumber
            min={100}
            max={3600000}
            addonAfter="ms"
            value={collection.opcua.samplingIntervalMs}
            disabled={readOnly}
            onChange={(value) => value !== null && update({ opcua: { samplingIntervalMs: value } })}
          />
        </label>
      )}
      {collection.modbus && (
        <>
          <label className={styles.field}>
            <span>{t('SourceStream.pollInterval')}</span>
            <InputNumber
              min={100}
              max={3600000}
              addonAfter="ms"
              value={collection.modbus.pollIntervalMs}
              disabled={readOnly}
              onChange={(value) =>
                value !== null && update({ modbus: { ...collection.modbus!, pollIntervalMs: value } })
              }
            />
          </label>
          <div className={styles['full-width']}>
            <SourceRegisterRanges
              value={collection.modbus.ranges}
              readOnly={readOnly}
              onChange={(ranges) => update({ modbus: { ...collection.modbus!, ranges } })}
            />
          </div>
        </>
      )}
    </div>
  );
};
export default StreamCollectionEditor;
