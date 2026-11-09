import { type FC } from 'react';
import { DatePicker, Input, Select, Space, Tag } from 'antd';
import dayjs from 'dayjs';
import ComButton from '@/components/com-button';
import { useTranslate } from '@/hooks';
import { EVENT_LOGIN, EVENT_LOGOUT, LOGIN_FAILED, LOGIN_SUCCESS, type LoginLogFilters } from '../filters';
import styles from '../index.module.scss';

type Props = {
  value: LoginLogFilters;
  applied: LoginLogFilters;
  onChange: (value: LoginLogFilters) => void;
  onApply: (value: LoginLogFilters) => void;
};

const LoginLogFilterBar: FC<Props> = ({ value, applied, onChange, onApply }) => {
  const t = useTranslate();
  const rangeLabel = t('auditLog.filter.time', {}, 'Time range');
  const keywordLabel = `${t('loginLog.account', {}, 'Account')} / ${t('loginLog.ip', {}, 'IP Address')}`;
  const tags = [
    { key: 'keyword', label: keywordLabel, value: applied.keyword },
    {
      key: 'eventType',
      label: t('loginLog.event', {}, 'Event'),
      value: applied.eventType
        ? applied.eventType === EVENT_LOGOUT
          ? t('loginLog.eventLogout', {}, 'Logout')
          : t('loginLog.eventLogin', {}, 'Login')
        : '',
    },
    {
      key: 'code',
      label: t('loginLog.status', {}, 'Status'),
      value:
        applied.code === undefined
          ? ''
          : applied.code === LOGIN_SUCCESS
            ? t('loginLog.statusSuccess', {}, 'Success')
            : t('loginLog.statusFailed', {}, 'Failed'),
    },
    {
      key: 'range',
      label: rangeLabel,
      value:
        applied.startTime || applied.endTime
          ? [
              applied.startTime
                ? dayjs(Number(applied.startTime)).format('YYYY-MM-DD HH:mm')
                : t('auditLog.filter.startTime', {}, 'Start time'),
              applied.endTime
                ? dayjs(Number(applied.endTime)).format('YYYY-MM-DD HH:mm')
                : t('auditLog.filter.endTime', {}, 'End time'),
            ].join(' – ')
          : '',
    },
  ];
  const removeFilter = (key: string) => {
    const next = { ...applied };
    if (key === 'range') {
      delete next.startTime;
      delete next.endTime;
    } else {
      delete next[key as keyof LoginLogFilters];
    }
    onApply(next);
  };

  return (
    <div className={styles.filters}>
      <div className={styles['filter-grid']}>
        <label className={`${styles['filter-field']} ${styles['filter-keyword']}`}>
          <span>{keywordLabel}</span>
          <Input
            aria-label={keywordLabel}
            placeholder={t('loginLog.searchPlaceholder', {}, 'Search account / IP')}
            allowClear
            value={value.keyword || ''}
            onChange={(event) => onChange({ ...value, keyword: event.target.value })}
            onPressEnter={() => onApply(value)}
          />
        </label>
        <label className={styles['filter-field']}>
          <span>{t('loginLog.event', {}, 'Event')}</span>
          <Select
            aria-label={t('loginLog.event', {}, 'Event')}
            allowClear
            value={value.eventType}
            placeholder={t('loginLog.allEvents', {}, 'All Events')}
            options={[
              { value: EVENT_LOGIN, label: t('loginLog.eventLogin', {}, 'Login') },
              { value: EVENT_LOGOUT, label: t('loginLog.eventLogout', {}, 'Logout') },
            ]}
            onChange={(eventType) => onChange({ ...value, eventType })}
          />
        </label>
        <label className={styles['filter-field']}>
          <span>{t('loginLog.status', {}, 'Status')}</span>
          <Select
            aria-label={t('loginLog.status', {}, 'Status')}
            allowClear
            value={value.code}
            placeholder={t('loginLog.allStatus', {}, 'All Status')}
            options={[
              { value: LOGIN_SUCCESS, label: t('loginLog.statusSuccess', {}, 'Success') },
              { value: LOGIN_FAILED, label: t('loginLog.statusFailed', {}, 'Failed') },
            ]}
            onChange={(code) => onChange({ ...value, code })}
          />
        </label>
        <div className={`${styles['filter-field']} ${styles['filter-time']}`}>
          <span>{rangeLabel}</span>
          <DatePicker.RangePicker
            aria-label={rangeLabel}
            allowEmpty={[true, true]}
            showTime={{ format: 'HH:mm' }}
            format="YYYY-MM-DD HH:mm"
            placeholder={[
              t('auditLog.filter.startTime', {}, 'Start time'),
              t('auditLog.filter.endTime', {}, 'End time'),
            ]}
            value={[
              value.startTime ? dayjs(Number(value.startTime)) : null,
              value.endTime ? dayjs(Number(value.endTime)) : null,
            ]}
            presets={[
              { label: t('auditLog.filter.today', {}, 'Today'), value: () => [dayjs().startOf('day'), dayjs()] },
              {
                label: t('auditLog.filter.last24h', {}, 'Last 24 hours'),
                value: () => [dayjs().subtract(24, 'hour'), dayjs()],
              },
              {
                label: t('auditLog.filter.last7d', {}, 'Last 7 days'),
                value: () => [dayjs().subtract(7, 'day'), dayjs()],
              },
              {
                label: t('auditLog.filter.last30d', {}, 'Last 30 days'),
                value: () => [dayjs().subtract(30, 'day'), dayjs()],
              },
            ]}
            onChange={(dates) =>
              onChange({
                ...value,
                startTime: dates?.[0] ? String(dates[0].startOf('minute').valueOf()) : undefined,
                endTime: dates?.[1] ? String(dates[1].endOf('minute').valueOf()) : undefined,
              })
            }
          />
        </div>
      </div>
      <div className={styles['filter-actions']}>
        <Space>
          <ComButton type="primary" onClick={() => onApply(value)}>
            {t('common.search', {}, 'Search')}
          </ComButton>
          <ComButton onClick={() => onApply({})}>{t('common.reset', {}, 'Reset')}</ComButton>
        </Space>
        <div className={styles['filter-tags']}>
          {tags
            .filter((tag) => tag.value)
            .map((tag) => (
              <Tag
                key={tag.key}
                closable
                onClose={(event) => {
                  event.preventDefault();
                  removeFilter(tag.key);
                }}
              >
                {tag.label}: {tag.value}
              </Tag>
            ))}
        </div>
      </div>
    </div>
  );
};

export default LoginLogFilterBar;
