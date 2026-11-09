import { useState } from 'react';
import { Alert, Input, Select, Spin } from 'antd';
import { useRequest } from 'ahooks';
import { Plug } from '@carbon/icons-react';
import type { Source } from '@/apis/core-api/source-stream';
import { useSourceStreamApi } from '@/apis/core-api/source-stream-scope';
import ComButton from '@/components/com-button';
import HelpTooltip from '@/components/help-tooltip';
import ProModal from '@/components/pro-modal';
import { useTranslate } from '@/hooks';
import SourcePicker from './SourcePicker';
import SourceCreateDialog from './SourceCreateDialog';
import { sourceInsertionModes } from './program-document';
import { validConnectionAlias, type WorkflowConnection } from './program-workflow';
import { connectionFromSource, isHTTPConnection } from './workflow-connections';
import styles from './workflow.module.scss';

export default function WorkflowConnectionDialog({
  value,
  aliases,
  onSave,
  onClose,
}: {
  value?: WorkflowConnection;
  aliases: string[];
  onSave: (value: WorkflowConnection) => void;
  onClose: () => void;
}) {
  const sourceStreamApi = useSourceStreamApi();
  const t = useTranslate();
  const [picker, setPicker] = useState(!value);
  const [creating, setCreating] = useState(false);
  const [selected, setSelected] = useState<Source>();
  const [alias, setAlias] = useState(() => {
    if (value) return value.alias;
    let index = 1;
    while (aliases.includes('source' + index)) index++;
    return 'source' + index;
  });
  const [resource, setResource] = useState(value?.resource ?? '');
  const { data, loading, error, refresh } = useRequest(() => sourceStreamApi.source(Number(value?.id)), {
    ready: !!value,
  });
  const source = selected ?? data;
  const resources =
    source?.resources.filter((item) => sourceInsertionModes(source, item.id).some((mode) => mode !== 'http-server')) ??
    [];
  const aliasOK = validConnectionAlias(alias) && !aliases.includes(alias);
  const connection = source && connectionFromSource(source, alias, resource);
  const valid = aliasOK && !!connection;
  return (
    <ProModal
      open
      title={t(value ? 'Workflow.changeConnection' : 'Workflow.addConnection')}
      onCancel={onClose}
      width={560}
      footer={
        <ComButton
          className={styles['primary-button']}
          disabled={!valid}
          onClick={() => connection && valid && onSave(connection)}
        >
          {t('common.confirm')}
        </ComButton>
      }
    >
      <div className={styles['form-stack']}>
        <label className={styles.field}>
          <span className={styles['inline-row']}>
            {t('SourceStream.source')}
            <HelpTooltip title={t('Workflow.connectionHint')} />
          </span>
          <ComButton icon={<Plug />} onClick={() => setPicker(true)}>
            {source?.name ?? value?.name ?? t('SourceStream.chooseSource')}
          </ComButton>
        </label>
        {loading && !selected && <Spin />}
        {error && !selected && (
          <Alert
            type="error"
            message={t('SourceStream.loadFailed')}
            action={<ComButton onClick={refresh}>{t('SourceStream.refresh')}</ComButton>}
          />
        )}
        <label className={styles.field}>
          <span>{t('Workflow.connectionAlias')}</span>
          <Input
            aria-label={t('Workflow.connectionAlias')}
            value={alias}
            disabled={!!value}
            placeholder="crm"
            status={alias && !aliasOK ? 'error' : undefined}
            onChange={(event) => setAlias(event.target.value)}
          />
          {alias && !aliasOK && <span role="alert">{t('Workflow.issue.connectionAlias')}</span>}
        </label>
        {source && !isHTTPConnection(source) && (
          <label className={styles.field}>
            <span>{t('SourceStream.nativeResource')}</span>
            <Select
              showSearch
              optionFilterProp="label"
              value={resource || undefined}
              aria-label={t('SourceStream.nativeResource')}
              placeholder={t('SourceStream.nativeResource')}
              onChange={setResource}
              options={resources.map((item) => ({ value: item.id, label: item.name + ' · ' + item.id }))}
            />
          </label>
        )}
        {source && <span className={styles['muted-text']}>{t('Workflow.sourceOwnsAuth')}</span>}
      </div>
      {picker && (
        <SourcePicker
          onClose={() => setPicker(false)}
          onCreate={() => {
            setPicker(false);
            setCreating(true);
          }}
          disabledReason={(item) =>
            isHTTPConnection(item) ||
            item.resources.some((resource) =>
              sourceInsertionModes(item, resource.id).some((mode) => mode !== 'http-server')
            )
              ? undefined
              : t('Workflow.unsupportedSource')
          }
          onSelect={(item) => {
            setSelected(item);
            const available = item.resources.filter((resource) =>
              sourceInsertionModes(item, resource.id).some((mode) => mode !== 'http-server')
            );
            setResource(!isHTTPConnection(item) && available.length === 1 ? available[0].id : '');
            setPicker(false);
          }}
        />
      )}
      {creating && (
        <SourceCreateDialog
          open
          onClose={() => setCreating(false)}
          onCreated={(item) => {
            setSelected(item);
            setResource('');
            setCreating(false);
          }}
        />
      )}
    </ProModal>
  );
}
