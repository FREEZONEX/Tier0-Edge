import type { FC } from 'react';
import { Input, Segmented } from 'antd';
import { useTranslate } from '@/hooks';
import { MAX_LENGTHS } from '@/utils/limits';
import type { ProgramDocument } from '@/apis/core-api/source-stream';
import styles from './workflow.module.scss';

export type WorkflowEditorMode = 'steps' | 'code';
const WorkflowEditorToolbar: FC<{
  document?: ProgramDocument;
  readOnly: boolean;
  mode: WorkflowEditorMode;
  onMode: (mode: WorkflowEditorMode) => void;
  onChange: (value: string) => void;
}> = ({ document, readOnly, mode, onMode, onChange }) => {
  const t = useTranslate();
  return (
    <div className={styles['editor-toolbar']}>
      <Input
        aria-label={t('SourceStream.name')}
        value={document?.name ?? ''}
        disabled={readOnly || !document}
        placeholder={t('Workflow.namePlaceholder')}
        count={{ max: MAX_LENGTHS.name, strategy: (value) => Array.from(value).length }}
        onChange={(event) => {
          const name = event.target.value;
          if (document && Array.from(name).length <= MAX_LENGTHS.name)
            onChange(JSON.stringify({ ...document, name }, null, 2));
        }}
      />
      <Segmented
        aria-label={t('Workflow.editorMode')}
        value={mode}
        onChange={(next) => onMode(next as WorkflowEditorMode)}
        options={[
          { value: 'steps', label: t('Workflow.steps'), disabled: !document },
          { value: 'code', label: t('SourceStream.code'), disabled: !document },
        ]}
      />
    </div>
  );
};
export default WorkflowEditorToolbar;
