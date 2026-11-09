import { useEffect, useRef, useState } from 'react';
import { Alert, Button, Select, Space } from 'antd';
import { useTranslate } from '@/hooks';
import { usbHidApi, type HIDInputDevice } from '@/apis/core-api/usb-hid';
import styles from './index.module.scss';

interface Props {
  endpoint: string;
  creating: boolean;
  disabled: boolean;
  onConfirm: (device: HIDInputDevice) => void;
  onValidityChange: (valid: boolean) => void;
}

export default function HIDDevicePicker({ endpoint, creating, disabled, onConfirm, onValidityChange }: Props) {
  const t = useTranslate();
  const [devices, setDevices] = useState<HIDInputDevice[]>([]);
  const [selected, setSelected] = useState(endpoint);
  const [confirmed, setConfirmed] = useState(creating ? '' : endpoint);
  const [scan, setScan] = useState<'idle' | 'waiting' | 'timeout' | 'error' | 'received'>('idle');
  const [sample, setSample] = useState('');
  const [listState, setListState] = useState<'loading' | 'ready' | 'error'>('loading');
  const [refresh, setRefresh] = useState(0);
  const preview = useRef<AbortController>();
  const confirm = useRef(onConfirm);
  useEffect(() => {
    confirm.current = onConfirm;
  }, [onConfirm]);
  useEffect(() => {
    const request = new AbortController();
    usbHidApi
      .devices(request.signal)
      .then(({ devices }) => {
        if (!request.signal.aborted) {
          setDevices(devices || []);
          setListState('ready');
        }
      })
      .catch(() => {
        if (!request.signal.aborted) setListState('error');
      });
    return () => request.abort();
  }, [refresh]);
  useEffect(() => () => preview.current?.abort(), []);
  const device = devices.find((item) => item.endpoint === selected || (selected && item.legacyEndpoint === selected));
  const valid = Boolean(selected && confirmed === selected && (!creating || (listState === 'ready' && device)) && scan !== 'waiting');
  useEffect(() => onValidityChange(valid), [valid, onValidityChange]);

  function select(value: string) {
    preview.current?.abort();
    setSelected(value);
    setScan('idle');
    setSample('');
  }
  async function testDevice() {
    if (!device) return;
    preview.current?.abort();
    const request = new AbortController();
    preview.current = request;
    setScan('waiting');
    setConfirmed('');
    setSample('');
    try {
      const result = await usbHidApi.preview(device.endpoint, request.signal);
      if (request.signal.aborted) return;
      if (!result.received || !result.text) {
        setScan('timeout');
        return;
      }
      setSample(result.text);
      setScan('received');
      setConfirmed(selected);
      confirm.current(device.legacyEndpoint && endpoint === device.legacyEndpoint ? { ...device, endpoint } : device);
    } catch {
      if (!request.signal.aborted) setScan('error');
    }
  }
  return (
    <div className={styles['full-width']}>
      <Space direction="vertical" style={{ width: '100%' }}>
        <span>{t('SourceStream.hid.device')}</span>
        <Space.Compact style={{ width: '100%' }}>
          <Select
            aria-label={t('SourceStream.hid.device')}
            style={{ flex: 1 }}
            value={device?.endpoint}
            placeholder={t(selected && !creating ? 'SourceStream.hid.configuredMissing' : 'SourceStream.hid.choose')}
            loading={listState === 'loading'}
            disabled={disabled || listState !== 'ready'}
            options={devices.map((item) => ({ value: item.endpoint, label: item.label }))}
            onChange={select}
          />
          <Button
            disabled={disabled || listState === 'loading'}
            onClick={() => {
              select(selected);
              setListState('loading');
              setRefresh((value) => value + 1);
            }}
          >
            {t('SourceStream.hid.refresh')}
          </Button>
        </Space.Compact>
        {listState === 'error' && <Alert type="error" showIcon message={t('SourceStream.hid.listFailed')} />}
        {listState === 'ready' && !devices.length && (
          <Alert type="info" showIcon message={t('SourceStream.hid.empty')} />
        )}
        <Button
          loading={scan === 'waiting'}
          disabled={!device || disabled || listState !== 'ready'}
          onClick={() => void testDevice()}
        >
          {t('SourceStream.hid.preview')}
        </Button>
        {scan !== 'idle' && (
          <div role="status">
            <Alert
              showIcon
              type={scan === 'received' ? 'success' : scan === 'error' ? 'error' : 'info'}
              message={t('SourceStream.hid.' + scan)}
              description={scan === 'received' ? <code style={{ overflowWrap: 'anywhere' }}>{sample}</code> : undefined}
            />
          </div>
        )}
      </Space>
    </div>
  );
}
