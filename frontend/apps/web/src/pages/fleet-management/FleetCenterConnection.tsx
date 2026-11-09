import { useCallback, useEffect, useState, type ReactNode } from 'react';
import { Alert, App, Button, Drawer, Form, Input, Modal, Space, Steps, Tag, Typography, Checkbox } from 'antd';
import { Activity, Download, FileClock, FileSearch, Inspect, Plug, Settings, Upload, Users } from 'lucide-react';
import { toolbarIconProps } from '@/components/lucide-icon/icon-props';
import { Renew } from '@/components/lucide-icon/carbon';
import { configureCurrentFleetConnection } from '@/apis/core-api/fleet';
import {
  getFleetConnection,
  saveFleetCollectionScope,
  type FleetCollectionScope,
  saveFleetRemoteAccess,
  getFleetSyncedUsers,
  type FleetConnectionState,
  type FleetSyncedUser,
} from '@/apis/core-api/fleet-connection';
import { getClusterSyncLogs, type CloudSyncLogItem } from '@/apis/core-api/cloudsync';
import { collectionStorageAction, type CollectionStorage, type StorageMode } from '@/apis/core-api/collection-storage';
import { AuthButton } from '@/components/auth';
import HelpTooltip from '@/components/help-tooltip';
import ProTable from '@/components/pro-table';
import { ButtonPermission } from '@/common-types/button-permission';
import { useTranslate } from '@/hooks';
import { formatTimestamp } from '@/utils';
import { hasPermission } from '@/utils/auth';
import styles from './FleetCenterConnection.module.scss';
import FleetCollectionSelection from './FleetCollectionSelection';

const connectionReasons: Record<string, string> = {
  FLEET_BUSINESS_CHECK_TIMEOUT: 'fleet.edge.diag.businessTimeout',
  FLEET_BUSINESS_CHECK_FAILED: 'fleet.edge.diag.businessFailed',
  FLEET_INVALID_TOKEN: 'fleet.edge.diag.token',
  FLEET_CENTER_ENDPOINT_NOT_FOUND: 'fleet.edge.diag.endpoint',
  FLEET_NETWORK_BOOTSTRAP_UNAVAILABLE: 'fleet.edge.diag.bootstrap',
  FLEET_NETWORK_BOOTSTRAP_REJECTED: 'fleet.edge.diag.rejected',
  FLEET_AGENT_UNAVAILABLE: 'fleet.edge.diag.agent',
  FLEET_AGENT_NOT_CONFIGURED: 'fleet.edge.diag.agent',
  FLEET_AGENT_AUTH_FAILED: 'fleet.edge.diag.agent',
  FLEET_NETWORK_CLIENT_UNAVAILABLE: 'fleet.edge.diag.client',
  FLEET_NETWORK_PREFERENCES_UNAVAILABLE: 'fleet.edge.diag.client',
  FLEET_NETWORK_STATE_ISOLATION_REQUIRED: 'fleet.edge.diag.isolation',
  FLEET_NETWORK_DISABLED: 'fleet.edge.diag.disabled',
  FLEET_CONNECTION_MODE_MISMATCH: 'fleet.edge.diag.mode',
  FLEET_NETWORK_IDENTITY_ALREADY_BOUND: 'fleet.edge.diag.identity',
  FLEET_NETWORK_IDENTITY_MISMATCH: 'fleet.edge.diag.identity',
  FLEET_NETWORK_CONTROL_PLANE_CONFLICT: 'fleet.edge.diag.identity',
  FLEET_NETWORK_KEY_EXPIRED: 'fleet.edge.diag.expired',
  FLEET_NETWORK_REAUTH_REQUIRED: 'fleet.edge.diag.expired',
  FLEET_NETWORK_KEY_UNAVAILABLE: 'fleet.edge.diag.key',
  FLEET_NETWORK_CREDENTIAL_UNAVAILABLE: 'fleet.edge.diag.key',
  FLEET_NETWORK_PROFILE_LOGIN_FAILED: 'fleet.edge.diag.login',
  FLEET_NETWORK_PROFILE_NAME_FAILED: 'fleet.edge.diag.profile',
  FLEET_NETWORK_PROFILE_UNAVAILABLE: 'fleet.edge.diag.profile',
  FLEET_NETWORK_PROFILES_INVALID: 'fleet.edge.diag.profile',
  FLEET_NETWORK_PROFILE_RESTORE_FAILED: 'fleet.edge.diag.profile',
  FLEET_NETWORK_PROFILE_ROLLBACK_PENDING: 'fleet.edge.diag.profile',
  FLEET_NETWORK_INITIALIZING: 'fleet.edge.diag.initializing',
  FLEET_CONNECTION_PREPARE_FAILED: 'fleet.edge.diag.prepare',
};

export default function FleetCenterConnection() {
  const t = useTranslate();
  const { message } = App.useApp();
  const [current, setCurrent] = useState<FleetConnectionState>();
  const [lastStorage, setStorage] = useState<CollectionStorage>();
  const [accountSync, setAccountSync] = useState<{ nodeId: string; log?: CloudSyncLogItem; failed?: boolean }>();
  const [accountRefresh, setAccountRefresh] = useState(0);
  const [open, setOpen] = useState(false);
  const [replacingToken, setReplacingToken] = useState(false);
  const [scopeEditing, setScopeEditing] = useState<FleetCollectionScope>();
  const [storageDraft, setStorageDraft] = useState<string[]>([]);
  const [storageRevision, setStorageRevision] = useState(0);
  const [configError, setConfigError] = useState('');
  const [connectionError, setConnectionError] = useState('');
  const [storageError, setStorageError] = useState(false);
  const storage = storageError ? undefined : lastStorage;
  const [saving, setSaving] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState(false);
  const [drawer, setDrawer] = useState<'users' | 'history'>();
  const [users, setUsers] = useState<FleetSyncedUser[]>([]);
  const [logs, setLogs] = useState<CloudSyncLogItem[]>([]);
  const [detail, setDetail] = useState<CloudSyncLogItem>();
  const [page, setPage] = useState(1);
  const [total, setTotal] = useState(0);
  const [drawerBusy, setDrawerBusy] = useState(false);
  const [drawerError, setDrawerError] = useState(false);
  const [connectionDetails, setConnectionDetails] = useState(false);
  const [diagnostics, setDiagnostics] = useState(false);
  const [syncDetails, setSyncDetails] = useState(false);
  const [form] = Form.useForm<{
    accessToken: string;
    allowSSH: boolean;
    allowMetadataWrite: boolean;
    allowControl: boolean;
    collectionScope: FleetCollectionScope;
  }>();
  const load = useCallback(async () => {
    setLoading(true);
    try {
      const result = await getFleetConnection();
      setCurrent((previous) => ({
        ...result,
        access: previous && previous.access.revision > result.access.revision ? previous.access : result.access,
        collectionScope:
          previous && previous.collectionScope.revision > result.collectionScope.revision
            ? previous.collectionScope
            : result.collectionScope,
      }));
      setError(false);
    } catch {
      setError(true);
    } finally {
      setLoading(false);
    }
  }, []);
  const loadStorage = useCallback(async () => {
    try {
      setStorage(await collectionStorageAction<CollectionStorage>(undefined, 'get'));
      setStorageError(false);
    } catch {
      setStorageError(true);
    }
  }, []);
  useEffect(() => {
    void loadStorage();
  }, [loadStorage, current?.checkedAt]);
  useEffect(() => {
    void load();
    const timer = window.setInterval(
      () => void load(),
      current?.switch && !['ready', 'cancelled'].includes(current.switch.state) ? 3000 : 30000
    );
    return () => window.clearInterval(timer);
  }, [load, current?.switch?.state]);
  const nodeId = current?.connection?.nodeId;
  const lastAccountSync = accountSync?.nodeId === nodeId ? accountSync : undefined;
  const latest = lastAccountSync?.log;
  const syncError = lastAccountSync?.failed;
  useEffect(() => {
    if (!nodeId) return;
    let active = true;
    const refresh = async () => {
      try {
        const result = await getClusterSyncLogs({
          syncType: 'user',
          direction: 'inbound',
          nodeID: nodeId,
          page: 1,
          size: 1,
        });
        if (active) setAccountSync({ nodeId, log: result.list[0] });
      } catch {
        if (active) setAccountSync({ nodeId, failed: true });
      }
    };
    void refresh();
    const timer = window.setInterval(refresh, 30000);
    return () => {
      active = false;
      window.clearInterval(timer);
    };
  }, [nodeId, accountRefresh]);
  const loadDrawer = useCallback(
    async (view: 'users' | 'history', nextPage = 1) => {
      setDrawerBusy(true);
      setDrawerError(false);
      try {
        if (view === 'users') {
          const result = await getFleetSyncedUsers();
          setUsers(result.list.filter((user) => !user.deletedTime));
        } else if (nodeId) {
          const result = await getClusterSyncLogs({
            syncType: 'user',
            direction: 'inbound',
            nodeID: nodeId,
            page: nextPage,
            size: 10,
          });
          setLogs(result.list);
          setTotal(result.total);
          setPage(nextPage);
        }
      } catch {
        setDrawerError(true);
      } finally {
        setDrawerBusy(false);
      }
    },
    [nodeId]
  );
  const openDrawer = (view: 'users' | 'history') => {
    setDetail(undefined);
    setDrawer(view);
    void loadDrawer(view);
  };
  const save = async () => {
    const values = await form.validateFields();
    setSaving(true);
    setConnectionError('');
    try {
      if (!current?.access) throw new Error(t('fleet.resources.unavailable'));
      await saveFleetRemoteAccess({
        allowSSH: values.allowSSH,
        allowMetadataWrite: values.allowMetadataWrite,
        allowControl: values.allowControl,
        expectedRevision: current.access.revision,
      });
      await saveFleetCollectionScope({
        mode: values.collectionScope.mode,
        collectionIds: values.collectionScope.collectionIds,
        expectedRevision: current.collectionScope.revision,
      });
      if (values.accessToken?.trim()) await configureCurrentFleetConnection({ accessToken: values.accessToken.trim() });
      form.resetFields();
      setOpen(false);
      await load();
      message.success(t(values.accessToken?.trim() ? 'fleet.edge.tokenSaved' : 'appGui.saveSuccess'));
    } catch (failure) {
      const code = (failure as { code?: number })?.code;
      // Never render raw transport errors: they may contain request credentials.
      setConnectionError(code === 404 ? 'fleet.edge.diag.endpoint' : 'fleet.edge.diag.submit');
    } finally {
      await load();
      setSaving(false);
    }
  };
  const openTokenForm = () => {
    setConnectionDetails(false);
    setReplacingToken(false);
    setConnectionError('');
    setOpen(true);
  };
  const openSyncSettings = () => {
    if (!storage || !current?.collectionScope) return;
    setScopeEditing(current.collectionScope);
    setStorageDraft(storage.policy.mode === 'both' ? ['local', 'center'] : [storage.policy.mode]);
    setStorageRevision(storage.policy.revision);
    setConfigError('');
  };
  const saveScope = async () => {
    if (!scopeEditing || !storage || !current) return;
    if (!storageDraft.length) {
      setConfigError(t('collectionStorage.atLeastOne'));
      return;
    }
    setSaving(true);
    setConfigError('');
    let scopeSaved = false;
    try {
      const changed =
        scopeEditing.mode !== current.collectionScope.mode ||
        JSON.stringify([...scopeEditing.collectionIds].sort()) !==
          JSON.stringify([...current.collectionScope.collectionIds].sort());
      if (changed) {
        const collectionScope = await saveFleetCollectionScope({
          mode: scopeEditing.mode,
          collectionIds: scopeEditing.collectionIds,
          expectedRevision: scopeEditing.revision,
        });
        setCurrent((previous) => (previous ? { ...previous, collectionScope } : previous));
        setScopeEditing(collectionScope);
        scopeSaved = true;
      }
      const mode: StorageMode = storageDraft.length === 2 ? 'both' : (storageDraft[0] as StorageMode);
      if (mode !== storage.policy.mode) {
        setStorage(
          await collectionStorageAction<CollectionStorage>(undefined, 'save', {
            expectedRevision: storageRevision,
            mode,
          })
        );
      }
      setScopeEditing(undefined);
      message.success(t('UnsMeta.saved'));
    } catch {
      setConfigError(t(scopeSaved ? 'fleet.collections.policyNotSaved' : 'fleet.detail.accountsSaveFailed'));
    } finally {
      await Promise.all([load(), loadStorage()]);
      setSaving(false);
    }
  };
  const updateAccess = async (key: 'allowSSH' | 'allowMetadataWrite' | 'allowControl', checked: boolean) => {
    if (!current?.access || saving) return;
    setSaving(true);
    try {
      const access = await saveFleetRemoteAccess({
        allowSSH: current.access.allowSSH,
        allowMetadataWrite: current.access.allowMetadataWrite,
        allowControl: current.access.allowControl ?? false,
        [key]: checked,
        expectedRevision: current.access.revision,
      });
      setCurrent((previous) => (previous ? { ...previous, access } : previous));
    } catch {
      message.error(t('fleet.access.saveFailed'));
      await load();
    } finally {
      setSaving(false);
    }
  };
  const time = (value?: number) => (value ? formatTimestamp(value) : '-');
  const field = (label: string, value: ReactNode) => (
    <div className={styles.branchInfoField}>
      <span className={styles.branchInfoLabel}>{label}</span>
      <div className={styles.branchInfoValue} title={typeof value === 'string' ? value : undefined}>
        {value}
      </div>
    </div>
  );
  const syncStatus = (status?: string) => (
    <Tag color={status === 'success' ? 'success' : status === 'failed' ? 'error' : 'default'}>
      {t(
        status === 'success'
          ? 'mqttAuth.syncSuccess'
          : status === 'failed'
            ? 'mqttAuth.syncFailed'
            : status === 'partial'
              ? 'mqttAuth.syncPartial'
              : 'mqttAuth.syncUnknown'
      )}
    </Tag>
  );
  const connected = !error && current?.businessStatus === 'available';
  const activeSwitch = current?.switch && !['ready', 'cancelled'].includes(current.switch.state);
  const networkMode =
    (activeSwitch && current?.switch?.networkMode) || current?.connection?.networkMode || current?.network?.mode;
  const networkState = error ? 'unchecked' : current?.network?.state || 'unchecked';
  const diagnosticCode = !error
    ? (activeSwitch && current?.switch?.causeCode) ||
      current?.network?.errorCode ||
      (current?.connectionReason && connectionReasons[current.connectionReason] ? current.connectionReason : undefined)
    : undefined;
  const diagnosticKey = diagnosticCode ? connectionReasons[diagnosticCode] || 'fleet.edge.diag.prepare' : undefined;
  const connectBlocked = error
    ? 'fleet.edge.diag.status'
    : !current
      ? 'fleet.edge.diag.checking'
      : !current.enabled
        ? 'fleet.edge.diag.localDisabled'
        : undefined;
  const stage = connected
    ? 3
    : activeSwitch
      ? ['network', 'registration', 'verification'].indexOf(current.switch!.state)
      : 0;
  const summary = (log?: CloudSyncLogItem) => {
    if (!log) return '-';
    try {
      const details = JSON.parse(log.details || '{}');
      const stats = details.stats || {};
      return t('fleet.edge.userSummary', {
        created: stats.created || 0,
        updated: stats.updated || 0,
        disabled: stats.disabled || 0,
        deleted: stats.deleted || 0,
        conflicts: stats.conflicts || 0,
      });
    } catch {
      return log.summary || '-';
    }
  };
  const registrationPending = current?.registrationStatus === 'pending';
  const displayedConnection = (registrationPending && current?.requestedConnection) || current?.connection;
  const scopeLabel =
    !current?.collectionScope || error
      ? '—'
      : current.collectionScope.mode === 'selected'
        ? t('fleet.collections.selectedCount', { count: current.collectionScope.collectionIds.length })
        : t('fleet.collections.selectAll');
  const counts = storage?.definitions;
  const syncedDefinitions = counts ? `${counts.synced} / ${counts.total}` : '—';
  const metric = (label: string, value: string | number | undefined, failed = false) => (
    <div className={styles.branchInfoField}>
      <dt className={styles.branchInfoLabel}>{label}</dt>
      <dd className={failed ? styles.failed : styles.branchInfoValue}>{value ?? '—'}</dd>
    </div>
  );
  const detailGroups = (() => {
    if (!detail) return [];
    try {
      const value = JSON.parse(detail.details || '{}') as Record<string, unknown>;
      return ['createdUsers', 'updatedUsers', 'disabledUsers', 'deletedUsers', 'skippedUsers', 'conflicts']
        .map((key) => ({ key, rows: Array.isArray(value[key]) ? (value[key] as Record<string, unknown>[]) : [] }))
        .filter((group) => group.rows.length);
    } catch {
      return [];
    }
  })();
  return (
    <div className={styles.branchPanel}>
      <section className={styles.branchCard} aria-label={t('fleet.sync.connection')}>
        <div className={styles.branchBlockHeader}>
          <h2 className={styles.branchConnectionTitle}>{t('fleet.sync.connection')}</h2>
          <div className={styles.branchBlockActions}>
            {current?.registrationStatus === 'unconfigured' && !current.tokenConfigured && (
              <AuthButton
                type="primary"
                icon={<Plug {...toolbarIconProps} />}
                className={styles.branchPrimaryAction}
                auth={ButtonPermission['FleetConnection.configure']}
                disabled={saving || !!connectBlocked}
                onClick={openTokenForm}
              >
                {t('fleet.runtime.connect')}
              </AuthButton>
            )}
            <Button
              className={styles.branchMutedAction}
              icon={<Settings {...toolbarIconProps} />}
              onClick={() => setConnectionDetails(true)}
            >
              {t('nativeApp.connectionSettings')}
            </Button>
            <Button
              className={styles.branchSecondaryAction}
              icon={<Activity {...toolbarIconProps} />}
              onClick={() => setDiagnostics(true)}
            >
              {t('nativeApp.diagnostics')}
            </Button>
          </div>
        </div>
        <div className={styles.branchMetaGrid}>
          {field(t('fleet.sync.centerAddress'), displayedConnection?.centerUrl || '—')}
          {field(
            t('mqttAuth.connectionStatus'),
            <Tag
              className={styles.connectionStatus}
              color={!current || error ? 'default' : connected ? 'success' : 'warning'}
            >
              {t(
                !current || error
                  ? 'fleet.collaboration.unchecked'
                  : connected
                    ? 'fleet.ui.connected'
                    : registrationPending
                      ? 'fleet.edge.registrationPending'
                      : 'fleet.sync.notConnected'
              )}
            </Tag>
          )}
          {field(t('fleet.edge.lastChecked'), time(current?.checkedAt))}
        </div>
        {connectBlocked && <Alert type={error ? 'error' : 'info'} showIcon message={t(connectBlocked)} />}
        {activeSwitch ? (
          <Steps
            className={styles['connection-steps']}
            size="small"
            current={stage}
            status={error || current?.switch?.errorCode ? 'error' : connected ? 'finish' : 'process'}
            items={['network', 'registration', 'verification'].map((step) => ({
              title: t(`fleet.edge.stage.${step}`),
            }))}
          />
        ) : null}
        {!error && activeSwitch ? (
          <Alert
            type={current?.switch?.errorCode ? 'error' : 'info'}
            showIcon
            message={t(`fleet.edge.switch.${current?.switch?.state}`)}
            description={
              <Space direction="vertical">
                <span>
                  {t(
                    diagnosticKey ||
                      (current?.switch?.errorCode ? 'fleet.edge.switchFailed' : 'fleet.edge.diag.autoRetry')
                  )}
                </span>
                {diagnosticCode && (
                  <Typography.Text type="secondary">
                    {t('fleet.edge.diag.code', { code: diagnosticCode })}
                  </Typography.Text>
                )}
              </Space>
            }
          />
        ) : !error && diagnosticKey ? (
          <Alert type="warning" showIcon message={t(diagnosticKey)} />
        ) : current?.connectionReason === 'BUSINESS_REGISTRATION_PENDING' ? (
          <Alert type="warning" showIcon message={t('fleet.edge.registrationMismatch')} />
        ) : null}
        <Drawer
          width={720}
          title={t('nativeApp.connectionSettings')}
          open={connectionDetails}
          onClose={() => setConnectionDetails(false)}
        >
          <div className={styles['drawer-body']}>
            <Typography.Text>
              {current?.requestedConnection?.centerUrl ||
                current?.connection?.centerUrl ||
                t('fleet.sync.notConnected')}
            </Typography.Text>
            <AuthButton
              auth={ButtonPermission['FleetConnection.configure']}
              disabled={saving || !!connectBlocked}
              onClick={openTokenForm}
            >
              {t(current?.tokenConfigured ? 'fleet.edge.changeToken' : 'fleet.runtime.connect')}
            </AuthButton>
            <Space wrap>
              <Checkbox
                checked={!!current?.access?.allowSSH}
                disabled={saving || !current?.access || !hasPermission(ButtonPermission['FleetConnection.configure'])}
                onChange={(e) => void updateAccess('allowSSH', e.target.checked)}
              >
                {t('fleet.access.ssh')}
              </Checkbox>
              <Checkbox
                checked={!!current?.access?.allowMetadataWrite}
                disabled={saving || !current?.access || !hasPermission(ButtonPermission['FleetConnection.configure'])}
                onChange={(e) => void updateAccess('allowMetadataWrite', e.target.checked)}
              >
                {t('fleet.access.metadata')}
              </Checkbox>
              <HelpTooltip title={t('fleet.access.help')} />
              <Checkbox
                checked={!!current?.access?.allowControl}
                disabled={saving || !current?.access || !hasPermission(ButtonPermission['FleetConnection.configure'])}
                onChange={(e) => void updateAccess('allowControl', e.target.checked)}
              >
                {t('fleet.access.control')}
              </Checkbox>
              <HelpTooltip title={t('fleet.access.controlHelp')} />
            </Space>
          </div>
        </Drawer>
        <Drawer width={640} title={t('nativeApp.diagnostics')} open={diagnostics} onClose={() => setDiagnostics(false)}>
          <div className={styles['drawer-body']}>
            <div className={styles['drawer-toolbar']}>
              <Button icon={<Renew size={16} />} loading={loading} onClick={() => void load()}>
                {t('common.refresh')}
              </Button>
            </div>
            {diagnosticKey && <Alert type="warning" showIcon message={t(diagnosticKey)} />}
            {diagnosticCode && (
              <Typography.Text type="secondary">{t('fleet.edge.diag.code', { code: diagnosticCode })}</Typography.Text>
            )}
            <div className={styles.grid}>
              {field(
                t('fleet.sync.centerAddress'),
                <span>{current?.requestedConnection?.centerUrl || current?.connection?.centerUrl || '-'}</span>
              )}
              {field(t('fleet.collaboration.nodeId'), displayedConnection?.nodeId || '—')}
              {field(
                t('mqttAuth.connectionStatus'),
                <Tag color={connected ? 'success' : current?.connection ? 'warning' : 'default'}>
                  {t(
                    error
                      ? 'fleet.collaboration.unchecked'
                      : connected
                        ? 'fleet.ui.connected'
                        : current?.connectionReason === 'FLEET_BUSINESS_CHECK_TIMEOUT'
                          ? 'fleet.edge.checkTimeout'
                          : current?.connection
                            ? 'fleet.edge.disconnected'
                            : 'fleet.sync.notConnected'
                  )}
                </Tag>
              )}
              {networkMode !== 'direct' &&
                field(
                  t('fleet.edge.network.title'),
                  <Space>
                    <Tag color={networkState === 'online' ? 'success' : diagnosticCode ? 'error' : 'default'}>
                      {t(
                        `fleet.edge.network.${['online', 'offline', 'unjoined', 'initializing', 'unavailable'].includes(networkState) ? networkState : 'unchecked'}`
                      )}
                    </Tag>
                    <HelpTooltip title={t('fleet.edge.network.help')} />
                  </Space>
                )}
              {networkMode !== 'direct' && field(t('fleet.edge.network.checked'), time(current?.network?.checkedAt))}
              {field(t('fleet.edge.lastChecked'), time(current?.checkedAt || current?.switch?.updatedAt))}
              {field(
                t('fleet.collaboration.registration'),
                <Tag>
                  {t(
                    current?.registrationStatus === 'registered'
                      ? 'fleet.collaboration.registeredStatus'
                      : current?.registrationStatus === 'pending'
                        ? 'fleet.edge.registrationPending'
                        : 'fleet.collaboration.unregistered'
                  )}
                </Tag>
              )}
              {field(
                t('fleet.edge.registeredAt'),
                time(current?.registrationStatus === 'registered' ? current.connection?.registeredAt : undefined)
              )}
            </div>
          </div>
        </Drawer>
      </section>
      <section className={styles.branchDataSync} aria-label={t('fleet.sync.dataSync')}>
        <h2 className={styles.branchDataSyncTitle}>{t('fleet.sync.dataSync')}</h2>
        {current?.connectionReason === 'BUSINESS_REGISTRATION_PENDING' && current.connection && (
          <Alert
            type="info"
            showIcon
            message={t('fleet.edge.previousCenterResults', { center: current.connection.centerUrl })}
          />
        )}
        <section className={styles.branchCard} aria-label={t('fleet.edge.collectionSync')}>
          <div className={styles.branchBlockHeader}>
            <h3 className={styles.branchBlockTitle}>
              <Upload {...toolbarIconProps} />
              {t('fleet.edge.collectionSync')}
              <HelpTooltip title={t('fleet.edge.collectionHelp')} />
            </h3>
            <div className={styles.branchBlockActions}>
              <Button
                className={styles.branchSecondaryAction}
                icon={<FileSearch {...toolbarIconProps} />}
                onClick={() => setSyncDetails(true)}
              >
                {t('common.detail')}
              </Button>
              <AuthButton
                className={styles.branchDarkAction}
                icon={<Inspect {...toolbarIconProps} />}
                auth={ButtonPermission['FleetConnection.configure']}
                disabled={error || !current?.collectionScope || !storage}
                onClick={openSyncSettings}
              >
                {t('nativeApp.syncSettings')}
              </AuthButton>
            </div>
          </div>
          {storageError && <Alert type="error" message={t('collectionStorage.loadFailed')} />}
          <div className={styles.branchMetaGrid}>
            {field(t('fleet.collections.scope'), scopeLabel)}
            {field(t('collectionStorage.title'), storage ? t(`collectionStorage.${storage.policy.mode}`) : '—')}
          </div>
          <div className={styles.branchSectionDividerLine} aria-hidden />
          <dl className={styles.metrics}>
            {metric(t('collectionStorage.definitions'), syncedDefinitions)}
            {metric(t('fleet.edge.definitionFailures'), counts?.failed, !!counts?.failed)}
            {metric(t('collectionStorage.pending'), storage?.delivery.pending)}
            {metric(t('fleet.edge.deliveryFailures'), storage?.delivery.failed, !!storage?.delivery.failed)}
          </dl>
          <Drawer width={720} title={t('fleet.sync.dataSync')} open={syncDetails} onClose={() => setSyncDetails(false)}>
            <div className={styles['drawer-body']}>
              <div className={styles.grid}>
                {field(t('fleet.collections.scope'), scopeLabel)}
                {field(t('collectionStorage.title'), storage ? t(`collectionStorage.${storage.policy.mode}`) : '-')}
                {field(t('collectionStorage.definitions'), syncedDefinitions)}
                {field(t('collectionStorage.syncPending'), counts?.pending ?? '—')}
                {field(t('fleet.edge.definitionFailures'), counts?.failed ?? '—')}
                {field(t('collectionStorage.pending'), storage?.delivery.pending ?? '—')}
                {field(t('fleet.edge.deliveryFailures'), storage?.delivery.failed ?? '—')}
                {field(t('collectionStorage.committed'), storage?.delivery.committed ?? '—')}
              </div>
              {!!storage?.historicalDelivery?.pending && (
                <Space wrap>
                  <Tag color="warning">
                    {t('fleet.edge.historicalDelivery', { count: storage.historicalDelivery.pending })}
                  </Tag>
                  <HelpTooltip title={t('fleet.edge.historicalDeliveryHelp')} />
                </Space>
              )}
            </div>
          </Drawer>
        </section>
        <section className={styles.branchCard} aria-label={t('fleet.sync.accountsTab')}>
          <div className={styles.branchBlockHeader}>
            <h3 className={styles.branchBlockTitle}>
              <Download {...toolbarIconProps} />
              {t('fleet.sync.accountsTab')}
              <HelpTooltip title={t('fleet.edge.userHelp')} />
            </h3>
            <div className={styles.branchBlockActions}>
              <Button
                className={styles.branchSecondaryAction}
                icon={<FileClock {...toolbarIconProps} />}
                disabled={!nodeId}
                onClick={() => openDrawer('history')}
              >
                {t('cluster.userSyncHistory')}
              </Button>
              <Button
                className={styles.branchSecondaryAction}
                icon={<Users {...toolbarIconProps} />}
                disabled={!nodeId}
                onClick={() => openDrawer('users')}
              >
                {t('cluster.syncedAccountList')}
              </Button>
              <Button
                type="text"
                aria-label={t('common.refresh')}
                title={t('common.refresh')}
                icon={<Renew size={16} />}
                disabled={!nodeId}
                onClick={() => setAccountRefresh((value) => value + 1)}
              />
            </div>
          </div>
          {syncError && <Alert type="error" showIcon message={t('fleet.resources.unavailable')} />}
          <div className={styles.branchMetaGrid}>
            {field(t('fleet.sync.lastSync'), time(latest?.createdTime))}
            {field(t('mqttAuth.syncStatus'), latest ? syncStatus(latest.status) : '—')}
            {field(
              t('fleet.sync.result'),
              latest ? summary(latest) : lastAccountSync && !syncError ? t('fleet.edge.noAccountSync') : '—'
            )}
          </div>
        </section>
      </section>
      <Drawer
        rootClassName={styles.accountDrawer}
        width={900}
        title={t(drawer === 'users' ? 'cluster.syncedAccountList' : 'cluster.userSyncHistory')}
        open={!!drawer}
        onClose={() => setDrawer(undefined)}
        destroyOnHidden
      >
        <div className={styles['drawer-body']}>
          <div className={styles['drawer-toolbar']}>
            <Button
              icon={<Renew size={16} />}
              loading={drawerBusy}
              onClick={() => drawer && void loadDrawer(drawer, page)}
            >
              {t('common.refresh')}
            </Button>
          </div>
          {drawerError && <Alert type="error" showIcon message={t('fleet.resources.unavailable')} />}
          {drawer === 'users' ? (
            <ProTable
              rowKey="userId"
              loading={drawerBusy}
              dataSource={users}
              pagination={{ pageSize: 10 }}
              columns={[
                { title: t('account.account'), dataIndex: 'userName' },
                { title: t('account.email'), dataIndex: 'email', render: (value: string) => value || '-' },
                {
                  title: t('account.role'),
                  dataIndex: 'roleList',
                  render: (roles: FleetSyncedUser['roleList']) =>
                    roles.map((role) => role.roleName || role.roleCode).join(', ') || '-',
                },
                {
                  title: t('common.status'),
                  dataIndex: 'status',
                  render: (status: number) => (
                    <Tag color={status ? 'success' : 'default'}>
                      {t(status ? 'fleet.edge.enabled' : 'fleet.edge.disabled')}
                    </Tag>
                  ),
                },
              ]}
            />
          ) : (
            <ProTable
              rowKey="id"
              loading={drawerBusy}
              dataSource={logs}
              pagination={{ current: page, total, pageSize: 10, onChange: (next) => void loadDrawer('history', next) }}
              columns={[
                { title: t('mqttAuth.syncTime'), dataIndex: 'createdTime', render: time },
                { title: t('mqttAuth.syncStatus'), dataIndex: 'status', render: syncStatus },
                { title: t('fleet.sync.result'), render: (_: unknown, log: CloudSyncLogItem) => summary(log) },
                {
                  title: t('common.operation'),
                  render: (_: unknown, log: CloudSyncLogItem) => (
                    <Button type="link" onClick={() => setDetail(log)}>
                      {t('common.detail')}
                    </Button>
                  ),
                },
              ]}
            />
          )}
        </div>
      </Drawer>
      <Drawer
        rootClassName={styles.accountDrawer}
        width={720}
        title={t('common.detail')}
        open={!!detail}
        onClose={() => setDetail(undefined)}
        destroyOnHidden
      >
        <Space direction="vertical" style={{ width: '100%' }}>
          <Typography.Text>{time(detail?.createdTime)}</Typography.Text>
          {syncStatus(detail?.status)}
          <Typography.Text>{summary(detail)}</Typography.Text>
          {detail?.errorMessage && <Alert type="error" message={detail.errorMessage} />}
        </Space>
        {detailGroups.map((group) => (
          <div className={styles['detail-group']} key={group.key}>
            <Typography.Title level={5}>{t(`fleet.edge.${group.key}`)}</Typography.Title>
            <ProTable
              rowKey={(_, index) => String(index)}
              pagination={false}
              dataSource={group.rows}
              columns={[
                {
                  title: t('account.account'),
                  render: (_: unknown, row: Record<string, unknown>) =>
                    String(row.username || row.userName || row.sourceUserName || row.userId || '-'),
                },
                {
                  title: t('fleet.sync.result'),
                  render: (_: unknown, row: Record<string, unknown>) =>
                    String(row.reason || row.errorMessage || row.targetRoleName || '-'),
                },
              ]}
            />
          </div>
        ))}
      </Drawer>
      <Modal
        title={t('fleet.edge.collectionSync')}
        open={!!scopeEditing}
        onCancel={() => setScopeEditing(undefined)}
        onOk={() => void saveScope()}
        okText={t('common.save')}
        okButtonProps={{ disabled: !storage || error }}
        confirmLoading={saving}
        destroyOnHidden
      >
        <Form layout="vertical">
          <Form.Item
            label={
              <Space>
                {t('fleet.collections.scope')}
                <HelpTooltip title={t('fleet.collections.scopeHelp')} />
              </Space>
            }
          >
            <FleetCollectionSelection
              value={scopeEditing}
              onChange={(value) => setScopeEditing((previous) => (previous ? { ...previous, ...value } : undefined))}
            />
          </Form.Item>
          <Form.Item
            label={
              <Space>
                {t('collectionStorage.title')}
                <HelpTooltip title={t('collectionStorage.hint')} />
              </Space>
            }
            validateStatus={!storageDraft.length ? 'error' : undefined}
            help={!storageDraft.length ? t('collectionStorage.atLeastOne') : undefined}
          >
            <Checkbox.Group
              value={storageDraft}
              disabled={saving || !hasPermission(ButtonPermission['uns.metadataWrite'])}
              onChange={(values) => {
                setStorageDraft(values.map(String));
                setConfigError('');
              }}
              options={[
                { value: 'local', label: t('collectionStorage.localLabel') },
                { value: 'center', label: t('collectionStorage.centerLabel'), disabled: !storage?.connected },
              ]}
            />
          </Form.Item>
        </Form>
        {(configError || storageError || error) && (
          <Alert
            type="error"
            showIcon
            message={configError || t(storageError ? 'collectionStorage.loadFailed' : 'fleet.edge.diag.status')}
          />
        )}
      </Modal>
      <Modal
        title={t('fleet.runtime.connectTitle')}
        open={open}
        onCancel={() => setOpen(false)}
        onOk={() => void save()}
        confirmLoading={saving}
        okButtonProps={{ disabled: !!connectBlocked }}
        destroyOnHidden
      >
        {(connectionError || connectBlocked) && (
          <Alert type="error" showIcon message={t(connectionError || connectBlocked!)} />
        )}
        <Form
          form={form}
          layout="vertical"
          preserve={false}
          initialValues={{
            collectionScope: current?.collectionScope || { mode: 'all', collectionIds: [], revision: 0 },
            allowSSH: current?.access?.allowSSH ?? true,
            allowMetadataWrite: current?.access?.allowMetadataWrite ?? true,
            allowControl: current?.access?.allowControl ?? false,
          }}
        >
          {current?.tokenConfigured && !replacingToken ? (
            <Form.Item label={t('fleet.runtime.accessToken')}>
              <Space>
                <Tag>{t('fleet.edge.tokenConfigured')}</Tag>
                <HelpTooltip title={t('fleet.edge.tokenRetained')} />
                <Button onClick={() => setReplacingToken(true)}>{t('fleet.edge.replaceToken')}</Button>
              </Space>
            </Form.Item>
          ) : (
            <Form.Item
              name="accessToken"
              label={t('fleet.runtime.accessToken')}
              rules={[{ required: true, whitespace: true, message: t('fleet.runtime.tokenRequired') }]}
            >
              <Input.Password autoComplete="new-password" placeholder={t('fleet.runtime.tokenPlaceholder')} />
            </Form.Item>
          )}
          <Form.Item name="collectionScope" label={t('fleet.collections.scope')}>
            <FleetCollectionSelection />
          </Form.Item>
          <Form.Item name="allowSSH" valuePropName="checked">
            <Checkbox>{t('fleet.access.ssh')}</Checkbox>
          </Form.Item>
          <Form.Item name="allowMetadataWrite" valuePropName="checked">
            <Checkbox>{t('fleet.access.metadata')}</Checkbox>
          </Form.Item>
          <Form.Item name="allowControl" valuePropName="checked">
            <Checkbox>{t('fleet.access.control')}</Checkbox>
          </Form.Item>
          <HelpTooltip title={t('fleet.access.controlHelp')} />
        </Form>
      </Modal>
    </div>
  );
}
