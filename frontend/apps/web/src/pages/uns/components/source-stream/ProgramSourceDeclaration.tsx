import { useState } from 'react';
import { Typography } from 'antd';
import { Plug } from '@carbon/icons-react';
import ComButton from '@/components/com-button';
import HelpTooltip from '@/components/help-tooltip';
import { useTranslate } from '@/hooks';
import WorkflowConnectionDialog from './WorkflowConnectionDialog';
import { sourceConnectionDeclaration } from './workflow-connections';
import type { WorkflowConnection } from './program-workflow';
import styles from './workflow.module.scss';

export default function ProgramSourceDeclaration() {
  const t = useTranslate();
  const [open, setOpen] = useState(false);
  const [connection, setConnection] = useState<WorkflowConnection>();
  return (
    <div className={styles['form-stack']}>
      <div className={styles['inline-row']}>
        <ComButton icon={<Plug />} onClick={() => setOpen(true)}>
          {t('Workflow.addConnection')}
        </ComButton>
        <HelpTooltip title={t('Workflow.declarationHint')} />
      </div>
      {connection && (
        <>
          <span>{t('Workflow.connectionDeclaration')}</span>
          <Typography.Paragraph copyable={{ text: sourceConnectionDeclaration(connection) }}>
            <code>{sourceConnectionDeclaration(connection)}</code>
          </Typography.Paragraph>
        </>
      )}
      {open && (
        <WorkflowConnectionDialog
          aliases={[]}
          onClose={() => setOpen(false)}
          onSave={(value) => {
            setConnection(value);
            setOpen(false);
          }}
        />
      )}
    </div>
  );
}
