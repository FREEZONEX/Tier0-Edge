import { Button, Flex, Select } from 'antd';
import ComEllipsis from '@/components/com-ellipsis';
import ComCopy from '@/components/com-copy';
import useTranslate from '@/hooks/useTranslate.ts';
import { useBaseStore } from '@/stores/base';
import SearchSelect from '@/pages/uns/components/use-create-modal/components/SearchSelect.tsx';
import { type CSSProperties, useEffect, useMemo, useRef, useState } from 'react';
import { getInstanceInfo } from '@/apis/core-api';
import { fromPairs, map } from 'lodash-es';
import DatabaseInfoModal, { type ModalRef } from './DatabaseInfoModal.tsx';
import { DataBase } from '@/components/lucide-icon/carbon';
import styles from './index.module.scss';
import HelpTooltip from '../../../../components/help-tooltip';
import useSSE from '@/hooks/useSSE.ts';
import { resolveMqttWebsocketPort, resolveMqttWebsocketUrl } from '@/utils/mqtt';

const Item = ({ item, height = 32, ellipsis = true }: any) => {
  const formatMessage = useTranslate();
  const customStyle: CSSProperties = ellipsis
    ? { whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }
    : {
        overflow: 'auto',
        whiteSpace: 'pre-wrap',
        wordWrap: 'break-word',
        wordBreak: 'break-all',
      };
  return (
    <div key={item.key}>
      <Flex justify="space-between" align="center" style={{ marginBottom: 8 }}>
        <ComEllipsis
          style={{ fontWeight: 400, fontSize: 12, lineHeight: '20px', color: 'var(--ui-description-card-color)' }}
        >
          {formatMessage(item.label)}
        </ComEllipsis>
        {item?.extra && <div style={{ flexShrink: 0, lineHeight: 1 }}>{item?.extra}</div>}
      </Flex>
      <Flex
        title={item.text || formatMessage('uns.selectTopic')}
        align="center"
        justify="space-between"
        gap={6}
        style={item.style}
      >
        <pre
          style={{
            background: 'var(--ui-bg-color)',
            borderRadius: '3px',
            border: '1px solid var(--ui-select-card-color)',
            width: '100%',
            height,
            padding: '4px 12px',
            ...customStyle,
          }}
        >
          {item.text || formatMessage('uns.selectTopic')}
        </pre>
        <ComCopy style={{ height }} bg textToCopy={item.text || formatMessage('uns.selectTopic')} />
      </Flex>
    </div>
  );
};

const formatPanelValue = (value: any) => {
  if (value === undefined || value === null || value === '') {
    return '';
  }
  if (typeof value === 'string') {
    const trimmed = value.trim();
    if (!trimmed) {
      return '';
    }
    try {
      return JSON.stringify(JSON.parse(trimmed), null, 2);
    } catch {
      return trimmed;
    }
  }
  return JSON.stringify(value, null, 2);
};

const pickRealtimePayload = (rawData: any) => {
  if (typeof rawData !== 'string') {
    return rawData;
  }
  const trimmed = rawData.trim();
  if (!trimmed || trimmed === 'Connected') {
    return undefined;
  }
  try {
    const parsed = JSON.parse(trimmed);
    if (parsed?.lastPayload !== undefined) return parsed.lastPayload;
    if (parsed?.payload !== undefined) return parsed.payload;
    if (parsed?.data !== undefined) return parsed.data;
    return parsed;
  } catch {
    return trimmed;
  }
};

const buildSchemaPayload = (data: any, jsonBFallback: string) => {
  if (!data) {
    return '';
  }
  if (data?.schema && Object.keys(data.schema).length > 0) {
    return formatPanelValue(data.schema);
  }
  if (data?.dataType === 8) {
    return jsonBFallback;
  }
  const fieldExampleList = data?.fields?.map((item: any) => ({
    key: item.name,
    value: item.type,
  }));
  return formatPanelValue(fromPairs(map(fieldExampleList, (item) => [item.key, item.value])));
};

const MQTT = () => {
  const formatMessage = useTranslate();
  const systemInfo = useBaseStore((state) => state.systemInfo);
  const mqttUrl = resolveMqttWebsocketUrl(systemInfo);
  const mqttPort = resolveMqttWebsocketPort(mqttUrl);
  const endpointOptions = useMemo(
    () => [
      {
        label: `${mqttUrl.startsWith('wss:') ? 'WSS' : 'WS'} ${mqttPort}`,
        value: 'websocket',
        url: mqttUrl,
      },
    ],
    [mqttPort, mqttUrl]
  );
  const [endpointType, setEndpointType] = useState(endpointOptions[0].value);
  const currentEndpoint = endpointOptions.find((item) => item.value === endpointType) || endpointOptions[0];
  const mqttList = [
    {
      key: 'url',
      label: 'uns.MQTTUrl',
      style: { marginBottom: 8 },
      text: currentEndpoint.url,
    },
  ];
  const [topicInfo, setTopicInfo] = useState<any>(null);
  const modalRef = useRef<ModalRef>(null);
  const [schemaPayload, setSchemaPayload] = useState('');
  const [realtimePayload, setRealtimePayload] = useState('');
  const selectedTopicId = topicInfo?.id;

  useEffect(() => {
    setRealtimePayload('');
  }, [selectedTopicId]);

  useSSE(selectedTopicId ? `/api/core/uns/newMsg?id=${selectedTopicId}` : '', {
    autoConnect: Boolean(selectedTopicId),
    onMessage: (event) => {
      const nextPayload = pickRealtimePayload(event.data);
      const formatted = formatPanelValue(nextPayload);
      if (formatted) {
        setRealtimePayload(formatted);
      }
    },
  });

  return (
    <Flex vertical className={styles['mqtt']}>
      <Flex align="center" gap={8} style={{ marginBottom: 5 }}>
        <DataBase size={24} />
        <ComEllipsis style={{ fontWeight: 600 }}>{formatMessage('uns.mqttAccess')}</ComEllipsis>
        <HelpTooltip title={formatMessage('uns.mqttDescription')} />
      </Flex>
      <Flex style={{ flex: 1, overflow: 'hidden' }} vertical>
        {mqttList?.map((item: any) => {
          return <Item item={item} key={item.key} />;
        })}
        <ComEllipsis
          style={{
            fontWeight: 400,
            fontSize: 12,
            lineHeight: '20px',
            color: 'var(--ui-description-card-color)',
            margin: '8px 0',
          }}
        >
          {formatMessage('uns.MQTTPort')}
        </ComEllipsis>
        <Select
          value={endpointType}
          options={endpointOptions.map((item) => ({
            label: item.label,
            value: item.value,
          }))}
          style={{
            width: '100%',
            marginBottom: 8,
          }}
          onChange={setEndpointType}
        />
        <ComEllipsis
          style={{
            fontWeight: 400,
            fontSize: 12,
            lineHeight: '20px',
            color: 'var(--ui-description-card-color)',
            margin: '8px 0',
          }}
        >
          {formatMessage('uns.topic')}
        </ComEllipsis>
        <SearchSelect
          apiParams={{
            type: 2,
          }}
          style={{
            width: '100%',
            marginBottom: 8,
          }}
          placeholder={formatMessage('common.select')}
          onChange={(e) => {
            if (e?.value) {
              getInstanceInfo({ id: e?.value })
                .then((data) => {
                  setTopicInfo(data);
                  setSchemaPayload(buildSchemaPayload(data, formatMessage('uns.jsonBExample')));
                })
                .catch(() => {
                  setTopicInfo(null);
                  setSchemaPayload('');
                });
            } else {
              setTopicInfo(null);
              setSchemaPayload('');
            }
          }}
          labelInValue
        />
        <Item
          height={125}
          ellipsis={false}
          item={{
            key: 'payload',
            label: 'uns.payload',
            text: realtimePayload || schemaPayload,
            extra: topicInfo?.persistence ? (
              <Button
                // type="link"
                title={formatMessage('uns.databaseInfo')}
                size="small"
                style={{ height: 20 }}
                onClick={() => modalRef.current?.onOpen(topicInfo)}
              >
                <DataBase />
              </Button>
            ) : null,
          }}
        />
      </Flex>
      <DatabaseInfoModal ref={modalRef} />
    </Flex>
  );
};

export default MQTT;
