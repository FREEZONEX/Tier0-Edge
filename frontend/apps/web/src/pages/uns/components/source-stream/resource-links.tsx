import { useSourceStreamApi, useSourceStreamScope } from '@/apis/core-api/source-stream-scope';
import { useEffect, useState, type FC } from 'react';
import { Spin } from 'antd';
import ComButton from '@/components/com-button';
import { useTranslate } from '@/hooks';
import { getModelInfo } from '@/apis/core-api/uns';

import { hasPermission } from '@/utils/auth';
import { ButtonPermission } from '@/common-types/button-permission';
import { useIntegrationRoute } from './IntegrationTabs';
import styles from './index.module.scss';

export const TopicLink: FC<{ id: number | string; space?: 'metadata' | 'legacy' }> = ({ id, space }) => {
  const sourceScope = useSourceStreamScope();
  const t = useTranslate();
  const { go } = useIntegrationRoute();
  const [name, setName] = useState<string>();
  const [failed, setFailed] = useState(false);
  const numericId =
    space !== 'metadata' && /^\d+$/.test(String(id)) && Number.isSafeInteger(Number(id)) && Number(id) > 0
      ? Number(id)
      : undefined;
  const canonicalId =
    space !== 'legacy' &&
    (space === 'metadata' || !/^\d+$/.test(String(id))) &&
    /^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$/.test(String(id))
      ? String(id)
      : undefined;
  useEffect(() => {
    let alive = true;
    setName(undefined);
    setFailed(false);
    if (canonicalId) {
      sourceScope
        .loadMetadata()
        .then((items) => {
          const item = items.find((meta) => meta.id === canonicalId);
          if (alive) {
            setName(item?.data.name);
            setFailed(!item);
          }
        })
        .catch(() => alive && setFailed(true));
      return () => {
        alive = false;
      };
    }
    if (numericId === undefined || sourceScope.nodeId) {
      setName(String(id));
      return;
    }
    getModelInfo({ id: numericId })
      .then((node) => {
        if (alive) setName(node.displayName || node.name || node.path || node.alias || String(id));
      })
      .catch(() => alive && setFailed(true));
    return () => {
      alive = false;
    };
  }, [numericId, canonicalId]);
  if (sourceScope.nodeId) return <span>{name || String(id)}</span>;
  if (failed || (numericId === undefined && !canonicalId))
    return (
      <span>
        {t('SourceStream.referenceUnavailable')} · {id}
      </span>
    );
  return (
    <ComButton
      className={styles['resource-link']}
      type="link"
      onClick={() =>
        go(canonicalId ? { view: 'namespace', metaId: canonicalId } : { view: 'namespace', namespaceId: numericId })
      }
    >
      {name || <Spin size="small" />}
    </ComButton>
  );
};

export const SourceLink: FC<{ id: number }> = ({ id }) => {
  const sourceStreamApi = useSourceStreamApi();
  const t = useTranslate();
  const { go } = useIntegrationRoute();
  const [name, setName] = useState<string>();
  const [failed, setFailed] = useState(false);
  const allowed = hasPermission(ButtonPermission['uns.sources']);
  useEffect(() => {
    if (!allowed) return;
    let alive = true;
    sourceStreamApi
      .source(id)
      .then((source) => alive && setName(source.name))
      .catch(() => alive && setFailed(true));
    return () => {
      alive = false;
    };
  }, [id, allowed]);
  if (!allowed || failed)
    return (
      <span>
        {t(failed ? 'SourceStream.referenceUnavailable' : 'SourceStream.source')} · {id}
      </span>
    );
  return (
    <ComButton className={styles['resource-link']} type="link" onClick={() => go({ view: 'source', sourceId: id })}>
      {name || <Spin size="small" />}
    </ComButton>
  );
};
