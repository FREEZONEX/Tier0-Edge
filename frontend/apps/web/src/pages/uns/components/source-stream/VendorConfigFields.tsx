import type { FC } from 'react';
import { Input, InputNumber, Space } from 'antd';
import HelpTooltip from '@/components/help-tooltip';
import { useTranslate } from '@/hooks';
import type { JsonObject, SourceTypeDefinition } from '@/apis/core-api/source-stream';
import { vendorParameterKeys } from './source-types';
import styles from './index.module.scss';

const VendorConfigFields: FC<{
  profile: SourceTypeDefinition;
  value: JsonObject;
  readOnly: boolean;
  onChange: (parameters: JsonObject) => void;
}> = ({ profile, value, readOnly, onChange }) => {
  const t = useTranslate();
  return (
    <div className={styles['full-width']}>
      <Space>
        <h4>{t(`SourceStream.vendorName.${profile.id}`)}</h4>
        <HelpTooltip title={t(`SourceStream.vendorHint.${profile.id}`)} />
      </Space>
      <div className={styles.editor}>
        {vendorParameterKeys(profile).map((key) => (
          <label key={key} className={styles.field}>
            <span>{t(`SourceStream.vendorParam.${key}`, undefined, key)}</span>
            {typeof value[key] === 'number' ? (
              <InputNumber
                disabled={readOnly}
                value={value[key] as number}
                onChange={(next) => next !== null && onChange({ ...value, [key]: next })}
              />
            ) : (
              <Input
                readOnly={readOnly}
                value={typeof value[key] === 'string' ? (value[key] as string) : JSON.stringify(value[key] ?? '')}
                onChange={(event) => onChange({ ...value, [key]: event.target.value })}
              />
            )}
          </label>
        ))}
      </div>
      <HelpTooltip title={t('SourceStream.vendorParametersHint')} />
    </div>
  );
};

export default VendorConfigFields;
