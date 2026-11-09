import { useEffect, useMemo, useState } from 'react';
import { Help } from '@carbon/icons-react';
import { Button, Checkbox, Collapse, Input } from 'antd';
import HelpTooltip from '@/components/help-tooltip';
import { Add, TrashCan } from '@/components/lucide-icon/carbon';
import { useTranslate } from '@/hooks';
import { APP_NETWORK_ALIAS_MAX_LENGTH, appNetworkAliasError } from '@/utils/app-network-alias';
import styles from './AppBundleModal.module.scss';

const SCHEMA_RE = /^[a-z0-9_]{1,63}$/;
const RESERVED_SCHEMAS = new Set([
  'public',
  'information_schema',
  'pg_catalog',
  'pg_toast',
  'pg_temp_1',
  'pg_toast_temp_1',
]);

export type SchemaNameIssue = '' | 'required' | 'invalid' | 'reserved';

// schemaNameIssue Schema 名轻校验（纯函数）：组件内即时提示与导入提交前拦截共用同一份规则
export const schemaNameIssue = (value: string): SchemaNameIssue => {
  const text = value.trim();
  if (!text) {
    return 'required';
  }
  if (!SCHEMA_RE.test(text)) {
    return 'invalid';
  }
  if (RESERVED_SCHEMAS.has(text) || text.startsWith('pg_') || text.startsWith('template')) {
    return 'reserved';
  }
  return '';
};
/** 平台自动注入且不允许用户修改/删除的变量（与后端 platformForcedEnvs 保持一致） */
const PLATFORM_FORCED_ENVS = new Set([
  'DATABASE_URL',
  'DB_SCHEMA',
  'PORT',
  'HOSTNAME',
  'TIER0_PROJECT_ID',
  'TIER0_API_KEY',
  'TIER0_API_HOST',
  'TIER0_MQTT_HOST',
  'TIER0_MQTT_PORT',
]);

export interface EnvVarItem {
  key: string;
  value: string;
}

export interface VolumeItem {
  hostPath: string;
  containerPath: string;
  readOnly?: boolean;
}

export interface AppAdvancedChangeValue {
  schemaName?: string;
  envVars: EnvVarItem[];
  volumes: VolumeItem[];
  /** 本次变更是否来自环境变量编辑（父层据此决定 envPolicy 是否 replace） */
  envTouched?: boolean;
  /** 本次变更是否来自磁盘映射编辑（父层据此决定 volumePolicy 是否 replace） */
  volumeTouched?: boolean;
}

/** 磁盘映射轻校验：整行留空允许（提交时按空行过滤）；填写过的行两个路径都必填且须以 / 开头 */
export const isVolumeRowValid = (item: Pick<VolumeItem, 'hostPath' | 'containerPath'>) => {
  const host = item.hostPath.trim();
  const container = item.containerPath.trim();
  if (host === '' && container === '') {
    return true;
  }
  return host.startsWith('/') && container.startsWith('/');
};

interface AppAdvancedFieldsProps {
  /** schema 默认值（首次导入时展示并允许编辑；Replace 场景传入 '' 且 readOnly） */
  networkAlias?: string;
  onNetworkAliasChange?: (value: string) => void;
  schemaDefault?: string;
  schemaReadOnly?: boolean;
  /** 默认展示的环境变量列表 */
  defaultEnvVars?: EnvVarItem[];
  /** 默认展示的磁盘映射列表 */
  defaultVolumes?: VolumeItem[];
  disabled?: boolean;
  onChange?: (value: AppAdvancedChangeValue) => void;
}

const AppAdvancedFields = ({
  networkAlias = '',
  onNetworkAliasChange,
  schemaDefault,
  schemaReadOnly,
  defaultEnvVars,
  defaultVolumes,
  disabled,
  onChange,
}: AppAdvancedFieldsProps) => {
  const formatMessage = useTranslate();
  const aliasError = appNetworkAliasError(networkAlias);
  const [schemaName, setSchemaName] = useState(schemaDefault ?? '');
  const [envVars, setEnvVars] = useState<EnvVarItem[]>(defaultEnvVars ?? []);
  const [volumes, setVolumes] = useState<VolumeItem[]>(defaultVolumes ?? []);
  const [schemaError, setSchemaError] = useState('');
  const schemaHint = formatMessage(
    'project.importApp.schemaHint',
    {},
    'Applied only on first install and cannot be changed afterwards'
  );
  const volumeHint = formatMessage(
    'project.importApp.volumeHint',
    {},
    'Map host directories into the app container. If untouched, the app keeps its current mappings when replaced.'
  );

  useEffect(() => {
    if (schemaDefault !== undefined && !schemaReadOnly) {
      setSchemaName(schemaDefault);
    }
  }, [schemaDefault, schemaReadOnly]);

  useEffect(() => {
    if (defaultEnvVars) {
      setEnvVars(defaultEnvVars);
    }
  }, [defaultEnvVars]);

  useEffect(() => {
    if (defaultVolumes) {
      setVolumes(defaultVolumes);
    }
  }, [defaultVolumes]);

  const emitChange = (patch: Partial<AppAdvancedChangeValue>) => {
    onChange?.({
      schemaName: patch.schemaName ?? schemaName,
      envVars: patch.envVars ?? envVars,
      volumes: patch.volumes ?? volumes,
      envTouched: patch.envTouched,
      volumeTouched: patch.volumeTouched,
    });
  };

  const validateSchema = (value: string): string => {
    switch (schemaNameIssue(value)) {
      case 'required':
        return formatMessage('project.importApp.schemaRequired', {}, 'Schema name is required');
      case 'invalid':
        return formatMessage(
          'project.importApp.schemaInvalid',
          {},
          'Schema name must be 1-63 chars of lowercase letters, digits or underscores'
        );
      case 'reserved':
        return formatMessage('project.importApp.schemaReserved', {}, 'Schema name is a reserved system name');
      default:
        return '';
    }
  };

  // 轻校验见 isVolumeRowValid；行不合法时标出缺失/非法的那个路径输入框
  const isVolumeInputInvalid = (item: VolumeItem, value: string) => {
    if (isVolumeRowValid(item)) {
      return false;
    }
    const text = value.trim();
    return text === '' || !text.startsWith('/');
  };

  const handleSchemaChange = (value: string) => {
    setSchemaName(value);
    setSchemaError(validateSchema(value));
    emitChange({ schemaName: value });
  };

  const updateEnv = (index: number, field: 'key' | 'value', value: string) => {
    const next = envVars.map((item, i) => (i === index ? { ...item, [field]: value } : item));
    setEnvVars(next);
    emitChange({ envVars: next, envTouched: true });
  };

  const addEnv = () => {
    const next = [...envVars, { key: '', value: '' }];
    setEnvVars(next);
    emitChange({ envVars: next, envTouched: true });
  };

  const removeEnv = (index: number) => {
    const next = envVars.filter((_, i) => i !== index);
    setEnvVars(next);
    emitChange({ envVars: next, envTouched: true });
  };

  const updateVolume = (index: number, patch: Partial<VolumeItem>) => {
    const next = volumes.map((item, i) => (i === index ? { ...item, ...patch } : item));
    setVolumes(next);
    emitChange({ volumes: next, volumeTouched: true });
  };

  const addVolume = () => {
    const next = [...volumes, { hostPath: '', containerPath: '', readOnly: false }];
    setVolumes(next);
    emitChange({ volumes: next, volumeTouched: true });
  };

  const removeVolume = (index: number) => {
    const next = volumes.filter((_, i) => i !== index);
    setVolumes(next);
    emitChange({ volumes: next, volumeTouched: true });
  };

  const hasInvalidVolume = volumes.some((item) => !isVolumeRowValid(item));

  const visibleEnvVars = useMemo(
    () =>
      envVars.map((item) => ({
        ...item,
        // 平台强制变量只读：展示但不可编辑/删除
        readOnly: PLATFORM_FORCED_ENVS.has(item.key),
      })),
    [envVars]
  );

  return (
    <Collapse
      ghost
      className={styles['advanced-collapse']}
      expandIconPosition="end"
      items={[
        {
          key: 'advanced',
          label: (
            <span className={styles['advanced-title']}>
              {formatMessage('project.importApp.advanced', {}, 'Advanced')}
            </span>
          ),
          children: (
            <div className={styles['advanced-grid']}>
              <div className={styles['advanced-full']}>
                <div className={styles['field-heading']}>
                  <label htmlFor="app-network-alias" className={styles['advanced-label']}>
                    {formatMessage('project.networkAlias')}
                  </label>
                  <HelpTooltip title={formatMessage('project.networkAliasHint')} />
                </div>
                <Input
                  id="app-network-alias"
                  value={networkAlias}
                  placeholder={formatMessage('project.networkAliasPlaceholder')}
                  maxLength={APP_NETWORK_ALIAS_MAX_LENGTH}
                  disabled={disabled}
                  status={aliasError ? 'error' : undefined}
                  onChange={(e) => onNetworkAliasChange?.(e.target.value)}
                />
                {aliasError ? <div className={styles['advanced-error']}>{formatMessage(aliasError)}</div> : null}
              </div>
              {!schemaReadOnly && (
                <div className={styles['advanced-full']}>
                  <div className={styles['field-heading']}>
                    <label className={styles['advanced-label']}>
                      {formatMessage('project.importApp.schema', {}, 'Schema')}
                    </label>
                    <HelpTooltip
                      icon={
                        <button type="button" className={styles['field-help-button']} aria-label={schemaHint}>
                          <Help size={16} />
                        </button>
                      }
                      title={schemaHint}
                    />
                  </div>
                  <Input
                    value={schemaName}
                    placeholder={formatMessage('project.importApp.schemaPlaceholder', {}, 'app_xxx')}
                    maxLength={63}
                    disabled={disabled}
                    status={schemaError ? 'error' : undefined}
                    onChange={(e) => handleSchemaChange(e.target.value)}
                  />
                  {schemaError ? <div className={styles['advanced-error']}>{schemaError}</div> : null}
                </div>
              )}
              <div className={styles['advanced-full']}>
                <div className={styles['advanced-section-header']}>
                  <label className={styles['advanced-label']}>
                    {formatMessage('project.importApp.envVars', {}, 'Environment Variables')}
                  </label>
                  <Button
                    className={styles['add-env-button']}
                    size="small"
                    icon={<Add size={14} />}
                    disabled={disabled}
                    onClick={addEnv}
                  >
                    {formatMessage('common.add', {}, 'Add')}
                  </Button>
                </div>
                <div className={styles['env-list']}>
                  {visibleEnvVars.length === 0 ? (
                    <div className={styles['env-empty']}>
                      {formatMessage(
                        'project.importApp.envEmpty',
                        {},
                        'No environment variables defined in the package'
                      )}
                    </div>
                  ) : (
                    visibleEnvVars.map((item, index) => (
                      <div key={index} className={styles['env-row']}>
                        <Input
                          value={item.key}
                          placeholder="KEY"
                          disabled={disabled || item.readOnly}
                          onChange={(e) => updateEnv(index, 'key', e.target.value)}
                        />
                        <Input
                          value={item.value}
                          placeholder={formatMessage('common.value', {}, 'Value')}
                          disabled={disabled || item.readOnly}
                          onChange={(e) => updateEnv(index, 'value', e.target.value)}
                        />
                        <Button
                          className={styles['env-remove-button']}
                          size="small"
                          type="text"
                          danger
                          icon={<TrashCan size={16} />}
                          disabled={disabled || item.readOnly}
                          onClick={() => removeEnv(index)}
                        />
                      </div>
                    ))
                  )}
                </div>
              </div>
              <div className={styles['advanced-full']}>
                <div className={styles['field-heading']}>
                  <label className={styles['advanced-label']}>
                    {formatMessage('project.importApp.volumes', {}, 'Disk Mappings')}
                  </label>
                  <HelpTooltip
                    icon={
                      <button type="button" className={styles['field-help-button']} aria-label={volumeHint}>
                        <Help size={16} />
                      </button>
                    }
                    title={volumeHint}
                  />
                </div>
                <div className={styles['env-list']}>
                  {volumes.length === 0 ? (
                    <div className={styles['env-empty']}>
                      {formatMessage('project.importApp.volumeEmpty', {}, 'No disk mappings configured')}
                    </div>
                  ) : (
                    volumes.map((item, index) => (
                      <div key={index} className={styles['volume-row']}>
                        <Input
                          value={item.hostPath}
                          placeholder={formatMessage('project.importApp.volumeHostPath', {}, 'Host path')}
                          disabled={disabled}
                          status={isVolumeInputInvalid(item, item.hostPath) ? 'error' : undefined}
                          onChange={(e) => updateVolume(index, { hostPath: e.target.value })}
                        />
                        <Input
                          value={item.containerPath}
                          placeholder={formatMessage('project.importApp.volumeContainerPath', {}, 'Container path')}
                          disabled={disabled}
                          status={isVolumeInputInvalid(item, item.containerPath) ? 'error' : undefined}
                          onChange={(e) => updateVolume(index, { containerPath: e.target.value })}
                        />
                        <Checkbox
                          className={styles['volume-readonly']}
                          checked={Boolean(item.readOnly)}
                          disabled={disabled}
                          onChange={(e) => updateVolume(index, { readOnly: e.target.checked })}
                        >
                          {formatMessage('project.importApp.volumeReadOnly', {}, 'Read-only')}
                        </Checkbox>
                        <Button
                          className={styles['env-remove-button']}
                          size="small"
                          type="text"
                          danger
                          icon={<TrashCan size={16} />}
                          disabled={disabled}
                          onClick={() => removeVolume(index)}
                        />
                      </div>
                    ))
                  )}
                </div>
                {hasInvalidVolume ? (
                  <div className={styles['advanced-error']}>
                    {formatMessage(
                      'project.importApp.volumePathInvalid',
                      {},
                      'Host and container paths must be absolute paths starting with "/"'
                    )}
                  </div>
                ) : null}
                <div>
                  <Button
                    className={styles['add-env-button']}
                    size="small"
                    icon={<Add size={14} />}
                    disabled={disabled}
                    onClick={addVolume}
                  >
                    {formatMessage('project.importApp.volumeAdd', {}, 'Add Mapping')}
                  </Button>
                </div>
              </div>
            </div>
          ),
        },
      ]}
    />
  );
};

export default AppAdvancedFields;
