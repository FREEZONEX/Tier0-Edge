import { useEffect, useState, type FC } from 'react';
import { Alert, Button, Input, Segmented, Space } from 'antd';
import { json } from '@codemirror/lang-json';
import ProCodemirror from '@/components/pro-codemirror';
import HelpTooltip from '@/components/help-tooltip';
import { useTranslate } from '@/hooks';
import type { JsonObject } from '@/apis/core-api/source-stream';
import styles from './index.module.scss';

const HTTPParameters: FC<{
  value: JsonObject;
  onChange: (value: JsonObject) => void;
  onValidityChange?: (valid: boolean) => void;
  readOnly: boolean;
}> = ({ value, onChange, onValidityChange, readOnly }) => {
  const t = useTranslate();
  const [mode, setMode] = useState('json');
  const serialized = JSON.stringify(value, null, 2);
  const [raw, setRaw] = useState(serialized);
  const [valid, setValid] = useState(true);
  useEffect(() => {
    setRaw(serialized);
    setValid(true);
    onValidityChange?.(true);
  }, [serialized]);
  useEffect(() => () => onValidityChange?.(true), [onValidityChange]);
  const update = (text: string) => {
    setRaw(text);
    try {
      const next: unknown = JSON.parse(text);
      if (!next || typeof next !== 'object' || Array.isArray(next)) throw new Error('object required');
      setValid(true);
      onValidityChange?.(true);
      onChange(next as JsonObject);
    } catch {
      setValid(false);
      onValidityChange?.(false);
    }
  };
  return (
    <div className={styles.section}>
      <Space>
        <Segmented
          value={mode}
          onChange={(next) => setMode(String(next))}
          options={[
            { value: 'json', label: t('SourceStream.jsonConfig') },
            { value: 'kv', label: t('SourceStream.parameterPairs'), disabled: !valid },
          ]}
        />
        <HelpTooltip title={t('SourceStream.parametersHint')} />
      </Space>
      {!valid && <Alert type="error" showIcon message={t('SourceStream.invalidParameters')} />}
      {mode === 'json' ? (
        <ProCodemirror
          value={raw}
          onChange={update}
          readOnly={readOnly}
          extensions={[json()]}
          height="140px"
          showHint={false}
        />
      ) : (
        <>
          {Object.entries(value).map(([key, item]) => (
            <div key={key} className={styles.editor}>
              <Input aria-label={t('SourceStream.parameterName')} value={key} readOnly />
              <Input
                aria-label={t('SourceStream.parameterValue')}
                value={JSON.stringify(item)}
                readOnly={readOnly}
                onChange={(e) => {
                  let next: unknown = e.target.value;
                  try {
                    next = JSON.parse(e.target.value);
                  } catch {
                    /* Plain text is a string parameter. */
                  }
                  onChange({ ...value, [key]: next });
                }}
              />
              <Button
                disabled={readOnly}
                onClick={() => {
                  const next = { ...value };
                  delete next[key];
                  onChange(next);
                }}
              >
                {t('common.delete')}
              </Button>
            </div>
          ))}
          <Button
            disabled={readOnly}
            onClick={() => {
              let key = 'parameter';
              let index = 1;
              while (key in value) key = `parameter${index++}`;
              onChange({ ...value, [key]: '' });
              setMode('json');
            }}
          >
            {t('SourceStream.addParameter')}
          </Button>
        </>
      )}
    </div>
  );
};
export default HTTPParameters;
