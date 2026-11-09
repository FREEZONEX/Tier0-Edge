import { PlusOutlined } from '@ant-design/icons';
import { Button, Alert, Form, Popconfirm, Space, Tabs, Tag, message as antdMessage } from 'antd';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  copyNotification,
  deleteNotification,
  getNotificationList,
  getNotificationDetail,
  type NotificationItem,
  revokeNotification,
  saveNotification,
  sendNotification,
} from '@/apis/core-api/notify';
import { useTranslate } from '@/hooks';
import ComLayout from '@/components/com-layout';
import ComContent from '@/components/com-layout/ComContent';
import ProTable from '@/components/pro-table';
import ProModal from '@/components/pro-modal';
import ComButton from '@/components/com-button';

import { resolveRecipientUserIds } from './components/RecipientSelector';
import SendSettings from './components/SendSettings';
import NotificationFields from './components/NotificationFields';
import {
  NOTIFICATION_INITIAL_VALUES,
  SEND_INITIAL_VALUES,
  scheduleTimestamp,
  type NotificationFormValues,
  type SendFormValues,
} from './notification-form';
import MessageHistoryTable from './components/MessageHistoryTable';
import styles from './index.module.scss';

// 通知管理（TASK-030 管理端）：运营通知的草稿、发送（全部/指定用户、定时）、撤回、复制、删除。
// 创建/编辑弹窗内置发送设置，可保存草稿或保存并发送一步完成。
// 权限：IAM 资源 notify.manage（admin 默认全量）。
// i18n：文案主源为后端 etc/i18n（notificationManage.* 前缀），前端 locale 不进 fallback 白名单。

const PAGE_SIZE = 20;

const STATUS_COLOR: Record<number, string> = {
  1: 'default',
  2: 'blue',
  3: 'processing',
  4: 'green',
  5: 'red',
};

const NotificationManage = () => {
  const formatMessage = useTranslate();
  // 签名对齐 messageCenter 组件的 t：键名省略前缀，values 为插值，fallback 为键缺失时的兜底文案
  const t = useCallback(
    (key: string, values?: Record<string, unknown>, fallback?: string) =>
      formatMessage(`notificationManage.${key}`, values, fallback),
    [formatMessage]
  );

  const [list, setList] = useState<NotificationItem[]>([]);
  const [activeTab, setActiveTab] = useState('notifications');
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(false);

  const [editOpen, setEditOpen] = useState(false);
  const [editForm] = Form.useForm<NotificationFormValues>();
  const [editingId, setEditingId] = useState<number | null>(null);

  const [sendTarget, setSendTarget] = useState<NotificationItem | null>(null);
  const [sendForm] = Form.useForm<SendFormValues>();
  const [pendingAction, setPendingAction] = useState<'draft' | 'send' | null>(null);
  const submitting = useRef(false);
  const sending = pendingAction !== null;
  const [sendError, setSendError] = useState<'saveSendFailed' | 'sendStateUnknown' | null>(null);
  const editSendMode = Form.useWatch('sendMode', editForm);
  const sendMode = Form.useWatch('sendMode', sendForm);

  const statusText = useCallback(
    (s: number) =>
      (
        ({
          1: t('statusDraft'),
          2: t('statusPlanned'),
          3: t('statusRunning'),
          4: t('statusSent'),
          5: t('statusRevoked'),
        }) as Record<number, string>
      )[s] ?? String(s),
    [t]
  );

  const typeText = useCallback(
    (tp: string) =>
      (({ inbox: t('typeInbox'), banner: t('typeBanner'), news: t('typeNews') }) as Record<string, string>)[tp] ?? tp,
    [t]
  );

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const res = await getNotificationList({ page, pageSize: PAGE_SIZE });
      setList(res.list || []);
      setTotal(res.total || 0);
    } finally {
      setLoading(false);
    }
  }, [page]);

  useEffect(() => {
    load();
  }, [load]);

  const openCreate = () => {
    setEditingId(null);
    editForm.resetFields();
    setSendError(null);
    setEditOpen(true);
  };

  const resolveSelectedUserIds = async (values: SendFormValues) => {
    if (values.audienceType !== 2) return undefined;
    const userIds = await resolveRecipientUserIds(values.recipientSubjects);
    if (!userIds.length) {
      antdMessage.warning(t('recipientEmpty'));
      return null;
    }
    return userIds;
  };

  // 响应丢失不等于投递失败；只有确认仍为草稿，才能再次保存和发送。
  const reconcileSavedNotification = async (id: number) => {
    try {
      const current = await getNotificationDetail(id, true);
      if (current.status === 1) return true;
      if ([2, 3, 4, 5].includes(current.status)) {
        setSendError(null);
        antdMessage.info(t('sendStateVerified', { status: statusText(current.status) }));
        setEditOpen(false);
        void load();
        return false;
      }
    } catch {
      // 无法核对时保留表单，但禁止继续写入或发送，下一次操作先重新核对。
    }
    setSendError('sendStateUnknown');
    return false;
  };

  // 两个入口共享提交锁；失败后复用已保存的 ID，保留表单，避免重试创建重复草稿。
  const handleSave = async (send: boolean) => {
    if (submitting.current) return;
    submitting.current = true;
    setPendingAction(send ? 'send' : 'draft');
    try {
      if (editingId !== null && !(await reconcileSavedNotification(editingId))) return;
      const values = send
        ? await editForm.validateFields()
        : await editForm.validateFields(['type', 'title', 'content', 'link', 'asModal']);
      const userIds = send ? await resolveSelectedUserIds(values) : undefined;
      if (userIds === null) return;
      setSendError(null);
      const saved = await saveNotification({
        id: editingId ?? undefined,
        type: values.type,
        title: values.title.trim(),
        content: values.content,
        link: values.link,
        asModal: values.asModal ? 1 : 0,
      });
      setEditingId(saved.id);
      if (!send) {
        antdMessage.success(t('savedDraft', { id: saved.id }));
        setEditOpen(false);
        void load();
        return;
      }
      try {
        await sendNotification(
          {
            id: saved.id,
            audienceType: values.audienceType,
            userIds,
            scheduleTime: scheduleTimestamp(values),
          },
          true
        );
        antdMessage.success(values.sendMode === 'scheduled' ? t('savedAndScheduled') : t('savedAndSent'));
        setEditOpen(false);
      } catch {
        if (await reconcileSavedNotification(saved.id)) setSendError('saveSendFailed');
      } finally {
        void load();
      }
    } finally {
      submitting.current = false;
      setPendingAction(null);
    }
  };

  const openSend = async (row: NotificationItem) => {
    setSendTarget(row);
    sendForm.resetFields();
    sendForm.setFieldsValue(SEND_INITIAL_VALUES);
  };

  const handleSend = async () => {
    if (!sendTarget || submitting.current) return;
    submitting.current = true;
    setPendingAction('send');
    try {
      const values = await sendForm.validateFields();
      const userIds = await resolveSelectedUserIds(values);
      if (userIds === null) return;
      await sendNotification({
        id: sendTarget.id,
        audienceType: values.audienceType,
        userIds,
        scheduleTime: scheduleTimestamp(values),
      });
      antdMessage.success(values.sendMode === 'scheduled' ? t('sentScheduled') : t('sentNow'));
      setSendTarget(null);
      load();
    } finally {
      submitting.current = false;
      setPendingAction(null);
    }
  };

  const columns = useMemo(
    () => [
      { title: t('colId'), dataIndex: 'id', width: 70 },
      { title: t('colType'), dataIndex: 'type', width: 90, render: (tp: string) => typeText(tp) },
      { title: t('colTitle'), dataIndex: 'title', ellipsis: true },
      {
        title: t('colStatus'),
        dataIndex: 'status',
        width: 100,
        render: (st: number) => <Tag color={STATUS_COLOR[st]}>{statusText(st)}</Tag>,
      },
      {
        title: t('colCreatedAt'),
        dataIndex: 'createdTime',
        width: 170,
        render: (ms: number) => (ms ? new Date(ms).toLocaleString() : ''),
      },
      {
        title: t('colAction'),
        key: 'action',
        width: 260,
        render: (_: unknown, row: NotificationItem) => (
          <Space size={4} wrap>
            {(row.status === 1 || row.status === 2) && (
              <Button size="small" type="link" onClick={() => openSend(row)}>
                {t('actionSend')}
              </Button>
            )}
            {[3, 4].includes(row.status) && (
              <Popconfirm
                title={t('revokeConfirm')}
                onConfirm={async () => {
                  await revokeNotification(row.id);
                  antdMessage.success(t('revoked'));
                  load();
                }}
              >
                <Button size="small" type="link">
                  {t('actionRevoke')}
                </Button>
              </Popconfirm>
            )}
            {row.status === 4 && (
              <Button
                size="small"
                type="link"
                onClick={async () => {
                  const res = await copyNotification(row.id);
                  antdMessage.success(t('copied', { id: res.id }));
                  load();
                }}
              >
                {t('actionCopy')}
              </Button>
            )}
            {[1, 4, 5].includes(row.status) && (
              <Popconfirm
                title={t('deleteConfirm')}
                onConfirm={async () => {
                  await deleteNotification(row.id);
                  antdMessage.success(t('deleted'));
                  load();
                }}
              >
                <Button size="small" type="link" danger>
                  {t('actionDelete')}
                </Button>
              </Popconfirm>
            )}
          </Space>
        ),
      },
    ],
    // openSend/load 闭包依赖页面状态，随组件重渲染更新
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [t, page, list]
  );

  return (
    <ComLayout>
      <ComContent
        title={t('title')}
        hasBack={false}
        className={styles['notification-content']}
        extra={
          activeTab === 'notifications' ? (
            <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>
              {t('create')}
            </Button>
          ) : null
        }
      >
        <Tabs
          className={styles['notification-tabs']}
          activeKey={activeTab}
          onChange={setActiveTab}
          items={[
            {
              key: 'notifications',
              label: t('tabNotifications'),
              children: (
                <ProTable
                  rowKey="id"
                  loading={loading}
                  dataSource={list}
                  columns={columns}
                  pagination={{
                    current: page,
                    pageSize: PAGE_SIZE,
                    total,
                    onChange: (p) => setPage(p),
                    showSizeChanger: false,
                  }}
                />
              ),
            },
            { key: 'history', label: t('tabHistory'), children: <MessageHistoryTable t={t} /> },
          ]}
        />
      </ComContent>

      {/* 创建/编辑草稿（含发送设置，可顺便发送） */}
      <ProModal
        size="sm"
        title={editingId ? t('editDraft') : t('create')}
        open={editOpen}
        maskClosable={false}
        closable={!sending}
        onCancel={() => {
          if (!sending) setEditOpen(false);
        }}
        destroyOnHidden
        footer={[
          <Button key="cancel" disabled={sending} onClick={() => setEditOpen(false)}>
            {t('cancel')}
          </Button>,
          <ComButton
            key="draft"
            disabled={sending}
            loading={pendingAction === 'draft'}
            onClick={() => handleSave(false)}
          >
            {t('saveDraft')}
          </ComButton>,
          <ComButton
            key="save-send"
            type="primary"
            disabled={sending}
            loading={pendingAction === 'send'}
            onClick={() => handleSave(true)}
          >
            {t(editSendMode === 'scheduled' ? 'sendScheduled' : 'sendNow')}
          </ComButton>,
        ]}
      >
        {sendError && (
          <Alert
            className={styles['send-error']}
            type={sendError === 'sendStateUnknown' ? 'warning' : 'error'}
            showIcon
            message={t(sendError, { id: editingId })}
          />
        )}
        <Form form={editForm} layout="vertical" initialValues={NOTIFICATION_INITIAL_VALUES} disabled={sending}>
          <div className={styles['editor-grid']}>
            <section>
              <NotificationFields />
            </section>
            <section className={styles['send-settings']}>
              <h3>{t('sendSettings')}</h3>
              <SendSettings form={editForm} disabled={sending} />
            </section>
          </div>
        </Form>
      </ProModal>

      {/* 发送配置 */}
      <ProModal
        size="xs"
        title={t('actionSend')}
        open={!!sendTarget}
        maskClosable={false}
        closable={!sending}
        footer={[
          <Button key="cancel" disabled={sending} onClick={() => setSendTarget(null)}>
            {t('cancel')}
          </Button>,
          <ComButton key="send" type="primary" loading={sending} onClick={handleSend}>
            {t(sendMode === 'scheduled' ? 'sendScheduled' : 'sendNow')}
          </ComButton>,
        ]}
        onCancel={() => {
          if (!sending) setSendTarget(null);
        }}
        destroyOnHidden
      >
        <div className={styles['notification-summary']}>
          <strong>{sendTarget?.title}</strong>
          <p>{sendTarget?.content}</p>
        </div>
        <Form form={sendForm} layout="vertical" initialValues={SEND_INITIAL_VALUES} disabled={sending}>
          <SendSettings form={sendForm} disabled={sending} />
        </Form>
      </ProModal>
    </ComLayout>
  );
};

export default NotificationManage;
