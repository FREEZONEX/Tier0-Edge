import { useState } from 'react';
import { Button, DatePicker, Input, InputNumber, Select, Space, Tag } from 'antd';
import dayjs from 'dayjs';
import { useTranslate } from '@/hooks';
import type { AuditFilterOption } from '@/apis/core-api/audit-log';
import { getAuditActionLabel, getAuditModuleLabel } from './presentation';
import { type AuditFilters, filterKeys } from './filters';
import styles from './index.module.scss';

type Props = {
  value: AuditFilters;
  applied: AuditFilters;
  options: AuditFilterOption[];
  onChange: (value: AuditFilters) => void;
  onApply: (value: AuditFilters) => void;
  onReset: () => void;
};

export const AuditFilterBar = ({ value, applied, options, onChange, onApply, onReset }: Props) => {
  const t = useTranslate();
  const [expanded, setExpanded] = useState(false);
  const update = (key: keyof AuditFilters, next: string | number | undefined) => onChange({ ...value, [key]: next });
  const modules = [...new Set(options.map((item) => item.resType))].filter(Boolean);
  const actions = [
    ...new Set(
      options.filter((item) => !value.resType || item.resType === value.resType).map((item) => item.businessType)
    ),
  ].filter(Boolean);
  const tags = filterKeys.filter((key) => applied[key] !== undefined && applied[key] !== '');
  const tagValue = (key: (typeof filterKeys)[number]) => {
    const raw = String(applied[key]);
    if (key === 'resType') return getAuditModuleLabel(undefined, undefined, raw, t);
    if (key === 'businessType') return getAuditActionLabel(raw, t);
    if (key === 'result') return t(`auditLog.result.${raw}`);
    if (key === 'scopeType') return t(`auditLog.filter.scope${raw}`);
    if (key === 'startTime' || key === 'endTime') return dayjs(raw).format('YYYY-MM-DD HH:mm');
    return raw;
  };
  const label = (key: string) => t(`auditLog.filter.${key}`);
  const input = (key: 'resourceKeyword' | 'scopeName' | 'requestId' | 'ip' | 'resId') => (
    <label className={styles['filter-field']} key={key}>
      <span>{label(key)}</span>
      <Input
        aria-label={label(key)}
        allowClear
        value={value[key] || ''}
        onChange={(e) => update(key, e.target.value)}
        onPressEnter={() => onApply(value)}
      />
    </label>
  );
  return (
    <div className={styles['filters']}>
      <div className={styles['filter-grid']}>
        <label className={`${styles['filter-field']} ${styles['filter-time']}`}>
          <span>{label('time')}</span>
          <DatePicker.RangePicker
            aria-label={label('time')}
            showTime
            allowEmpty={[true, true]}
            value={[value.startTime ? dayjs(value.startTime) : null, value.endTime ? dayjs(value.endTime) : null]}
            format="YYYY-MM-DD HH:mm:ss"
            placeholder={[label('startTime'), label('endTime')]}
            presets={[
              { label: label('today'), value: () => [dayjs().startOf('day'), dayjs()] },
              { label: label('last24h'), value: () => [dayjs().subtract(24, 'hour'), dayjs()] },
              { label: label('last7d'), value: () => [dayjs().subtract(7, 'day'), dayjs()] },
              { label: label('last30d'), value: () => [dayjs().subtract(30, 'day'), dayjs()] },
            ]}
            onChange={(dates) =>
              onChange({ ...value, startTime: dates?.[0]?.toISOString(), endTime: dates?.[1]?.toISOString() })
            }
          />
        </label>
        <label className={styles['filter-field']}>
          <span>{label('operatorKeyword')}</span>
          <Input
            aria-label={label('operatorKeyword')}
            placeholder={t('auditLog.operatorPlaceholder')}
            allowClear
            value={value.operatorKeyword || ''}
            onChange={(e) => update('operatorKeyword', e.target.value)}
            onPressEnter={() => onApply(value)}
          />
        </label>
        <label className={styles['filter-field']}>
          <span>{label('resType')}</span>
          <Select
            aria-label={label('resType')}
            allowClear
            showSearch
            optionFilterProp="label"
            value={value.resType}
            placeholder={t('auditLog.all')}
            options={modules.map((item) => ({
              value: item,
              label: getAuditModuleLabel(undefined, undefined, item, t),
            }))}
            onChange={(resType) => onChange({ ...value, resType, businessType: undefined })}
          />
        </label>
        <label className={styles['filter-field']}>
          <span>{label('businessType')}</span>
          <Select
            aria-label={label('businessType')}
            allowClear
            showSearch
            optionFilterProp="label"
            value={value.businessType}
            placeholder={t('auditLog.all')}
            options={actions.map((item) => ({ value: item, label: getAuditActionLabel(item, t) }))}
            onChange={(next) => update('businessType', next)}
          />
        </label>
        <label className={styles['filter-field']}>
          <span>{label('result')}</span>
          <Select
            aria-label={label('result')}
            allowClear
            value={value.result}
            placeholder={t('auditLog.all')}
            options={['success', 'failed', 'accepted'].map((item) => ({
              value: item,
              label: t(`auditLog.result.${item}`),
            }))}
            onChange={(next) => update('result', next)}
          />
        </label>
      </div>
      {expanded && (
        <div className={styles['filter-grid']}>
          {input('resourceKeyword')}
          <label className={styles['filter-field']}>
            <span>{label('scopeType')}</span>
            <Select
              aria-label={label('scopeType')}
              allowClear
              value={value.scopeType}
              placeholder={t('auditLog.all')}
              options={[-1, 1, 2].map((item) => ({ value: item, label: t(`auditLog.filter.scope${item}`) }))}
              onChange={(next) => update('scopeType', next)}
            />
          </label>
          {input('scopeName')}
          <label className={styles['filter-field']}>
            <span>{label('operatorId')}</span>
            <InputNumber
              aria-label={label('operatorId')}
              min={1}
              precision={0}
              value={value.operatorId}
              onChange={(next) => update('operatorId', next ?? undefined)}
            />
          </label>
          {input('requestId')}
          {input('ip')}
        </div>
      )}
      <div className={styles['filter-actions']}>
        <Space wrap>
          <Button type="primary" onClick={() => onApply(value)}>
            {t('auditLog.query')}
          </Button>
          <Button onClick={onReset}>{t('auditLog.reset')}</Button>
          <Button type="link" aria-expanded={expanded} onClick={() => setExpanded(!expanded)}>
            {t(expanded ? 'auditLog.lessFilters' : 'auditLog.moreFilters')}
          </Button>
        </Space>
        <Space wrap size={[0, 6]} className={styles['filter-tags']}>
          {tags.map((key) => (
            <Tag
              key={key}
              closable
              onClose={(e) => {
                e.preventDefault();
                onApply({ ...applied, [key]: undefined });
              }}
            >
              {label(key)}: {tagValue(key)}
            </Tag>
          ))}
          {tags.length > 0 && (
            <Button type="link" size="small" onClick={() => onApply({})}>
              {t('auditLog.clearFilters')}
            </Button>
          )}
        </Space>
      </div>
    </div>
  );
};
