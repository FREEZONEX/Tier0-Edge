import { useState, type FC } from 'react';
import { Empty, Input } from 'antd';
import { Code, Filter, Flow, Plug, Save, Search } from '@carbon/icons-react';
import ProModal from '@/components/pro-modal';
import { useTranslate } from '@/hooks';
import type { WorkflowAction } from './program-workflow';
import styles from './workflow.module.scss';

export const actionKinds: WorkflowAction['kind'][] = ['source', 'script', 'map', 'filter', 'write'];
const icons = { source: Plug, script: Code, map: Flow, filter: Filter, write: Save };

const WorkflowActionPicker: FC<{
  onSelect: (kind: WorkflowAction['kind']) => void;
  onClose: () => void;
}> = ({ onSelect, onClose }) => {
  const t = useTranslate();
  const [search, setSearch] = useState('');
  const kinds = actionKinds.filter((kind) =>
    (t(`Workflow.kind.${kind}`) + ' ' + t(`Workflow.actionDescription.${kind}`))
      .toLocaleLowerCase()
      .includes(search.trim().toLocaleLowerCase())
  );
  return (
    <ProModal open title={t('Workflow.addAction')} onCancel={onClose} footer={null} width={680}>
      <div className={styles['form-stack']}>
        <Input
          autoFocus
          allowClear
          prefix={<Search />}
          value={search}
          aria-label={t('Workflow.searchActions')}
          placeholder={t('Workflow.searchActions')}
          onChange={(event) => setSearch(event.target.value)}
        />
        <div className={styles['action-options']}>
          {kinds.map((kind) => {
            const Icon = icons[kind];
            return (
              <button type="button" key={kind} className={styles['action-option']} onClick={() => onSelect(kind)}>
                <span className={styles['step-icon']}>
                  <Icon size={22} />
                </span>
                <span className={styles['step-copy']}>
                  <strong>{t(`Workflow.kind.${kind}`)}</strong>
                  <span className={styles['muted-text']}>{t(`Workflow.actionDescription.${kind}`)}</span>
                </span>
              </button>
            );
          })}
        </div>
        {!kinds.length && <Empty description={t('common.noData')} />}
      </div>
    </ProModal>
  );
};
export default WorkflowActionPicker;
