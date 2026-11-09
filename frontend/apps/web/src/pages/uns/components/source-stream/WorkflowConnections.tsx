import { useState } from 'react';
import { App, Empty } from 'antd';
import { Add, Plug, TrashCan } from '@carbon/icons-react';
import { ButtonPermission } from '@/common-types/button-permission';
import ComButton from '@/components/com-button';
import HelpTooltip from '@/components/help-tooltip';
import ProModal from '@/components/pro-modal';
import { openConfirmModal } from '@/components/confirm-modal';
import { useTranslate } from '@/hooks';
import type { ProgramWorkflow, WorkflowConnection } from './program-workflow';
import { workflowUsedSources } from './workflow-connections';
import WorkflowConnectionDialog from './WorkflowConnectionDialog';
import styles from './workflow.module.scss';

export default function WorkflowConnections({
  workflow,
  readOnly,
  onChange,
  onClose,
}: {
  workflow: ProgramWorkflow;
  readOnly: boolean;
  onChange: (connections: WorkflowConnection[]) => void;
  onClose: () => void;
}) {
  const t = useTranslate();
  const { modal } = App.useApp();
  const [editing, setEditing] = useState<WorkflowConnection | 'new'>();
  const connections = workflow.connections ?? [];
  const used = workflowUsedSources(workflow);
  return (
    <>
      <ProModal open title={t('Workflow.connections')} onCancel={onClose} footer={null} width={680}>
        <div className={styles['form-stack']}>
          <div className={styles['connection-heading']}>
            <span className={styles['inline-row']}>
              {t('Workflow.scriptConnections')}
              <HelpTooltip title={t('Workflow.connectionHint')} />
            </span>
            {!readOnly && (
              <ComButton auth={ButtonPermission['uns.sources']} icon={<Add />} onClick={() => setEditing('new')}>
                {t('Workflow.addConnection')}
              </ComButton>
            )}
          </div>
          {!connections.length && (
            <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={t('Workflow.noConnections')} />
          )}
          {connections.map((connection) => (
            <div className={styles['connection-row']} key={connection.alias}>
              <Plug size={20} />
              <div className={styles['step-copy']}>
                <strong>{connection.name}</strong>
                <code>connections.{connection.alias}</code>
                {connection.resource && <span className={styles['muted-text']}>{connection.resource}</span>}
              </div>
              {!readOnly && (
                <>
                  <ComButton size="small" onClick={() => setEditing(connection)}>
                    {t('Workflow.changeConnection')}
                  </ComButton>
                  <ComButton
                    type="text"
                    icon={<TrashCan />}
                    aria-label={t('Workflow.removeConnection')}
                    onClick={() =>
                      openConfirmModal(modal, {
                        title: t('Workflow.removeConnection'),
                        content: t('Workflow.removeConnectionHint', { alias: connection.alias }),
                        okText: t('Workflow.removeConnection'),
                        cancelText: t('common.cancel'),
                        danger: true,
                        onOk: () => onChange(connections.filter((item) => item.alias !== connection.alias)),
                      })
                    }
                  />
                </>
              )}
            </div>
          ))}
          {!!used.length && (
            <>
              <div className={styles['connection-heading']}>
                <span>{t('Workflow.stepConnections')}</span>
                <HelpTooltip title={t('Workflow.stepConnectionsHint')} />
              </div>
              {used.map((source) => (
                <div key={source.id + '/' + source.resource} className={styles['connection-row']}>
                  <Plug size={20} />
                  <div className={styles['step-copy']}>
                    <strong>{source.name}</strong>
                    {source.resource && <span className={styles['muted-text']}>{source.resource}</span>}
                  </div>
                </div>
              ))}
            </>
          )}
        </div>
      </ProModal>
      {editing && (
        <WorkflowConnectionDialog
          value={editing === 'new' ? undefined : editing}
          aliases={connections
            .filter((item) => editing === 'new' || item.alias !== editing.alias)
            .map((item) => item.alias)}
          onClose={() => setEditing(undefined)}
          onSave={(connection) => {
            onChange(
              editing === 'new'
                ? [...connections, connection]
                : connections.map((item) => (item.alias === editing.alias ? connection : item))
            );
            setEditing(undefined);
          }}
        />
      )}
    </>
  );
}
