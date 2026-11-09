import { type FC, useMemo, useRef, useState } from 'react';
import { InputNumber, Select, Spin, Typography } from 'antd';

import ComButton from '@/components/com-button';
import { Add } from '@/components/lucide-icon/carbon';
import type { FieldItem } from '@/pages/uns/types';
import { useTranslate } from '@/hooks';
import styles from './ValueRangeSettings.module.scss';

export interface ValueRangePatch {
  fieldName: string;
  lowerLimit?: number;
  upperLimit?: number;
}

interface ValueRangeSettingsProps {
  fields: FieldItem[];
  editable: boolean;
  onSave: (patch: ValueRangePatch) => Promise<void>;
}

interface ValueRangeRowProps {
  field: FieldItem;
  editable: boolean;
  onSave: (patch: ValueRangePatch) => Promise<void>;
}

type SaveState = 'idle' | 'saving' | 'saveError';

const numericTypes = new Set(['int', 'integer', 'long', 'bigint', 'float', 'double', 'decimal', 'number']);

const isNumericField = (field: FieldItem) =>
  Boolean(field.name) &&
  !field.systemField &&
  numericTypes.has(
    String(field.type || '')
      .trim()
      .toLowerCase()
  );

const isLimit = (value: unknown): value is number => typeof value === 'number' && Number.isFinite(value) && value !== 0;

const hasAnyLimit = (field: FieldItem) => isLimit(field.lowerLimit) || isLimit(field.upperLimit);

const ValueRangeRow: FC<ValueRangeRowProps> = ({ field, editable, onSave }) => {
  const formatMessage = useTranslate();
  const rowRef = useRef<HTMLDivElement>(null);
  const fieldLabel = field.displayName || field.name;
  const persistedRange = {
    lowerLimit: isLimit(field.lowerLimit) ? field.lowerLimit : undefined,
    upperLimit: isLimit(field.upperLimit) ? field.upperLimit : undefined,
  };
  const [confirmedRange, setConfirmedRange] = useState(persistedRange);
  const [lowerLimit, setLowerLimit] = useState<number | undefined>(persistedRange.lowerLimit);
  const [upperLimit, setUpperLimit] = useState<number | undefined>(persistedRange.upperLimit);
  const [saveState, setSaveState] = useState<SaveState>('idle');
  const effectiveLower = isLimit(lowerLimit) ? lowerLimit : undefined;
  const effectiveUpper = isLimit(upperLimit) ? upperLimit : undefined;
  const invalid = effectiveLower !== undefined && effectiveUpper !== undefined && effectiveLower > effectiveUpper;
  const dirty = lowerLimit !== confirmedRange.lowerLimit || upperLimit !== confirmedRange.upperLimit;

  const saveRow = async () => {
    if (!editable || !dirty || invalid || saveState === 'saving') return;

    const normalizedRange = {
      lowerLimit: isLimit(lowerLimit) ? lowerLimit : undefined,
      upperLimit: isLimit(upperLimit) ? upperLimit : undefined,
    };
    setSaveState('saving');
    try {
      await onSave({ fieldName: field.name, ...normalizedRange });
      setConfirmedRange(normalizedRange);
      setLowerLimit(normalizedRange.lowerLimit);
      setUpperLimit(normalizedRange.upperLimit);
      setSaveState('idle');
    } catch {
      setSaveState('saveError');
    }
  };

  const busy = saveState === 'saving';

  return (
    <div
      ref={rowRef}
      className={styles['table-row']}
      role="row"
      onBlurCapture={(event) => {
        if (event.relatedTarget && rowRef.current?.contains(event.relatedTarget as Node)) return;
        void saveRow();
      }}
    >
      <span className={styles['field-name']} role="cell" title={fieldLabel}>
        {fieldLabel}
      </span>
      <div className={styles['range-input']} role="cell">
        <span className={styles['mobile-label']}>{formatMessage('dataQuality.range.lower')}</span>
        <InputNumber
          value={lowerLimit}
          disabled={!editable || busy}
          status={invalid ? 'error' : undefined}
          placeholder={formatMessage('dataQuality.range.zeroOrEmpty')}
          aria-label={formatMessage('dataQuality.range.lowerForField', { field: fieldLabel })}
          onChange={(value) => {
            setLowerLimit(isLimit(value) ? value : undefined);
            if (saveState === 'saveError') setSaveState('idle');
          }}
          onPressEnter={(event) => event.currentTarget.blur()}
        />
      </div>
      <div className={styles['range-input']} role="cell">
        <span className={styles['mobile-label']}>{formatMessage('dataQuality.range.upper')}</span>
        <InputNumber
          value={upperLimit}
          disabled={!editable || busy}
          status={invalid ? 'error' : undefined}
          placeholder={formatMessage('dataQuality.range.zeroOrEmpty')}
          aria-label={formatMessage('dataQuality.range.upperForField', { field: fieldLabel })}
          onChange={(value) => {
            setUpperLimit(isLimit(value) ? value : undefined);
            if (saveState === 'saveError') setSaveState('idle');
          }}
          onPressEnter={(event) => event.currentTarget.blur()}
        />
      </div>
      <span className={styles.unit} role="cell">
        {field.unit || '-'}
      </span>
      {(invalid || saveState !== 'idle') && (
        <Typography.Text
          type={invalid || saveState === 'saveError' ? 'danger' : 'secondary'}
          className={styles.error}
          role={invalid || saveState === 'saveError' ? 'alert' : 'status'}
        >
          {invalid ? (
            formatMessage('dataQuality.range.invalid')
          ) : busy ? (
            <span className={styles['save-state']}>
              <Spin size="small" />
              {formatMessage('dataQuality.autoSave.saving')}
            </span>
          ) : (
            <span className={styles['save-error']}>
              {formatMessage('dataQuality.range.saveFailed')}
              <ComButton
                size="small"
                type="link"
                onMouseDown={(event) => event.preventDefault()}
                onClick={() => void saveRow()}
              >
                {formatMessage('dataQuality.autoSave.retry')}
              </ComButton>
            </span>
          )}
        </Typography.Text>
      )}
    </div>
  );
};

const ValueRangeSettings: FC<ValueRangeSettingsProps> = ({ fields, editable, onSave }) => {
  const formatMessage = useTranslate();
  const numericFields = useMemo(() => {
    const byName = new Map<string, FieldItem>();
    fields.filter(isNumericField).forEach((field) => {
      if (!byName.has(field.name)) byName.set(field.name, field);
    });
    return Array.from(byName.values());
  }, [fields]);
  const fieldsByName = useMemo(() => new Map(numericFields.map((field) => [field.name, field])), [numericFields]);
  const configuredNames = useMemo(() => numericFields.filter(hasAnyLimit).map((field) => field.name), [numericFields]);
  const [appendedNames, setAppendedNames] = useState<string[]>([]);
  const [adding, setAdding] = useState(false);

  const selectedNames = Array.from(
    new Set([...configuredNames, ...appendedNames.filter((fieldName) => fieldsByName.has(fieldName))])
  );

  const selectedFields = selectedNames.flatMap((fieldName) => {
    const field = fieldsByName.get(fieldName);
    return field ? [field] : [];
  });
  const selectedNameSet = new Set(selectedNames);
  const availableFields = numericFields.filter((field) => !selectedNameSet.has(field.name));
  const selectOptions = availableFields.map((field) => ({
    value: field.name,
    label: field.displayName && field.displayName !== field.name ? `${field.displayName} (${field.name})` : field.name,
  }));

  const hasRows = selectedFields.length > 0 || adding;

  return (
    <div className={styles.panel}>
      {hasRows ? (
        <div className={styles.table} role="table" aria-label={formatMessage('dataQuality.range.current')}>
          <div className={styles['table-header']} role="row">
            <span role="columnheader">{formatMessage('dataQuality.range.field')}</span>
            <span role="columnheader">{formatMessage('dataQuality.range.lower')}</span>
            <span role="columnheader">{formatMessage('dataQuality.range.upper')}</span>
            <span role="columnheader">{formatMessage('uns.unit')}</span>
          </div>
          {selectedFields.map((field) => (
            <ValueRangeRow key={field.name} field={field} editable={editable} onSave={onSave} />
          ))}
          {adding && (
            <div className={styles['table-row']} role="row">
              <Select
                autoFocus
                showSearch
                allowClear
                className={styles['field-select']}
                placeholder={formatMessage('dataQuality.range.selectField')}
                optionFilterProp="label"
                options={selectOptions}
                aria-label={formatMessage('dataQuality.range.selectField')}
                onChange={(fieldName?: string) => {
                  if (!fieldName) {
                    setAdding(false);
                    return;
                  }
                  if (selectedNameSet.has(fieldName) || !fieldsByName.has(fieldName)) return;
                  setAppendedNames((current) => [...current, fieldName]);
                  setAdding(false);
                }}
              />
              <span className={styles.placeholder}>-</span>
              <span className={styles.placeholder}>-</span>
              <span className={styles.placeholder}>-</span>
            </div>
          )}
        </div>
      ) : (
        <div className={styles['empty-row']}>
          <Typography.Text type="secondary">
            {formatMessage(
              numericFields.length === 0 ? 'dataQuality.range.noNumericFields' : 'dataQuality.range.noConfiguredFields'
            )}
          </Typography.Text>
          {editable && availableFields.length > 0 && (
            <ComButton size="small" type="link" icon={<Add size={16} />} onClick={() => setAdding(true)}>
              {formatMessage('dataQuality.range.addField')}
            </ComButton>
          )}
        </div>
      )}

      {editable && hasRows && !adding && availableFields.length > 0 && (
        <ComButton
          className={styles['add-button']}
          size="small"
          type="link"
          icon={<Add size={16} />}
          onClick={() => setAdding(true)}
        >
          {formatMessage('dataQuality.range.addField')}
        </ComButton>
      )}
    </div>
  );
};

export default ValueRangeSettings;
