import type { FC } from 'react';
import { Dropdown } from 'antd';
import { Plug } from '@carbon/icons-react';
import ComButton from '@/components/com-button';
import { javascript } from '@codemirror/lang-javascript';
import { EditorView } from '@codemirror/view';
import HelpTooltip from '@/components/help-tooltip';
import ProCodemirror from '@/components/pro-codemirror';
import { useTranslate } from '@/hooks';
import { useThemeStore } from '@/stores/theme-store';
import { getStreamCodemirrorTheme } from './stream-codemirror-theme';
import type { WorkflowConnection } from './program-workflow';
import { insertConnectionCall } from './workflow-connections';
import styles from './workflow.module.scss';

const WorkflowScriptEditor: FC<{
  code: string;
  readOnly: boolean;
  onChange: (code: string) => void;
  connections?: WorkflowConnection[];
  onConnections: () => void;
}> = ({ code, readOnly, onChange, connections = [], onConnections }) => {
  const t = useTranslate();
  const theme = useThemeStore((state) => state.theme);
  return (
    <div className={styles.field}>
      <div className={styles['connection-heading']}>
        <span className={styles['inline-row']}>
          {t('Workflow.scriptStep')}
          <HelpTooltip title={t('Workflow.scriptHint')} />
        </span>
        <Dropdown
          disabled={readOnly}
          menu={{
            items: [
              ...connections.map((connection) => ({
                key: connection.alias,
                label: connection.name + ' · ' + connection.alias,
                disabled: connection.mode === 'source-event',
              })),
              ...(connections.length ? [{ type: 'divider' as const }] : []),
              { key: '__manage', label: t('Workflow.connections') },
            ],
            onClick: ({ key }) => {
              if (key === '__manage') onConnections();
              else {
                const connection = connections.find((item) => item.alias === key);
                if (connection) onChange(insertConnectionCall(code, connection));
              }
            },
          }}
        >
          <ComButton size="small" disabled={readOnly} icon={<Plug />}>
            {t('Workflow.useConnection')}
          </ComButton>
        </Dropdown>
      </div>
      <ProCodemirror
        theme={getStreamCodemirrorTheme(theme)}
        value={code}
        onChange={onChange}
        extensions={[javascript(), EditorView.lineWrapping]}
        readOnly={readOnly}
        height="280px"
        showHint={false}
      />
    </div>
  );
};

export default WorkflowScriptEditor;
