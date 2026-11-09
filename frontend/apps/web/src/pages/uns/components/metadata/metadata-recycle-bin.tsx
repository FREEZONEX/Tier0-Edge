import { useCallback, useEffect, useRef, useState } from 'react';
import { Alert, App, Space } from 'antd';
import { Renew, TrashCan, Undo } from '@carbon/icons-react';
import { v4 as uuid } from 'uuid';
import ComButton from '@/components/com-button';
import ProTable from '@/components/pro-table';
import { openConfirmModal } from '@/components/confirm-modal';
import { useTranslate } from '@/hooks';
import { loadMetadata, metadataApi, type MetaKind, type UnsMetadata } from '@/apis/core-api/uns-metadata';
import { displayTime } from './data-panel';
import { purgeJobState, trashBatches, trashRoots } from './metadata-trash-model';
import styles from './index.module.scss';

type PurgeTask = {
  kind: MetaKind;
  ids: string[];
  key?: string;
  jobId?: string;
  state: 'pending' | 'unknown' | 'failed' | 'succeeded';
  summary?: { succeeded: number; total: number };
};

export default function MetadataRecycleBin({
  identity,
  canRestore,
  canDelete,
  onChanged,
}: {
  identity: string;
  canRestore: boolean;
  canDelete: boolean;
  onChanged: () => void;
}) {
  const t = useTranslate();
  const { modal, message } = App.useApp();
  const [items, setItems] = useState<UnsMetadata[]>([]);
  const [selected, setSelected] = useState<React.Key[]>([]);
  const [loading, setLoading] = useState(true);
  const [failed, setFailed] = useState(false);
  const [working, setWorking] = useState(false);
  const [revision, setRevision] = useState(0);
  const cacheKey = `tier0.namespace-purge:${identity}`;
  const [tasks, setTasks] = useState<PurgeTask[]>(() => {
    try {
      const saved: unknown = JSON.parse(sessionStorage.getItem(cacheKey) || '[]');
      if (!Array.isArray(saved)) return [];
      return saved
        .filter(
          (task) =>
            task &&
            ['folder', 'collection', 'topic'].includes(task.kind) &&
            Array.isArray(task.ids) &&
            task.ids.every((id: unknown) => typeof id === 'string') &&
            typeof task.key === 'string' &&
            ['pending', 'unknown', 'failed'].includes(task.state)
        )
        .map((task) => ({ ...task, state: task.jobId ? 'pending' : 'unknown' }));
    } catch {
      return [];
    }
  });
  const [queryFailed, setQueryFailed] = useState(false);
  const [queryRevision, setQueryRevision] = useState(0);
  const changed = useRef(onChanged);
  useEffect(() => {
    changed.current = onChanged;
  }, [onChanged]);
  const reload = useCallback(() => setRevision((value) => value + 1), []);
  useEffect(() => {
    try {
      sessionStorage.setItem(cacheKey, JSON.stringify(tasks.filter((task) => task.key && task.state !== 'succeeded')));
    } catch {
      /* 受限存储不影响当前页面继续查询和重试。 */
    }
  }, [cacheKey, tasks]);

  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    loadMetadata(true, controller.signal)
      .then((result) => {
        if (controller.signal.aborted) return;
        setItems(result);
        setSelected((keys) => keys.filter((key) => result.some((item) => item.id === key && !item.data.purgeJobId)));
        setFailed(false);
        setTasks((current) => {
          const next = [...current];
          for (const item of result) {
            const jobId = item.data.purgeJobId;
            if (!jobId || next.some((task) => task.jobId === jobId)) continue;
            const uncertain = next.findIndex((task) => !task.jobId && task.ids.includes(item.id));
            if (uncertain >= 0) next[uncertain] = { ...next[uncertain], jobId, state: 'pending' };
            else next.push({ kind: item.kind, ids: [item.id], jobId, state: 'pending' });
          }
          return next;
        });
      })
      .catch(() => {
        if (!controller.signal.aborted) setFailed(true);
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [revision]);

  const pending = tasks
    .filter((task) => task.state === 'pending' && task.jobId)
    .map((task) => task.jobId!)
    .join(',');
  useEffect(() => {
    if (!pending) return;
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout>;
    const query = async () => {
      try {
        const ids = pending.split(',');
        const jobs = await metadataApi.purgeJobs(ids, controller.signal);
        if (controller.signal.aborted) return;
        if (ids.some((id) => !jobs.some((job) => job.jobId === id))) throw new Error('Missing purge job');
        setQueryFailed(false);
        setTasks((current) =>
          current.map((task) => {
            const job = jobs.find((item) => item.jobId === task.jobId);
            return job ? { ...task, state: purgeJobState(job), summary: job.result?.summary } : task;
          })
        );
        if (jobs.some((job) => purgeJobState(job) !== 'pending')) {
          reload();
          changed.current();
        }
        if (jobs.some((job) => purgeJobState(job) === 'pending')) timer = setTimeout(query, 1000);
      } catch {
        if (!controller.signal.aborted) setQueryFailed(true);
      }
    };
    void query();
    return () => {
      controller.abort();
      clearTimeout(timer);
    };
  }, [pending, queryRevision, reload]);

  const submitPurge = async (task: PurgeTask) => {
    if (!task.key) return;
    const update = (patch: Partial<PurgeTask>) =>
      setTasks((current) => current.map((item) => (item.key === task.key ? { ...item, ...patch } : item)));
    update({ state: 'pending' });
    try {
      const result = await metadataApi.purge(task.kind, task.ids, task.key);
      if (result.status !== 'accepted' || !result.jobId) throw new Error('Missing purge job');
      update({ jobId: result.jobId, state: 'pending' });
    } catch {
      // 未确认受理时保留同一请求标识，重试不能扩展删除范围。
      update({ state: 'unknown' });
    } finally {
      reload();
    }
  };
  const purge = (targets: UnsMetadata[]) =>
    openConfirmModal(modal, {
      title: t('UnsTrash.delete'),
      content: t('UnsTrash.deleteHint', { count: targets.length }),
      okText: t('UnsTrash.delete'),
      cancelText: t('common.cancel'),
      danger: true,
      onOk: async () => {
        if (!canDelete || working) return;
        setWorking(true);
        try {
          for (const batch of trashBatches(targets, items, true)) {
            const task: PurgeTask = {
              kind: batch.kind,
              ids: batch.items.map((item) => item.id),
              key: uuid(),
              state: 'pending',
            };
            setTasks((current) => [...current, task]);
            await submitPurge(task);
          }
          setSelected([]);
        } finally {
          setWorking(false);
        }
      },
    });
  const restore = (targets: UnsMetadata[]) =>
    openConfirmModal(modal, {
      title: t('UnsMeta.restore'),
      content: t('UnsMeta.restoreHint'),
      okText: t('UnsMeta.restore'),
      cancelText: t('common.cancel'),
      onOk: async () => {
        if (!canRestore || working) return;
        setWorking(true);
        try {
          for (const batch of trashBatches(targets, items)) await metadataApi.restore(batch.kind, batch.items);
          setSelected([]);
          message.success(t('UnsTrash.restored'));
        } finally {
          setWorking(false);
          reload();
          changed.current();
        }
      },
    });
  const roots = trashRoots(items);
  const available = roots.filter(
    (item) =>
      !item.mountId &&
      !item.data.purgeJobId &&
      !tasks.some((task) => task.state !== 'succeeded' && task.ids.includes(item.id))
  );
  const selection = available.filter((item) => selected.includes(item.id));
  const busy = loading || working || failed;
  const refresh = () => {
    reload();
    setTasks((current) =>
      current.map((task) => (task.jobId && task.state !== 'succeeded' ? { ...task, state: 'pending' } : task))
    );
    setQueryRevision((value) => value + 1);
  };
  return (
    <div className={styles.content}>
      <div className={styles.header}>
        <h2>{t('UnsUi.recycleBin')}</h2>
        <Space wrap>
          <ComButton icon={<Renew />} onClick={refresh}>
            {t('common.refresh')}
          </ComButton>
          {canRestore && (
            <ComButton icon={<Undo />} disabled={busy || !selection.length} onClick={() => restore(selection)}>
              {t('UnsMeta.restore')}
            </ComButton>
          )}
          {canDelete && (
            <>
              <ComButton
                danger
                icon={<TrashCan />}
                disabled={busy || !selection.length}
                onClick={() => purge(selection)}
              >
                {t('UnsTrash.delete')}
              </ComButton>
              <ComButton danger disabled={busy || !available.length} onClick={() => purge(available)}>
                {t('UnsTrash.empty')}
              </ComButton>
            </>
          )}
        </Space>
      </div>
      {failed && (
        <Alert
          type="error"
          message={t('UnsMeta.loadFailed')}
          action={<ComButton onClick={reload}>{t('UnsMeta.retry')}</ComButton>}
        />
      )}
      {queryFailed && (
        <Alert
          type="warning"
          message={t('UnsTrash.queryFailed')}
          action={<ComButton onClick={refresh}>{t('common.refresh')}</ComButton>}
        />
      )}
      <Space direction="vertical" className={styles.full}>
        {tasks.map((task, index) => (
          <Alert
            key={task.key || task.jobId || index}
            type={task.state === 'succeeded' ? 'success' : task.state === 'pending' ? 'info' : 'warning'}
            message={
              <>
                {t(`UnsTrash.${task.state}`)}
                {task.summary && (
                  <span>
                    {' '}
                    · {t('UnsTrash.progress', { completed: task.summary.succeeded, total: task.summary.total })}
                  </span>
                )}
              </>
            }
            action={
              canDelete && task.key && (task.state === 'unknown' || task.state === 'failed') ? (
                <ComButton disabled={working} onClick={() => void submitPurge(task)}>
                  {t('UnsMeta.retry')}
                </ComButton>
              ) : undefined
            }
          />
        ))}
      </Space>
      <ProTable
        rowKey="id"
        columnFit={false}
        loading={loading || working}
        dataSource={roots}
        pagination={{ pageSize: 20 }}
        rowSelection={{
          selectedRowKeys: selected,
          onChange: setSelected,
          getCheckboxProps: (item) => ({
            disabled: busy || (!canRestore && !canDelete) || !available.some((entry) => entry.id === item.id),
          }),
        }}
        columns={[
          { title: t('UnsMeta.name'), render: (_, item) => item.data.name },
          { title: t('UnsMeta.type'), render: (_, item) => t(`UnsMeta.${item.kind}`) },
          { title: t('UnsTrash.deletedAt'), dataIndex: 'deletedAt', render: displayTime },
        ]}
      />
    </div>
  );
}
