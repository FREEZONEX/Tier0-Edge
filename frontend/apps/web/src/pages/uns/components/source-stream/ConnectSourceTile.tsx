import type { FC } from 'react';
import { Tag } from 'antd';
import { Add, ArrowRight, ChevronDown, ChevronRight, Edit, Flow, Plug, Settings } from '@carbon/icons-react';
import { Handle, Position } from 'reactflow';
import ComButton from '@/components/com-button';
import { useTranslate } from '@/hooks';
import type { ConnectSourceCard, ConnectStreamCard } from './connect-model';
import { sourceKey } from './connect-model';
import { sourceCardLayout } from './connect-source';
import styles from './connect-workspace.module.scss';

export type ConnectSourceTileProps = {
  source: ConnectSourceCard;
  streams: ConnectStreamCard[];
  expanded: boolean;
  readable: boolean;
  complete: boolean;
  onToggle: () => void;
  onOpen: (id: number) => void;
  onManage?: (id: number) => void;
  onCreate?: (id: number) => void;
  onOpenStream: (id: number) => void;
  onEditStream?: (id: number) => void;
};

const ConnectSourceTile: FC<ConnectSourceTileProps> = ({
  source,
  streams,
  expanded,
  readable,
  complete,
  onToggle,
  onOpen,
  onManage,
  onCreate,
  onOpenStream,
  onEditStream,
}) => {
  const t = useTranslate();
  const layout = sourceCardLayout(streams.length, expanded, readable, Boolean(onCreate));
  return (
    <article className={styles['source-card']} style={{ height: layout.height }} data-connect-source={source.id}>
      <div className={styles['source-heading']}>
        <button type="button" className={`nodrag nopan ${styles.identity}`} onClick={() => onOpen(source.id)}>
          <span className={styles.icon}>
            <Plug size={20} />
          </span>
          <span>
            <strong title={source.name}>{source.name}</strong>
            <small title={t(`SourceStream.role.${source.role}`)}>
              {source.protocol.toUpperCase()} · {t(`SourceStream.role.${source.role}`)}
            </small>
          </span>
          <ArrowRight size={16} />
        </button>
        {onManage && (
          <ComButton
            type="text"
            className="nodrag nopan"
            aria-label={`${t('SourceStream.manageConnection')} · ${source.name}`}
            title={t('SourceStream.manageConnection')}
            icon={<Settings size={16} />}
            onClick={() => onManage(source.id)}
          />
        )}
      </div>
      <div className={styles['card-status']}>
        <Tag>{t(`SourceStream.health.${source.health}`)}</Tag>
      </div>
      <div className={styles['source-expand-row']}>
        {readable && (
          <button
            type="button"
            className={`nodrag nopan ${styles['source-expand']}`}
            aria-label={`${t('SourceStream.mappingRelations')} · ${source.name}`}
            aria-expanded={expanded}
            onClick={onToggle}
          >
            {expanded ? <ChevronDown size={14} /> : <ChevronRight size={14} />}
            {t('SourceStream.stream')}
            <span className={styles.count}>
              {complete ? streams.length : streams.length ? `${streams.length}+` : '—'}
            </span>
          </button>
        )}
      </div>
      {expanded && readable && (
        <div
          className={`nodrag nopan nowheel ${styles['source-connections']}`}
          style={{ height: layout.bodyHeight }}
          role="region"
          aria-label={`${t('SourceStream.mappingRelations')} · ${source.name}`}
        >
          {!streams.length && (
            <div className={styles['source-stream-empty']}>
              {t(complete ? 'SourceStream.noObjects' : 'SourceStream.loadFailed')}
            </div>
          )}
          {streams.map((stream) => (
            <div className={styles['source-stream-row']} key={stream.id}>
              <button
                type="button"
                className={styles['source-stream-name']}
                title={stream.name}
                onClick={() => onOpenStream(stream.id)}
              >
                <Flow size={14} />
                <span>{stream.name}</span>
              </button>
              <Tag className={styles['source-stream-status']} title={t(`SourceStream.status.${stream.status}`)}>
                {t(`SourceStream.status.${stream.status}`)}
              </Tag>
              {stream.editable && onEditStream && (
                <ComButton
                  type="text"
                  size="small"
                  aria-label={`${t('SourceStream.editProgram')} · ${stream.name}`}
                  icon={<Edit size={14} />}
                  onClick={() => onEditStream(stream.id)}
                />
              )}
            </div>
          ))}
          {onCreate && (
            <ComButton
              type="text"
              className={styles['source-create-stream']}
              icon={<Add size={14} />}
              onClick={() => onCreate(source.id)}
            >
              {t('SourceStream.addStream')}
            </ComButton>
          )}
        </div>
      )}
      <Handle
        type="source"
        position={Position.Right}
        id={sourceKey(source.id)}
        style={{ top: 28 }}
        isConnectable={false}
      />
    </article>
  );
};

export default ConnectSourceTile;
