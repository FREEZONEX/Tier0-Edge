import { useSourceStreamApi } from '@/apis/core-api/source-stream-scope';
import { useState } from 'react';
import { Alert, Empty, Input, Pagination, Spin } from 'antd';
import { Add, Checkmark, Plug, Search } from '@carbon/icons-react';
import { useDebounce, useRequest } from 'ahooks';
import { type Source } from '@/apis/core-api/source-stream';
import ComButton from '@/components/com-button';
import ProModal from '@/components/pro-modal';
import { useTranslate } from '@/hooks';
import styles from './index.module.scss';
import { sourceTypeLabelKey } from './source-types';
import { ButtonPermission } from '@/common-types/button-permission';

export default function SourcePicker({
  onSelect,
  onClose,
  selectedIds = [],
  disabledReason,
  onCreate,
}: {
  onSelect: (source: Source) => void;
  onClose: () => void;
  selectedIds?: number[];
  disabledReason?: (source: Source) => string | undefined;
  onCreate?: () => void;
}) {
  const sourceStreamApi = useSourceStreamApi();
  const t = useTranslate();
  const [search, setSearch] = useState('');
  const [page, setPage] = useState(1);
  const query = useDebounce(search.trim(), { wait: 250 });
  const { data, loading, error, refresh } = useRequest(
    () => sourceStreamApi.sources({ page, size: 20, search: query }),
    { refreshDeps: [page, query] }
  );
  const pending = loading || query !== search.trim();
  const protocols = [...new Set((data?.list ?? []).map((source) => source.protocol))];
  return (
    <ProModal open title={t('SourceStream.chooseSource')} onCancel={onClose} footer={null} width={760}>
      <div className={styles.section}>
        <Input
          autoFocus
          allowClear
          size="large"
          prefix={<Search />}
          value={search}
          placeholder={t('SourceStream.search')}
          aria-label={t('SourceStream.search')}
          onChange={(event) => {
            setSearch(event.target.value);
            setPage(1);
          }}
        />
        {error && (
          <Alert
            type="error"
            message={t('SourceStream.loadFailed')}
            action={<ComButton onClick={refresh}>{t('SourceStream.refresh')}</ComButton>}
          />
        )}
        <Spin spinning={pending}>
          <div className={styles['source-picker-results']} aria-busy={pending}>
            {!error &&
              protocols.map((protocol) => (
                <section key={protocol} className={styles['source-picker-group']}>
                  <h4>{protocol.toUpperCase()}</h4>
                  {data?.list
                    .filter((source) => source.protocol === protocol)
                    .map((source) => {
                      const selected = selectedIds.includes(source.id);
                      const reason = source.document ? disabledReason?.(source) : t('SourceStream.forbidden');
                      return (
                        <button
                          key={source.id}
                          type="button"
                          className={styles['source-choice']}
                          disabled={pending || selected || !!reason}
                          onClick={() => onSelect(source)}
                        >
                          <span className={styles['source-choice-icon']}>
                            <Plug size={20} />
                          </span>
                          <span className={styles['list-copy']}>
                            <strong>{source.name}</strong>
                            <span>{t(sourceTypeLabelKey(source.document ?? source))}</span>
                            <span className={styles['source-endpoint']}>{source.document?.endpoint}</span>
                            {reason && <span className={styles.muted}>{reason}</span>}
                          </span>
                          <span className={styles['source-choice-status']}>
                            {t(selected ? 'SourceStream.alreadyAdded' : `SourceStream.health.${source.health}`)}
                          </span>
                          {selected ? <Checkmark /> : <Add />}
                        </button>
                      );
                    })}
                </section>
              ))}
            {!pending && !error && !data?.list.length && <Empty description={t('SourceStream.noObjects')} />}
          </div>
        </Spin>
        <Pagination
          current={page}
          pageSize={20}
          total={data?.total ?? 0}
          onChange={setPage}
          hideOnSinglePage
          showSizeChanger={false}
          disabled={pending}
        />
        {onCreate && (
          <ComButton auth={ButtonPermission['uns.sourceManage']} icon={<Add />} onClick={onCreate}>
            {t('SourceStream.addSource')}
          </ComButton>
        )}
      </div>
    </ProModal>
  );
}
