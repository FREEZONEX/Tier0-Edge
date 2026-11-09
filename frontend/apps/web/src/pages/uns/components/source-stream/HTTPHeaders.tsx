import { useEffect, useRef, useState } from 'react';
import { Alert, Input, Space } from 'antd';
import { Add, TrashCan } from '@carbon/icons-react';
import ComButton from '@/components/com-button';
import HelpTooltip from '@/components/help-tooltip';
import { useTranslate } from '@/hooks';

const reserved = new Set([
  'authorization',
  'proxy-authorization',
  'cookie',
  'set-cookie',
  'x-api-key',
  'api-key',
  'apikey',
  'x-auth-token',
  'x-access-token',
  'host',
  'content-length',
  'connection',
  'transfer-encoding',
  'te',
  'trailer',
  'upgrade',
  'proxy-connection',
  'keep-alive',
]);
type Row = { id: number; name: string; value: string };
export default function HTTPHeaders({
  value,
  readOnly,
  onChange,
  onValidityChange,
}: {
  value: Record<string, string>;
  readOnly: boolean;
  onChange: (value: Record<string, string>) => void;
  onValidityChange: (valid: boolean) => void;
}) {
  const t = useTranslate();
  const serial = JSON.stringify(value);
  const published = useRef(serial);
  const nextId = useRef(0);
  const [rows, setRows] = useState<Row[]>(() =>
    Object.entries(value).map(([name, value]) => ({ id: nextId.current++, name, value }))
  );
  const [invalid, setInvalid] = useState(false);
  useEffect(() => {
    if (published.current === serial) return;
    published.current = serial;
    setRows(
      Object.entries(JSON.parse(serial) as Record<string, string>).map(([name, value]) => ({
        id: nextId.current++,
        name,
        value,
      }))
    );
    setInvalid(false);
    onValidityChange(true);
  }, [serial, onValidityChange]);
  useEffect(() => () => onValidityChange(true), [onValidityChange]);
  const change = (next: Row[]) => {
    if (readOnly) return;
    setRows(next);
    const names = next.map((row) => row.name.toLowerCase());
    const bad =
      new Set(names).size !== names.length ||
      next.length > 64 ||
      next.some(
        (row) =>
          !/^[!#$%&'*+.^_`|~0-9A-Za-z-]+$/.test(row.name) ||
          row.name.length > 128 ||
          reserved.has(row.name.toLowerCase()) ||
          !row.value.trim() ||
          new TextEncoder().encode(row.value).length > 4096 ||
          /\p{Cc}/u.test(row.value) ||
          row.value.includes('-----BEGIN')
      );
    setInvalid(bad);
    onValidityChange(!bad);
    if (!bad) {
      const result = Object.fromEntries(next.map((row) => [row.name, row.value]));
      published.current = JSON.stringify(result);
      onChange(result);
    }
  };
  return (
    <Space direction="vertical" style={{ width: '100%' }}>
      <Space>
        <strong>{t('SourceStream.httpHeaders')}</strong>
        <HelpTooltip title={t('SourceStream.httpHeadersHint')} />
        <ComButton
          icon={<Add />}
          aria-label={t('common.add')}
          disabled={readOnly || rows.length >= 64}
          onClick={() => change([...rows, { id: nextId.current++, name: '', value: '' }])}
        />
      </Space>
      {rows.map((row, index) => (
        <div key={row.id} style={{ display: 'flex', gap: 8 }}>
          <Input
            aria-label={`${t('SourceStream.parameterName')} ${index + 1}`}
            value={row.name}
            maxLength={128}
            readOnly={readOnly}
            placeholder="Accept"
            onChange={(event) =>
              change(rows.map((item) => (item.id === row.id ? { ...item, name: event.target.value } : item)))
            }
          />
          <Input
            aria-label={`${t('SourceStream.parameterValue')} ${index + 1}`}
            value={row.value}
            maxLength={4096}
            readOnly={readOnly}
            placeholder="application/json"
            onChange={(event) =>
              change(rows.map((item) => (item.id === row.id ? { ...item, value: event.target.value } : item)))
            }
          />
          <ComButton
            icon={<TrashCan />}
            aria-label={t('common.delete')}
            disabled={readOnly}
            onClick={() => change(rows.filter((item) => item.id !== row.id))}
          />
        </div>
      ))}
      {invalid && <Alert type="error" showIcon message={t('SourceStream.httpHeadersInvalid')} />}
    </Space>
  );
}
