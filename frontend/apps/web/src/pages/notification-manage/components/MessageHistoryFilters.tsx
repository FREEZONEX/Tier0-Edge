import { type FC, useState } from 'react';
import { DatePicker, Form, Popover, Select, Space, Tag, Tooltip } from 'antd';
import { Filter, Renew } from '@carbon/icons-react';
import dayjs from 'dayjs';
import ComButton from '@/components/com-button';
import ProSearch from '@/components/pro-search';
import { useTranslate } from '@/hooks';
import type { MessageHistoryListParams, MessageHistorySource, NotificationRecipientUser } from '@/apis/core-api/notify';
import styles from './MessageHistoryTable.module.scss';

type Props = {
  value: MessageHistoryListParams;
  sources: MessageHistorySource[];
  users: NotificationRecipientUser[];
  loading: boolean;
  onApply: (value: MessageHistoryListParams) => void;
  onRefresh: () => void;
};

const MessageHistoryFilters: FC<Props> = ({ value, sources, users, loading, onApply, onRefresh }) => {
  const t = useTranslate('notificationManage');
  const common = useTranslate();
  const [open, setOpen] = useState(false);
  const [keyword, setKeyword] = useState(value.keyword || '');
  const [draft, setDraft] = useState(value);
  const apply = (next: MessageHistoryListParams) => {
    const cleaned = { ...next, keyword: next.keyword?.trim() || undefined };
    setKeyword(cleaned.keyword || '');
    setDraft(cleaned);
    onApply(cleaned);
  };
  const sourceOptions = sources
    .filter((source) => !value.sourceType || source.sourceType === value.sourceType)
    .map((source) => ({
      value: JSON.stringify([source.sourceType, source.sourceId]),
      label:
        source.sourceType === 'platform'
          ? t('sourcePlatform')
          : source.sourceName
            ? `${source.sourceName} (${source.sourceId})`
            : source.sourceId,
    }));
  const userOptions = users.map((user) => ({
    value: user.userId,
    label:
      user.displayName && user.userName && user.displayName !== user.userName
        ? `${user.displayName} (${user.userName})`
        : user.displayName || user.userName || String(user.userId),
  }));
  const typeOptions = ['inbox', 'banner', 'news'].map((type) => ({
    value: type,
    label: t(`type${type.charAt(0).toUpperCase()}${type.slice(1)}`),
  }));
  const statusOptions = [
    { value: 'sent', label: t('statusSent') },
    { value: 'revoked', label: t('statusRevoked') },
  ];
  const tags = [
    { key: 'keyword', label: t('filterKeyword'), text: value.keyword },
    {
      key: 'sourceType',
      label: t('historySource'),
      text: value.sourceType ? t(value.sourceType === 'app' ? 'sourceApp' : 'sourcePlatform') : undefined,
    },
    {
      key: 'sourceId',
      label: t('filterSource'),
      text: value.sourceId
        ? sourceOptions.find((option) => option.value === JSON.stringify([value.sourceType, value.sourceId]))?.label ||
          value.sourceId
        : undefined,
    },
    { key: 'type', label: t('colType'), text: typeOptions.find((option) => option.value === value.type)?.label },
    {
      key: 'status',
      label: t('colStatus'),
      text: statusOptions.find((option) => option.value === value.status)?.label,
    },
    {
      key: 'recipientUserId',
      label: t('filterRecipient'),
      text: value.recipientUserId
        ? userOptions.find((option) => option.value === value.recipientUserId)?.label || String(value.recipientUserId)
        : undefined,
    },
    {
      key: 'timeRange',
      label: t('filterTime'),
      text:
        value.startTime || value.endTime
          ? `${value.startTime ? dayjs(value.startTime).format('YYYY-MM-DD HH:mm:ss') : '…'} – ${value.endTime ? dayjs(value.endTime).format('YYYY-MM-DD HH:mm:ss') : '…'}`
          : undefined,
    },
  ].filter((tag) => tag.text);
  const advancedCount = [
    value.sourceId,
    value.type,
    value.status,
    value.recipientUserId,
    value.startTime || value.endTime,
  ].filter(Boolean).length;
  const removeFilter = (key: string) => {
    const next = { ...value };
    if (key === 'timeRange') {
      delete next.startTime;
      delete next.endTime;
    } else if (key === 'sourceType') {
      delete next.sourceType;
      delete next.sourceId;
    } else {
      delete next[key as keyof MessageHistoryListParams];
    }
    apply(next);
  };
  const panel = (
    <Form layout="vertical" className={styles['filter-panel']}>
      <div className={styles['filter-grid']}>
        <Form.Item label={t('filterSource')}>
          <Select
            aria-label={t('filterSource')}
            allowClear
            showSearch
            optionFilterProp="label"
            placeholder={t('filterAll')}
            options={sourceOptions}
            value={draft.sourceId ? JSON.stringify([draft.sourceType, draft.sourceId]) : undefined}
            onChange={(key?: string) => {
              const source = key ? (JSON.parse(key) as [MessageHistorySource['sourceType'], string]) : undefined;
              setDraft({ ...draft, sourceType: source?.[0] || value.sourceType, sourceId: source?.[1] });
            }}
          />
        </Form.Item>
        <Form.Item label={t('colStatus')}>
          <Select
            aria-label={t('colStatus')}
            allowClear
            placeholder={t('filterAll')}
            options={statusOptions}
            value={draft.status}
            onChange={(status) => setDraft({ ...draft, status })}
          />
        </Form.Item>
        <Form.Item label={t('colType')}>
          <Select
            aria-label={t('colType')}
            allowClear
            placeholder={t('filterAll')}
            options={typeOptions}
            value={draft.type}
            onChange={(type) => setDraft({ ...draft, type })}
          />
        </Form.Item>
        <Form.Item label={t('filterRecipient')}>
          <Select
            aria-label={t('filterRecipient')}
            allowClear
            showSearch
            optionFilterProp="label"
            placeholder={t('filterAll')}
            options={userOptions}
            value={draft.recipientUserId}
            onChange={(recipientUserId) => setDraft({ ...draft, recipientUserId })}
          />
        </Form.Item>
        <Form.Item label={t('filterTime')} className={styles['time-filter']}>
          <DatePicker.RangePicker
            aria-label={t('filterTime')}
            showTime
            allowEmpty={[true, true]}
            format="YYYY-MM-DD HH:mm:ss"
            value={[draft.startTime ? dayjs(draft.startTime) : null, draft.endTime ? dayjs(draft.endTime) : null]}
            onChange={(range) =>
              setDraft({ ...draft, startTime: range?.[0]?.valueOf(), endTime: range?.[1]?.valueOf() })
            }
          />
        </Form.Item>
      </div>
      <Space className={styles['filter-actions']}>
        <ComButton
          onClick={() => {
            apply({});
            setOpen(false);
          }}
        >
          {t('filterReset')}
        </ComButton>
        <ComButton
          type="primary"
          onClick={() => {
            apply({ ...draft, keyword });
            setOpen(false);
          }}
        >
          {t('filterSearch')}
        </ComButton>
      </Space>
    </Form>
  );

  return (
    <div className={styles['history-filters']}>
      <div className={styles.toolbar}>
        <div className={styles['toolbar-filters']}>
          <Popover
            trigger="click"
            placement="bottomLeft"
            open={open}
            onOpenChange={(next) => {
              if (next) setDraft(value);
              setOpen(next);
            }}
            content={panel}
          >
            <ComButton
              icon={<Filter size={16} />}
              aria-expanded={open}
              className={advancedCount ? styles['filter-active'] : undefined}
            >
              {common('messageCenter.filters')}
              {advancedCount ? ` (${advancedCount})` : ''}
            </ComButton>
          </Popover>
          <ProSearch
            size="sm"
            className={styles['keyword-filter']}
            aria-label={t('filterKeyword')}
            placeholder={t('filterKeywordHint')}
            closeButtonLabelText={t('filterReset')}
            value={keyword}
            onChange={(event) => setKeyword(event.target.value)}
            onSearch={(next) => apply({ ...value, keyword: next })}
          />
          <Select
            className={styles['source-filter']}
            aria-label={t('historySource')}
            allowClear
            placeholder={t('historySource')}
            value={value.sourceType}
            options={[
              { value: 'platform', label: t('sourcePlatform') },
              { value: 'app', label: t('sourceApp') },
            ]}
            onChange={(sourceType) => apply({ ...value, keyword, sourceType, sourceId: undefined })}
          />
        </div>
        <Tooltip title={common('common.refresh')}>
          <ComButton
            aria-label={common('common.refresh')}
            icon={<Renew size={16} />}
            loading={loading}
            onClick={onRefresh}
          />
        </Tooltip>
      </div>
      {tags.length > 0 && (
        <div className={styles['filter-tags']}>
          {tags.map((tag) => (
            <Tag
              key={tag.key}
              closable
              onClose={(event) => {
                event.preventDefault();
                removeFilter(tag.key);
              }}
            >
              <span className={styles['filter-tag-label']} title={`${tag.label}: ${tag.text}`}>
                {tag.label}: {tag.text}
              </span>
            </Tag>
          ))}
          <ComButton type="link" size="small" onClick={() => apply({})}>
            {t('filterReset')}
          </ComButton>
        </div>
      )}
    </div>
  );
};

export default MessageHistoryFilters;
