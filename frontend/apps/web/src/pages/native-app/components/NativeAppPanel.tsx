import { cloneElement, type ReactElement, useCallback, useEffect, useRef, useState } from 'react';
import { Alert, App, Button, Checkbox, Dropdown, Flex, Progress, Space, Steps, Tag, Typography, Upload } from 'antd';
import Drawer from '@/components/com-drawer';
import { ChevronDown, Plus } from 'lucide-react';
import ComLayout from '@/components/com-layout';
import ComContent from '@/components/com-layout/ComContent';
import { PageTitleRow } from '@/components/lucide-icon';
import ProModal from '@/components/pro-modal';
import AppBundleUploadContent from '@/pages/project/components/AppBundleUploadContent';
import '@/pages/uns/components/import-modal/index.scss';
import NativeAppCatalog from './NativeAppCatalog';
import { Renew } from '@carbon/icons-react';
import { v4 as uuid } from 'uuid';
import {
  actOnNativeApp,
  getNativeAppJob,
  getNativeAppLogs,
  installNativeApp,
  listNativeApps,
  trashNativeApp,
  previewNativeApp,
  uploadNativeApp,
  type NativeAppBundle,
  type NativeAppAction,
  type NativeAppGrant,
  type NativeAppInstallation,
  type NativeAppJob,
} from '@/apis/core-api/native-app';
import { AuthButton } from '@/components/auth';
import { openConfirmModal } from '@/components/confirm-modal';
import HelpTooltip from '@/components/help-tooltip';
import ProTable from '@/components/pro-table';
import { ButtonPermission } from '@/common-types/button-permission';
import { useTranslate } from '@/hooks';
import { hasPermission } from '@/utils/auth';
import { useBaseStore } from '@/stores/base';
import NativeAppGrants from './NativeAppGrants';
import NativeAppManager from './NativeAppManager';
import AppEditor, { type AppEditorState } from './AppEditor';
import styles from './NativeAppPanel.module.scss';

const stages = ['queued', 'preparing', 'extracting', 'building', 'starting', 'ready'];
const terminal = (job: NativeAppJob) => ['succeeded', 'failed', 'canceled'].includes(job.status);

export default function NativeAppPanel({ launchpad = false }: { launchpad?: boolean }) {
  const t = useTranslate();
  const { message, modal } = App.useApp();
  const userId = useBaseStore((state) => state.currentUserInfo?.sub);
  const jobStorageKey = userId ? `nativeApp.job.${userId}` : '';
  const [apps, setApps] = useState<NativeAppInstallation[]>([]);
  const [showInstaller, setShowInstaller] = useState(false);
  const [archived, setArchived] = useState(false);
  const [editor, setEditor] = useState<AppEditorState>();
  const [skipInvalidRoles, setSkipInvalidRoles] = useState(false);
  const [renameRoles, setRenameRoles] = useState<string[]>([]);
  const listRequest = useRef(0);
  const [loading, setLoading] = useState(false);
  const [listError, setListError] = useState(false);
  const [operationError, setOperationError] = useState('');
  const [uploading, setUploading] = useState(false);
  const [uploadPercent, setUploadPercent] = useState(0);
  const [bundle, setBundle] = useState<NativeAppBundle>();
  const [fileId, setFileId] = useState('');
  const [replace, setReplace] = useState<NativeAppInstallation>();
  const [grants, setGrants] = useState<NativeAppGrant[]>([]);
  const [submitting, setSubmitting] = useState(false);
  const [job, setJob] = useState<NativeAppJob>();
  const [jobId, setJobId] = useState('');
  const [jobError, setJobError] = useState(false);
  const [busyApp, setBusyApp] = useState('');
  const [logApp, setLogApp] = useState<NativeAppInstallation>();
  const [managedAppId, setManagedAppId] = useState('');
  const [logs, setLogs] = useState('');
  const [logsBusy, setLogsBusy] = useState(false);
  const [logsError, setLogsError] = useState(false);
  const abortUpload = useRef<AbortController>();
  const logRequest = useRef(0);
  const installKey = useRef(uuid());
  const mounted = useRef(true);
  // Permission hydration can finish after this cached route first mounts.
  useBaseStore((state) => state.buttonList);
  useBaseStore((state) => state.currentUserInfo?.superAdmin);
  useBaseStore((state) => state.systemInfo?.authEnable);
  const canManage = !launchpad && hasPermission(ButtonPermission['uns.appManage']);
  const installing = !!jobId && (!job || !terminal(job));
  const busy = uploading || submitting || installing;
  const retainGrants = !!(replace && replace.status !== 'notInstalled') || !!bundle?.existingApp;
  const openInstaller = (target?: NativeAppInstallation) => {
    setManagedAppId('');
    setReplace(target);
    setRenameRoles([]);
    setBundle(undefined);
    setFileId('');
    setGrants([]);
    setOperationError('');
    installKey.current = uuid();
    setShowInstaller(true);
  };
  const zipInput = useRef<HTMLInputElement>(null);
  const chooseZip = () => {
    if (canManage && !busy) zipInput.current?.click();
  };
  const managedApp = apps.find((app) => app.appId === managedAppId);

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
      abortUpload.current?.abort();
    };
  }, []);
  const load = useCallback(async () => {
    const request = ++listRequest.current;
    setLoading(true);
    try {
      const result = await listNativeApps(archived && canManage, launchpad);
      if (mounted.current && request === listRequest.current) {
        setApps(result.items);
        setListError(false);
      }
    } catch {
      if (mounted.current && request === listRequest.current) setListError(true);
    } finally {
      if (mounted.current && request === listRequest.current) setLoading(false);
    }
  }, [archived, canManage, launchpad]);
  useEffect(() => {
    void load();
    const timer = window.setInterval(() => void load(), 15000);
    return () => window.clearInterval(timer);
  }, [load]);
  useEffect(() => {
    if (!canManage || !jobStorageKey) return;
    try {
      const saved = sessionStorage.getItem(jobStorageKey);
      if (saved && /^[a-f0-9]{64}$/.test(saved)) setJobId(saved);
    } catch {
      // 无法持久化浏览器会话时，安装仍在后端继续，列表可查看状态。
    }
  }, [canManage, jobStorageKey]);
  // 请求超时保留同一幂等键；重试不会启动第二个安装任务。
  useEffect(() => {
    if (!canManage || !jobId) return;
    let active = true;
    let timer: number;
    const poll = async () => {
      try {
        const result = await getNativeAppJob(jobId);
        if (!active) return;
        setJob(result);
        setJobError(false);
        if (terminal(result)) {
          void load();
          return;
        }
      } catch {
        if (!active) return;
        setJobError(true);
      }
      timer = window.setTimeout(() => void poll(), 2000);
    };
    void poll();
    return () => {
      active = false;
      window.clearTimeout(timer);
    };
  }, [canManage, jobId, load]);
  const upload = async (file: File) => {
    if (!file.name.toLowerCase().endsWith('.zip') || file.size > 500 * 1024 * 1024) {
      setOperationError(t('nativeApp.zipRequired'));
      return;
    }
    const controller = new AbortController();
    abortUpload.current = controller;
    setUploading(true);
    setUploadPercent(0);
    setOperationError('');
    setBundle(undefined);
    setFileId('');
    installKey.current = uuid();
    try {
      const result = await uploadNativeApp(file, controller.signal, setUploadPercent);
      if (controller.signal.aborted) return;
      if (!result?.filePath) throw new Error(t('nativeApp.uploadFailed'));
      const preview = await previewNativeApp(result.filePath);
      if (!mounted.current || controller.signal.aborted) return;
      setSkipInvalidRoles(false);
      setRenameRoles([]);
      setFileId(result.filePath);
      setBundle(preview);
    } catch (error) {
      if (mounted.current && !controller.signal.aborted) {
        setOperationError(error instanceof Error ? error.message : t('nativeApp.uploadFailed'));
      }
    } finally {
      if (mounted.current) setUploading(false);
    }
  };
  const install = async () => {
    if (!fileId) return;
    const aliases = grants.map((grant) => grant.alias.trim());
    if (
      !retainGrants &&
      !bundle?.metadata &&
      (grants.some((grant) => !grant.alias.trim() || !grant.collectionId || !grant.permissions.length) ||
        new Set(aliases).size !== aliases.length)
    ) {
      setOperationError(t('nativeApp.invalidGrants'));
      return;
    }
    setSubmitting(true);
    setOperationError('');
    try {
      if (bundle?.rolePlan) {
        const latest = await previewNativeApp(fileId);
        if (latest.rolePlan?.digest !== bundle.rolePlan.digest) {
          setBundle(latest);
          setRenameRoles([]);
          setOperationError(t('nativeApp.rolePlanChanged'));
          return;
        }
      }
      const result = await installNativeApp({
        fileId,
        appId: replace?.appId || bundle?.existingApp?.appId,
        idempotencyKey: installKey.current,
        skipInvalidRoles,
        rolePlanDigest: bundle?.rolePlan?.digest,
        renameRoleKeys: renameRoles,
        collections:
          retainGrants || bundle?.metadata ? [] : grants.map((grant) => ({ ...grant, alias: grant.alias.trim() })),
      });
      if (!mounted.current) return;
      setJob(undefined);
      setJobId(result.jobId);
      setShowInstaller(false);
      try {
        if (jobStorageKey) sessionStorage.setItem(jobStorageKey, result.jobId);
      } catch {
        // 浏览器存储不可用不影响已经受理的安装任务。
      }
      setBundle(undefined);
      setFileId('');
      setReplace(undefined);
      setGrants([]);
    } catch (error) {
      if (mounted.current) setOperationError(error instanceof Error ? error.message : t('nativeApp.installFailed'));
    } finally {
      if (mounted.current) setSubmitting(false);
    }
  };
  const action = async (app: NativeAppInstallation, name: NativeAppAction) => {
    setBusyApp(app.appId);
    try {
      await actOnNativeApp(app.appId, name);
      message.success(t('nativeApp.actionDone'));
    } catch (error) {
      message.error(error instanceof Error ? error.message : t('nativeApp.actionFailed'));
    } finally {
      await load();
      if (mounted.current) setBusyApp('');
    }
  };
  const confirmAction = (app: NativeAppInstallation, name: Exclude<NativeAppAction, 'start'>) =>
    openConfirmModal(modal, {
      title: t(`nativeApp.${name}`),
      content: t(
        name === 'uninstall'
          ? 'nativeApp.uninstallConfirm'
          : name === 'refreshCredentials'
            ? 'nativeApp.refreshCredsConfirm'
            : 'nativeApp.actionConfirm',
        { name: app.name }
      ),
      okText: t('common.confirm'),
      cancelText: t('common.cancel'),
      danger: name === 'uninstall',
      onOk: () => action(app, name),
    });
  const loadLogs = async (app: NativeAppInstallation) => {
    const request = ++logRequest.current;
    setLogsBusy(true);
    setLogsError(false);
    try {
      const result = await getNativeAppLogs(app.appId);
      if (mounted.current && request === logRequest.current) setLogs(result.text);
    } catch {
      if (mounted.current && request === logRequest.current) setLogsError(true);
    } finally {
      if (mounted.current && request === logRequest.current) setLogsBusy(false);
    }
  };
  const changeTrash = async (app: NativeAppInstallation, trashed: boolean) => {
    setBusyApp(app.appId);
    try {
      await trashNativeApp(app, trashed);
      await load();
    } catch (e) {
      message.error(e instanceof Error ? e.message : t('nativeApp.actionFailed'));
    } finally {
      setBusyApp('');
    }
  };
  const confirmTrash = (app: NativeAppInstallation) =>
    openConfirmModal(modal, {
      title: t('nativeApp.moveToTrash'),
      content: t('nativeApp.trashConfirm', { name: app.name }),
      okText: t('nativeApp.moveToTrash'),
      cancelText: t('common.cancel'),
      danger: true,
      onOk: () => changeTrash(app, true),
    });
  const uploadContent = (
    <div className={styles.stack}>
      {replace && (
        <Alert
          type="info"
          message={t('nativeApp.replacing', { name: replace.name })}
          action={
            <Button
              size="small"
              disabled={busy}
              onClick={() => {
                setReplace(undefined);
                setBundle(undefined);
                setFileId('');
                installKey.current = uuid();
              }}
            >
              {t('common.cancel')}
            </Button>
          }
        />
      )}
      <input
        ref={zipInput}
        type="file"
        accept=".zip"
        hidden
        disabled={busy || !canManage}
        onChange={(event) => {
          const file = event.currentTarget.files?.[0];
          event.currentTarget.value = '';
          if (file && !busy && canManage) void upload(file);
        }}
      />
      <div onClick={chooseZip}>
        <Upload.Dragger
          className="uploadWrap"
          openFileDialogOnClick={false}
          accept=".zip"
          maxCount={1}
          multiple={false}
          showUploadList={false}
          disabled={busy || !canManage}
          beforeUpload={(file) => {
            void upload(file);
            return false;
          }}
        >
          <AppBundleUploadContent />
          <Button
            disabled={busy || !canManage}
            onClick={(event) => {
              event.stopPropagation();
              chooseZip();
            }}
          >
            {t('nativeApp.chooseZip')}
          </Button>
        </Upload.Dragger>
      </div>
      {uploading && (
        <Space direction="vertical" style={{ width: '100%' }}>
          <Progress percent={uploadPercent} />
          <Space>
            {t(uploadPercent === 100 ? 'nativeApp.validating' : 'nativeApp.uploading')}
            <Button size="small" onClick={() => abortUpload.current?.abort()}>
              {t('common.cancel')}
            </Button>
          </Space>
        </Space>
      )}
      {operationError && <Alert type="error" showIcon message={operationError} />}
      {bundle && (
        <div className={`${styles.bundle} ${styles.stack}`}>
          <Space wrap>
            <Typography.Text strong>{bundle.name}</Typography.Text>
            {bundle.version && <Tag>{bundle.version}</Tag>}
            <Tag>{bundle.runtime.packageManager || 'npm'}</Tag>
          </Space>
          {bundle.description && <Typography.Text type="secondary">{bundle.description}</Typography.Text>}
          {bundle.existingApp && (
            <Alert
              type="info"
              showIcon
              message={t(
                bundle.existingApp.samePackage && !bundle.rolePlan
                  ? 'nativeApp.samePackage'
                  : 'nativeApp.updateExisting',
                {
                  name: bundle.existingApp.name,
                  version: bundle.existingApp.version,
                }
              )}
            />
          )}
          {bundle.invalidRoles && (
            <Alert
              type="warning"
              showIcon
              message={t('nativeApp.invalidRoleFile')}
              description={
                <Checkbox checked={skipInvalidRoles} onChange={(e) => setSkipInvalidRoles(e.target.checked)}>
                  {t('nativeApp.skipInvalidRoles')}
                </Checkbox>
              }
            />
          )}
          {bundle.rolePlan && (
            <section className={styles.stack}>
              <Typography.Text strong>{t('nativeApp.importRoles')}</Typography.Text>
              {bundle.rolePlan.items.map((role) => (
                <div key={role.role_key}>
                  <Space wrap>
                    <Tag>{t(role.existingName ? 'nativeApp.meta.reuse' : 'nativeApp.meta.create')}</Tag>
                    {role.existingName || role.name} <Typography.Text type="secondary">{role.role_key}</Typography.Text>
                  </Space>
                  {role.description && <div>{role.description}</div>}
                  {role.existingName && role.name !== role.existingName && (
                    <Checkbox
                      checked={renameRoles.includes(role.role_key)}
                      onChange={(e) => {
                        setRenameRoles((current) =>
                          e.target.checked
                            ? [...current, role.role_key]
                            : current.filter((key) => key !== role.role_key)
                        );
                        installKey.current = uuid();
                      }}
                    >
                      {t('nativeApp.renameRole', { name: role.name })}
                    </Checkbox>
                  )}
                </div>
              ))}
            </section>
          )}
          {!!bundle.ignoredResources.length && (
            <Alert type="info" message={t('nativeApp.ignored', { resources: bundle.ignoredResources.join(', ') })} />
          )}
          {bundle.metadata ? (
            <Space direction="vertical" style={{ width: '100%' }}>
              <Space>
                <Typography.Text strong>{t('nativeApp.metadata')}</Typography.Text>
                <HelpTooltip title={t('nativeApp.metadataHint')} />
              </Space>
              <ProTable
                rowKey={(item) => `${item.kind}:${item.key}`}
                pagination={false}
                dataSource={bundle.metadataPlan || []}
                columns={[
                  { title: t('common.name'), dataIndex: 'name' },
                  {
                    title: t('nativeApp.resourceKind'),
                    dataIndex: 'kind',
                    render: (kind: string) => t(`nativeApp.kind.${kind}`),
                  },
                  { title: t('nativeApp.alias'), dataIndex: 'alias' },
                  {
                    title: t('common.operation'),
                    dataIndex: 'action',
                    render: (action: string) => t(`nativeApp.meta.${action}`),
                  },
                  {
                    title: t('nativeApp.permissions'),
                    dataIndex: 'permissions',
                    render: (permissions: string[]) => permissions.join(', '),
                  },
                ]}
              />
              {bundle.metadataPlan?.some((item) => item.kind === 'source' || item.kind === 'stream') && (
                <Alert type="info" showIcon message={t('nativeApp.importResourcesInactive')} />
              )}
            </Space>
          ) : retainGrants ? (
            <Typography.Text type="secondary">{t('nativeApp.retainGrants')}</Typography.Text>
          ) : (
            <NativeAppGrants
              value={grants}
              onChange={(value) => {
                setGrants(value);
                installKey.current = uuid();
              }}
              disabled={busy}
            />
          )}
          <Space>
            <AuthButton
              auth={ButtonPermission['uns.appManage']}
              type="primary"
              loading={submitting}
              disabled={
                busy ||
                (bundle.invalidRoles && !skipInvalidRoles) ||
                (bundle.existingApp?.samePackage && !bundle.rolePlan)
              }
              onClick={() => void install()}
            >
              {t(
                bundle.rolePlan
                  ? 'nativeApp.confirmImport'
                  : bundle.existingApp?.samePackage
                    ? 'nativeApp.alreadyInstalled'
                    : replace || bundle.existingApp
                      ? 'nativeApp.updateApp'
                      : 'nativeApp.install'
              )}
            </AuthButton>
            <HelpTooltip title={t('nativeApp.trustHint')} />
          </Space>
        </div>
      )}
    </div>
  );
  const content = (
    <section className={styles.panel} aria-label={t('nativeApp.title')}>
      {canManage && jobId && (
        <div className={styles.job}>
          {(!job || !terminal(job)) && (
            <Steps
              size="small"
              current={
                job?.status === 'succeeded' ? stages.length : Math.max(0, stages.indexOf(job?.stage || 'queued'))
              }
              status={
                job?.status === 'failed' || job?.status === 'canceled'
                  ? 'error'
                  : job?.status === 'succeeded'
                    ? 'finish'
                    : 'process'
              }
              items={stages.map((stage) => ({ title: t(`nativeApp.stage.${stage}`) }))}
            />
          )}
          {jobError && <Alert type="warning" showIcon message={t('nativeApp.pollFailed')} />}
          {job?.error && <Alert type="error" showIcon message={job.error} />}
          {job?.status === 'succeeded' && (
            <Alert
              type="success"
              message={t('nativeApp.installDone')}
              closable
              onClose={() => {
                setJobId('');
                try {
                  if (jobStorageKey) sessionStorage.removeItem(jobStorageKey);
                } catch {
                  /* 安装结果仍保存在后端。 */
                }
              }}
            />
          )}
        </div>
      )}
      {listError && <Alert type="error" showIcon message={t('nativeApp.listFailed')} />}
      <NativeAppCatalog
        apps={apps}
        canManage={canManage}
        archived={archived && canManage}
        launchpad={launchpad}
        loading={loading}
        busy={busy || !!busyApp}
        onArchiveChange={(next) => {
          setApps([]);
          setArchived(next);
        }}
        onRefresh={() => void load()}
        onEdit={(app) => setEditor({ mode: 'edit', app })}
        onAccess={(app) => setEditor({ mode: 'access', app })}
        onImport={openInstaller}
        onManage={(app) => setManagedAppId(app.appId)}
        onTrash={confirmTrash}
        onRestore={(app) => void changeTrash(app, false)}
      />
      <ProModal
        width={560}
        className="importModalWrap attachment-upload-modal"
        fullScreenable={false}
        styles={{ body: { maxHeight: '68vh', overflowY: 'auto', paddingInlineEnd: 12 } }}
        title={t(replace ? 'nativeApp.update' : 'project.importApp')}
        open={canManage && showInstaller}
        onCancel={() => {
          if (!uploading && !submitting) setShowInstaller(false);
        }}
        maskClosable={!uploading && !submitting}
        keyboard={!uploading && !submitting}
      >
        {uploadContent}
      </ProModal>
      {canManage && editor && (
        <AppEditor
          editor={editor}
          onClose={() => setEditor(undefined)}
          onSaved={() => {
            setEditor(undefined);
            void load();
          }}
        />
      )}
      <NativeAppManager
        app={canManage ? managedApp : undefined}
        busy={busy || !!busyApp}
        actionLoading={busyApp === managedAppId}
        onClose={() => setManagedAppId('')}
        onStart={(app) => void action(app, 'start')}
        onConfirm={confirmAction}
        onReplace={openInstaller}
        onLogs={(app) => {
          setLogApp(app);
          setLogs('');
          void loadLogs(app);
        }}
      />
      <Drawer
        width={800}
        title={t('nativeApp.logsTitle', { name: logApp?.name || '' })}
        open={!!logApp}
        onClose={() => {
          logRequest.current++;
          setLogApp(undefined);
        }}
      >
        <Button icon={<Renew size={16} />} loading={logsBusy} onClick={() => logApp && void loadLogs(logApp)}>
          {t('common.refresh')}
        </Button>
        {logsError ? (
          <Alert type="error" message={t('nativeApp.logsFailed')} />
        ) : (
          <pre className={styles.logs}>{logs || t('nativeApp.noLogs')}</pre>
        )}
      </Drawer>
    </section>
  );
  if (launchpad) return content;
  return (
    <ComLayout>
      <ComContent
        hasBack={false}
        title={<PageTitleRow resourceKey="uns.app.read">{t('nativeApp.title')}</PageTitleRow>}
        extra={
          canManage &&
          !archived && (
            <Dropdown.Button
              type="primary"
              trigger={['click']}
              icon={<ChevronDown size={14} />}
              disabled={busy}
              onClick={() => setEditor({ mode: 'create' })}
              buttonsRender={([primary, more]) => [
                primary,
                cloneElement(more as ReactElement, { 'aria-label': t('common.more') }),
              ]}
              menu={{ items: [{ key: 'import', label: t('project.importApp') }], onClick: () => openInstaller() }}
            >
              <Flex align="center" gap={6}>
                <Plus size={16} />
                {t('nativeApp.create')}
              </Flex>
            </Dropdown.Button>
          )
        }
        style={{ padding: '0 24px 24px' }}
      >
        {content}
      </ComContent>
    </ComLayout>
  );
}
