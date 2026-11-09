import { type FC } from 'react';
import { Input, InputNumber, Select } from 'antd';
import { Add, TrashCan } from '@carbon/icons-react';
import { v4 as uuid } from 'uuid';
import ComButton from '@/components/com-button';
import HelpTooltip from '@/components/help-tooltip';
import { useTranslate } from '@/hooks';
import type { StreamCollection } from '@/apis/core-api/source-stream';
import styles from './index.module.scss';

type Ranges = NonNullable<StreamCollection['modbus']>['ranges'];
const SourceRegisterRanges: FC<{ value: Ranges; readOnly: boolean; onChange: (value: Ranges) => void }> = ({
  value,
  readOnly,
  onChange,
}) => {
  const t = useTranslate();
  return (
    <div className={styles.section}>
      <strong>{t('SourceStream.registerRanges')}</strong>
      {value.map((range, index) => {
        const update = (patch: Partial<typeof range>) =>
          onChange(value.map((item, i) => (i === index ? { ...item, ...patch } : item)));
        return (
          <div className={styles['register-range']} key={index}>
            <label className={styles.field}>
              <span>{t('SourceStream.resourceId')}</span>
              <Input value={range.id} readOnly={readOnly} onChange={(e) => update({ id: e.target.value })} />
            </label>
            <label className={styles.field}>
              <span>
                {t('SourceStream.unitId')} <HelpTooltip title={t('SourceStream.rangeUnitHint')} />
              </span>
              <InputNumber
                value={range.unitId}
                disabled={readOnly}
                min={1}
                max={247}
                onChange={(unitId) => update({ unitId: unitId ?? undefined })}
              />
            </label>
            <label className={styles.field}>
              <span>{t('SourceStream.rangeAccess')}</span>
              <Select
                value={range.writable === undefined ? 'default' : range.writable ? 'writable' : 'read-only'}
                disabled={readOnly}
                options={['default', 'read-only', 'writable'].map((value) => ({
                  value,
                  label: t(`SourceStream.rangeAccess.${value}`),
                }))}
                onChange={(value) => update({ writable: value === 'default' ? undefined : value === 'writable' })}
              />
            </label>
            <label className={styles.field}>
              <span>{t('SourceStream.registerTable')}</span>
              <Select
                aria-label={t('SourceStream.registerTable')}
                value={range.table}
                disabled={readOnly}
                options={['holding-register', 'input-register', 'coil', 'discrete-input'].map((value) => ({ value }))}
                onChange={(table) => update({ table })}
              />
            </label>
            <label className={styles.field}>
              <span>{t('SourceStream.address')}</span>
              <InputNumber
                value={range.address}
                disabled={readOnly}
                min={0}
                max={65535}
                onChange={(value) => value !== null && update({ address: value })}
              />
            </label>
            <label className={styles.field}>
              <span>{t('SourceStream.quantity')}</span>
              <InputNumber
                value={range.quantity}
                disabled={readOnly}
                min={1}
                max={range.table.includes('register') ? 125 : 2000}
                onChange={(value) => value !== null && update({ quantity: value })}
              />
            </label>
            {!readOnly && (
              <ComButton
                aria-label={t('SourceStream.remove')}
                icon={<TrashCan />}
                onClick={() => onChange(value.filter((_, i) => i !== index))}
              />
            )}
          </div>
        );
      })}
      {!readOnly && (
        <ComButton
          className={styles['catalog-back']}
          icon={<Add />}
          onClick={() =>
            onChange([...value, { id: uuid().slice(0, 8), table: 'holding-register', address: 0, quantity: 1 }])
          }
        >
          {t('SourceStream.addRange')}
        </ComButton>
      )}
    </div>
  );
};

export default SourceRegisterRanges;
