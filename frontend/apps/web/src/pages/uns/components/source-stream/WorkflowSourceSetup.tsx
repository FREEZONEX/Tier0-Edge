import { useEffect, useState } from 'react';
import { Alert, Select, Spin } from 'antd';
import { Plug } from '@carbon/icons-react';
import { sourceStreamApi, type Source } from '@/apis/core-api/source-stream';
import { useTranslate } from '@/hooks';
import ComButton from '@/components/com-button';
import SourcePicker from './SourcePicker';
import SourceCreateDialog from './SourceCreateDialog';
import { ButtonPermission } from '@/common-types/button-permission';
import { sourceInsertionModes } from './program-document';
import { workflowSource, type WorkflowSource, type WorkflowConnection } from './program-workflow';
import HelpTooltip from '@/components/help-tooltip';
import styles from './workflow.module.scss';

export default function WorkflowSourceSetup({
  value,
  event,
  onChange,
  operation,
  connections = [],
}: {
  value?: WorkflowSource;
  event: boolean;
  onChange: (source: WorkflowSource, operation?: 'read' | 'request') => void;
  operation?: 'read' | 'request';
  connections?: WorkflowConnection[];
}) {
  const t = useTranslate();
  const [enabling, setEnabling] = useState(false);
  const [picker, setPicker] = useState(false);
  const [creating, setCreating] = useState(false);
  const [source, setSource] = useState<Source>();
  const [loading, setLoading] = useState(false);
  const [failed, setFailed] = useState(false);
  const [retry, setRetry] = useState(0);
  useEffect(() => {
    let alive = true;
    setSource(undefined);
    setFailed(false);
    if (value?.id) {
      setLoading(true);
      sourceStreamApi
        .source(Number(value.id))
        .then((item) => {
          if (alive) setSource(item);
        })
        .catch(() => {
          if (alive) setFailed(true);
        })
        .finally(() => {
          if (alive) setLoading(false);
        });
    }
    return () => {
      alive = false;
    };
  }, [value?.id, retry]);
  const isHTTP = (item: Source) =>
    ['http-api', 'oauth2'].includes(item.protocol) && !item.document.sourceType && item.document.role !== 'server';
  const resources = (source?.resources ?? []).filter(
    (item) => source && sourceInsertionModes(source, item.id).includes(event ? 'source-event' : 'read')
  );
  const eligible = (item: Source) =>
    (!event && isHTTP(item)) ||
    item.resources.some((resource) =>
      sourceInsertionModes(item, resource.id).includes(event ? 'source-event' : 'read')
    );
  const probe = async () => {
    if (!source) return;
    setEnabling(true);
    try {
      await sourceStreamApi.probe(source.id);
      setSource(await sourceStreamApi.source(source.id));
    } catch {
      /* 请求层报告错误；保留来源配置。 */
    } finally {
      setEnabling(false);
    }
  };
  return (
    <div className={styles['form-stack']}>
      <label className={styles.field}>
        <span className={styles['inline-row']}>
          {t('SourceStream.source')}
          <HelpTooltip title={t('Workflow.connectionHint')} />
        </span>
        <ComButton onClick={() => setPicker(true)} icon={<Plug />}>
          {value?.name || t('SourceStream.chooseSource')}
        </ComButton>
      </label>
      {connections.some((item) => (event ? item.mode === 'source-event' : ['read', 'request'].includes(item.mode))) && (
        <Select
          aria-label={t('Workflow.reuseConnection')}
          placeholder={t('Workflow.reuseConnection')}
          value={undefined}
          options={connections
            .filter((item) => (event ? item.mode === 'source-event' : ['read', 'request'].includes(item.mode)))
            .map((item) => ({ value: item.alias, label: item.name + ' · ' + item.alias }))}
          onChange={(alias) => {
            const connection = connections.find((item) => item.alias === alias);
            if (!connection) return;
            onChange(
              { id: connection.id, name: connection.name, resource: connection.resource, qos: connection.qos },
              connection.mode === 'request' ? 'request' : 'read'
            );
          }}
        />
      )}
      <ComButton auth={ButtonPermission['uns.sourceManage']} onClick={() => setCreating(true)}>
        {t('SourceStream.addSource')}
      </ComButton>
      {creating && (
        <SourceCreateDialog
          open
          onClose={() => setCreating(false)}
          onCreated={(item) => {
            onChange(workflowSource(item), isHTTP(item) ? 'request' : 'read');
            setCreating(false);
          }}
        />
      )}
      {picker && (
        <SourcePicker
          onClose={() => setPicker(false)}
          disabledReason={(item) => (!eligible(item) ? t('Workflow.unsupportedSource') : undefined)}
          onSelect={(item) => {
            onChange(workflowSource(item), isHTTP(item) ? 'request' : 'read');
            setPicker(false);
          }}
        />
      )}
      {loading && <Spin />}
      {failed && (
        <Alert
          type="error"
          message={t('SourceStream.loadFailed')}
          action={<ComButton onClick={() => setRetry((value) => value + 1)}>{t('SourceStream.refresh')}</ComButton>}
        />
      )}
      {source && (
        <>
          {source.health !== 'healthy' && (
            <Alert
              type="info"
              message={t('SourceStream.verifyBeforeRun')}
              action={
                <ComButton auth={ButtonPermission['uns.sourceManage']} loading={enabling} onClick={probe}>
                  {t('SourceStream.testConnection')}
                </ComButton>
              }
            />
          )}
          {(event || operation === 'read') && (
            <label className={styles.field}>
              <span>{t('SourceStream.nativeResource')}</span>
              <Select
                showSearch
                optionFilterProp="label"
                aria-label={t('SourceStream.nativeResource')}
                placeholder={t('SourceStream.nativeResource')}
                value={value?.resource || undefined}
                onChange={(resource) => onChange(workflowSource(source, resource))}
                options={resources.map((resource) => ({
                  value: resource.id,
                  label: `${resource.name} · ${resource.id}`,
                }))}
              />
            </label>
          )}
          {!eligible(source) && <Alert type="warning" message={t('Workflow.unsupportedSource')} />}
        </>
      )}
    </div>
  );
}
