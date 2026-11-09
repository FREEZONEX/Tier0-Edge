import type { FC, ReactNode } from 'react';
import { Input, Select, Space } from 'antd';
import { Add, TrashCan } from '@carbon/icons-react';
import { v4 as uuid } from 'uuid';
import ComButton from '@/components/com-button';
import HelpTooltip from '@/components/help-tooltip';
import { useTranslate } from '@/hooks';
import { MAX_LENGTHS } from '@/utils/limits';
import type { OPCUAServerNode } from '@/apis/core-api/source-stream';
import styles from './index.module.scss';

const OPCUAServerNodes: FC<{
  value: OPCUAServerNode[];
  onChange: (nodes: OPCUAServerNode[]) => void;
  readOnly: boolean;
}> = ({ value, onChange, readOnly }) => {
  const t = useTranslate();
  const update = (index: number, patch: Partial<OPCUAServerNode>) =>
    onChange(value.map((node, i) => (i === index ? { ...node, ...patch } : node)));
  const field = (key: string, input: ReactNode) => (
    <label className={styles.field}>
      <span>{t(`SourceStream.${key}`)}</span>
      {input}
    </label>
  );
  return (
    <section className={styles['full-width']}>
      <h3>
        {t('SourceStream.serverNodes')} <HelpTooltip title={t('SourceStream.opcuaNodesHint')} />
      </h3>
      {value.map((node, index) => (
        <div className={styles['detail-section']} key={index}>
          <Space>
            <strong>{t('SourceStream.serverNode', { index: index + 1 })}</strong>
            <ComButton
              size="small"
              disabled={readOnly}
              icon={<TrashCan size={16} />}
              aria-label={t('SourceStream.remove')}
              onClick={() => onChange(value.filter((_, i) => i !== index))}
            >
              {t('SourceStream.remove')}
            </ComButton>
          </Space>
          <div className={styles.editor}>
            {field(
              'resourceId',
              <Input
                value={node.id}
                readOnly={readOnly}
                maxLength={MAX_LENGTHS.name}
                onChange={(e) => update(index, { id: e.target.value })}
              />
            )}
            {field(
              'name',
              <Input
                value={node.name}
                readOnly={readOnly}
                maxLength={MAX_LENGTHS.name}
                onChange={(e) => update(index, { name: e.target.value })}
              />
            )}
            {field(
              'nodeClass',
              <Select
                value={node.nodeClass}
                disabled={readOnly}
                options={['Variable', 'Method'].map((value) => ({
                  value,
                  label: t(`SourceStream.nodeClass.${value}`),
                }))}
                onChange={(nodeClass: OPCUAServerNode['nodeClass']) =>
                  update(
                    index,
                    nodeClass === 'Method' ? { nodeClass, dataType: 'String', direction: 'ingress' } : { nodeClass }
                  )
                }
              />
            )}
            {field(
              'nodeDataType',
              <Select
                value={node.dataType}
                disabled={readOnly || node.nodeClass === 'Method'}
                options={['Double', 'Int32', 'Boolean', 'String'].map((value) => ({ value }))}
                onChange={(dataType: OPCUAServerNode['dataType']) => update(index, { dataType })}
              />
            )}
            {field(
              'nodeDirection',
              <Select
                value={node.direction}
                disabled={readOnly || node.nodeClass === 'Method'}
                options={['ingress', 'egress', 'bidirectional'].map((value) => ({
                  value,
                  label: t(`SourceStream.nodeDirection.${value}`),
                }))}
                onChange={(direction: OPCUAServerNode['direction']) => update(index, { direction })}
              />
            )}
          </div>
        </div>
      ))}
      <ComButton
        disabled={readOnly || value.length >= 1000}
        icon={<Add size={16} />}
        onClick={() =>
          onChange([
            ...value,
            {
              id: `node_${uuid().slice(0, 8)}`,
              name: '',
              nodeClass: 'Variable',
              dataType: 'Double',
              direction: 'ingress',
            },
          ])
        }
      >
        {t('SourceStream.addServerNode')}
      </ComButton>
    </section>
  );
};

export default OPCUAServerNodes;
