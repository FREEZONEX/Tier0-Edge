import { useState, type FC } from 'react';
import ProModal from '@/components/pro-modal';
import { useTranslate } from '@/hooks';
import ProgramConfigEditor from './ProgramConfigEditor';
import { readProgramDocument } from './program-document';
import styles from './index.module.scss';

const ProgramJSONModal: FC<{
  value: string;
  readOnly: boolean;
  onApply: (value: string) => void;
  onClose: () => void;
}> = ({ value, readOnly, onApply, onClose }) => {
  const t = useTranslate();
  const [draft, setDraft] = useState(value);
  const valid = !!readProgramDocument(draft);
  return (
    <ProModal
      open
      title={t('SourceStream.jsonConfig')}
      width={900}
      footer={undefined}
      onCancel={onClose}
      okText={t('Workflow.applyConfig')}
      cancelText={t('common.cancel')}
      okButtonProps={{ className: styles['manage-button'], disabled: readOnly || !valid || draft === value }}
      onOk={() => {
        if (readOnly || !valid) return;
        onApply(draft);
        onClose();
      }}
    >
      <ProgramConfigEditor value={draft} onChange={setDraft} readOnly={readOnly} hideHeader editorMode="json" />
    </ProModal>
  );
};

export default ProgramJSONModal;
