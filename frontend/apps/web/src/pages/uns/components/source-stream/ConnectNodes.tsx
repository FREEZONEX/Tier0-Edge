import { useEffect, type FC } from 'react';
import { Tag } from 'antd';
import { ChevronDown, ChevronRight, Flow, Plug, ArrowRight } from '@carbon/icons-react';
import { Handle, Position, useUpdateNodeInternals, type NodeProps } from 'reactflow';
import ComButton from '@/components/com-button';
import { useTranslate } from '@/hooks';
import type { ConnectCategory } from '@/apis/core-api/connect-workspace';
import type { ConnectBasis, ConnectSourceCard, ConnectStreamCard } from './connect-model';
import ConnectSourceTile from './ConnectSourceTile';
import styles from './connect-workspace.module.scss';

export type SourceGroupData = {
  category: ConnectCategory;
  sources: ConnectSourceCard[];
  collapsed: boolean;
  onToggle: () => void;
  onOpen: (id: number) => void;
  streams: Map<string, ConnectStreamCard[]>;
  expanded: Set<number>;
  readable: boolean;
  complete: boolean;
  layoutKey: string;
  onExpand: (id: number) => void;
  onManage?: (id: number) => void;
  onCreate?: (id: number) => void;
  onOpenStream: (id: number) => void;
  onEditStream?: (id: number) => void;
};
export const ConnectSourceGroup: FC<NodeProps<SourceGroupData>> = ({ id, data }) => {
  const t = useTranslate();
  const updateNodeInternals = useUpdateNodeInternals();
  useEffect(() => updateNodeInternals(id), [id, data.layoutKey, updateNodeInternals]);
  return (
    <section className={styles['source-group']}>
      <header className={styles['group-heading']}>
        <Plug size={18} />
        <strong>{t(`UnsConnect.category.${data.category}`)}</strong>
        <span className={styles.count}>{data.sources.length}</span>
        <ComButton
          className="nodrag nopan"
          type="text"
          aria-label={t(data.collapsed ? 'UnsConnect.expand' : 'UnsConnect.collapse')}
          aria-expanded={!data.collapsed}
          icon={data.collapsed ? <ChevronRight /> : <ChevronDown />}
          onClick={data.onToggle}
        />
      </header>
      {!data.collapsed && (
        <div className={styles['source-group-body']}>
          {data.sources.map((source) => (
            <ConnectSourceTile
              key={source.id}
              source={source}
              streams={data.streams.get(String(source.id)) || []}
              expanded={data.expanded.has(source.id)}
              readable={data.readable}
              complete={data.complete}
              onToggle={() => data.onExpand(source.id)}
              onOpen={data.onOpen}
              onManage={data.onManage}
              onCreate={data.onCreate}
              onOpenStream={data.onOpenStream}
              onEditStream={data.onEditStream}
            />
          ))}
        </div>
      )}
      {data.collapsed &&
        data.sources.map((source, index) => (
          <Handle
            key={source.id}
            type="source"
            position={Position.Right}
            id={`source:${source.id}`}
            style={{ top: 20 + (index % 3) }}
            isConnectable={false}
          />
        ))}
    </section>
  );
};

export type StreamNodeData = {
  stream: ConnectStreamCard;
  basis: ConnectBasis;
  stacked: boolean;
  onOpen: (id: number) => void;
};
export const ConnectStreamNode: FC<NodeProps<StreamNodeData>> = ({ id, data }) => {
  const t = useTranslate();
  const updateNodeInternals = useUpdateNodeInternals();
  useEffect(() => updateNodeInternals(id), [id, data.stacked, updateNodeInternals]);
  const manifest = data.stream[data.basis];
  return (
    <section className={styles['stream-card']}>
      <Handle type="target" position={data.stacked ? Position.Top : Position.Left} id="input" isConnectable={false} />
      <button type="button" className={`nodrag nopan ${styles.identity}`} onClick={() => data.onOpen(data.stream.id)}>
        <Flow size={20} />
        <strong title={data.stream.name}>{data.stream.name}</strong>
        <ArrowRight size={16} />
      </button>
      <div className={styles['card-status']}>
        <Tag>{t(`SourceStream.status.${data.stream.status}`)}</Tag>
        <span title={String(data.stream.activeVersion)}>
          {t(data.basis === 'active' ? 'UnsConnect.active' : 'SourceStream.status.draft')}
        </span>
      </div>
      <p className={styles.summary}>
        {manifest
          ? t('UnsConnect.dependencies', { sources: manifest.sources.length, targets: manifest.topics.length })
          : t(data.basis === 'active' ? 'UnsConnect.notDeployed' : 'UnsConnect.unvalidated')}
      </p>
      {data.stream.executionState && !['ready', 'draft', 'disabled'].includes(data.stream.executionState) && (
        <p className={styles.warning}>{t(`SourceStream.execution.${data.stream.executionState}`)}</p>
      )}
      <Handle
        type="target"
        position={Position.Right}
        id="resource-input"
        data-connect-anchor={`stream:${data.stream.id}:read`}
        style={{ top: '35%' }}
        isConnectable={false}
      />
      <Handle
        type="source"
        position={Position.Right}
        id="output"
        data-connect-anchor={`stream:${data.stream.id}:write`}
        style={{ top: '65%' }}
        isConnectable={false}
      />
    </section>
  );
};

export const ConnectMissingSource: FC<NodeProps<{ id: string }>> = ({ data }) => {
  const t = useTranslate();
  return (
    <section className={styles['missing-card']}>
      <strong>Source · {data.id}</strong>
      <p>{t('UnsConnect.unavailableRef')}</p>
      <Handle type="source" position={Position.Right} id={`source:${data.id}`} isConnectable={false} />
    </section>
  );
};
