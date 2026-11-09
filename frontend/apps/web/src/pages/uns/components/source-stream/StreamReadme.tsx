import { useSourceStreamApi, useSourceStreamScope } from '@/apis/core-api/source-stream-scope';
import { useEffect, useState } from 'react';
import { Alert, Segmented, Spin } from 'antd';
import { Document, Plug } from '@carbon/icons-react';
import Markdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { type ProgramDocument, type ProgramManifest } from '@/apis/core-api/source-stream';

import { getModelInfo } from '@/apis/core-api/uns';
import { ButtonPermission } from '@/common-types/button-permission';
import { hasPermission } from '@/utils/auth';
import ComButton from '@/components/com-button';
import HelpTooltip from '@/components/help-tooltip';
import { useTranslate } from '@/hooks';
import { useBaseStore } from '@/stores/base';
import { canReadUnsMetadata } from '../../metadata-permission';
import { useIntegrationRoute } from './IntegrationTabs';
import { documentedWorkflow, entryMode, readmeDescription, readmeManifest } from './stream-readme';
import { programResourceKey } from './program-document';
import { workflowUsedSources } from './workflow-connections';
import ProgramScriptView from './ProgramScriptView';
import styles from './stream-readme.module.scss';

export default function StreamReadme({
  document,
  manifest: savedManifest,
  initialSourceName,
  initialTopicName,
}: {
  document?: ProgramDocument;
  manifest?: ProgramManifest;
  canManage: boolean;
  initialSourceName?: string;
  initialTopicName?: string;
}) {
  const sourceStreamApi = useSourceStreamApi();
  const sourceScope = useSourceStreamScope();
  const t = useTranslate();
  const { go } = useIntegrationRoute();
  const [view, setView] = useState<'readme' | 'code'>('readme');
  const manifest = readmeManifest(savedManifest);
  const workflow = document && documentedWorkflow(document);
  const description = readmeDescription(document);
  const sourceAllowed = hasPermission(ButtonPermission['uns.sources']);
  const user = useBaseStore((state) => state.currentUserInfo);
  const authEnable = useBaseStore((state) => state.systemInfo?.authEnable);
  const metadataAllowed = canReadUnsMetadata(user, authEnable);
  const references = [...(workflow?.connections ?? []), ...(workflow ? workflowUsedSources(workflow) : [])];
  const sourceKey = JSON.stringify(manifest?.sources.map((source) => source.id) ?? []);
  const resourceKey = JSON.stringify(manifest?.topics ?? []);
  const [labels, setLabels] = useState<{
    key: string;
    names: Record<string, string>;
    loading: boolean;
    failed: boolean;
  }>();
  const labelKey = JSON.stringify([sourceKey, resourceKey, sourceAllowed, metadataAllowed]);
  useEffect(() => {
    let alive = true;
    setLabels({ key: labelKey, names: {}, loading: true, failed: false });
    const sources: string[] = JSON.parse(sourceKey);
    const resources: ProgramManifest['topics'] = JSON.parse(resourceKey);
    Promise.allSettled([
      ...(sourceAllowed
        ? sources.map(async (id) => ['source:' + id, (await sourceStreamApi.source(Number(id))).name])
        : []),
      ...(metadataAllowed
        ? resources.map(async (resource) => {
            const { collectionId, topicId, space } = resource;
            const key = 'resource:' + programResourceKey(resource);
            if (collectionId || space === 'metadata' || (!space && !/^\d+$/.test(topicId ?? ''))) {
              const meta = await sourceScope.findMetadata(
                collectionId ? 'collection' : 'topic',
                collectionId ?? topicId!
              );
              if (!meta) throw new Error('StreamReadme.namesFailed');
              return [key, meta.data.name];
            }
            const id = Number(topicId);
            if (!Number.isSafeInteger(id) || id <= 0) throw new Error('StreamReadme.namesFailed');
            if (sourceScope.nodeId) return [key, String(id)];
            const node = await getModelInfo({ id });
            return [key, node.displayName || node.name || node.path || node.alias || String(id)];
          })
        : []),
    ]).then((results) => {
      if (!alive) return;
      const names: Record<string, string> = {};
      for (const result of results) if (result.status === 'fulfilled') names[result.value[0]] = result.value[1];
      setLabels({
        key: labelKey,
        names,
        loading: false,
        failed: results.some((result) => result.status === 'rejected'),
      });
    });
    return () => {
      alive = false;
    };
  }, [sourceKey, resourceKey, labelKey, sourceAllowed, metadataAllowed]);
  const names = labels?.key === labelKey ? labels.names : {};
  if (!document)
    return (
      <article className={styles['create-page']}>
        <Document size={28} />
        <h1>{t('StreamReadme.createTitle')}</h1>
        <p>{t('StreamReadme.createHint')}</p>
        {initialSourceName && (
          <p>
            {t('SourceStream.source')}: {initialSourceName}
          </p>
        )}
        {initialTopicName && (
          <p>
            {t('uns.topic')}: {initialTopicName}
          </p>
        )}
        <ol className={styles['creation-guide']}>
          <li>
            <strong>{t('StreamReadme.specify')}</strong>
            <span>{t('StreamReadme.specifyHint')}</span>
          </li>
          <li>
            <strong>{t('StreamReadme.review')}</strong>
            <span>{t('StreamReadme.reviewHint')}</span>
          </li>
          <li>
            <strong>{t('StreamReadme.testPublish')}</strong>
            <span>{t('StreamReadme.testHint')}</span>
          </li>
        </ol>
      </article>
    );
  return (
    <section className={styles.page}>
      <header className={styles.header}>
        <h1>{document.name}</h1>
        <Segmented
          aria-label={t('StreamReadme.view')}
          value={view}
          onChange={(value) => setView(value as 'readme' | 'code')}
          options={[
            { value: 'readme', label: t('StreamReadme.readme') },
            { value: 'code', label: t('SourceStream.code') },
          ]}
        />
      </header>
      {view === 'code' ? (
        <div className={styles['code-view']}>
          <div className={styles['code-toolbar']}>
            <span>{t('StreamReadme.codeHint')}</span>
          </div>
          <ProgramScriptView code={document.script} />
        </div>
      ) : (
        <div className={styles.layout}>
          <article className={styles.article}>
            <section>
              <h2>{t('StreamReadme.about')}</h2>
              {description.trim() ? (
                <div className={styles.markdown}>
                  <Markdown remarkPlugins={[remarkGfm]} skipHtml components={{ img: () => null }}>
                    {description}
                  </Markdown>
                </div>
              ) : (
                <p className={styles.muted}>{t('StreamReadme.noDescription')}</p>
              )}
            </section>
            <section>
              <h2>{t('Workflow.runMode')}</h2>
              {!manifest?.entries.length ? (
                <p className={styles.muted}>{t('StreamReadme.noManifest')}</p>
              ) : (
                <div className={styles.entries}>
                  {manifest.entries.map((entry) => (
                    <div key={entry.id} className={styles.entry}>
                      <strong>{t('StreamReadme.mode.' + entryMode(entry.kind))}</strong>
                      <span>
                        {t('StreamReadme.run.' + entryMode(entry.kind), { seconds: (entry.intervalMs ?? 0) / 1000 })}
                      </span>
                      {entry.kind === 'http-request' && (
                        <code>
                          {entry.method} {entry.path}
                        </code>
                      )}
                      {(entry.resourceRefs ?? entry.topicIds ?? []).length > 0 && (
                        <code>{(entry.resourceRefs ?? entry.topicIds)?.join(', ')}</code>
                      )}
                      {manifest.entries.length > 1 && <code className={styles.muted}>{entry.id}</code>}
                    </div>
                  ))}
                </div>
              )}
            </section>
            <section>
              <h2>{t('StreamReadme.processing')}</h2>
              {workflow?.actions.length ? (
                <ol className={styles['logic-list']}>
                  {workflow.actions.map((action) => (
                    <li key={action.id}>
                      <strong>{t('Workflow.kind.' + action.kind)}</strong>
                      <span>
                        {action.kind === 'source'
                          ? [
                              action.source?.name,
                              action.operation === 'request'
                                ? action.method + ' ' + action.path
                                : action.source?.resource,
                            ]
                              .filter(Boolean)
                              .join(' · ')
                          : t('StreamReadme.logic.' + action.kind)}
                      </span>
                    </li>
                  ))}
                </ol>
              ) : (
                <p className={styles.muted}>{t('StreamReadme.scriptLogic')}</p>
              )}
            </section>
            <section>
              <h2>
                {t('StreamReadme.dataResources')} <HelpTooltip title={t('StreamReadme.resourceHint')} />
              </h2>
              {!manifest ? (
                <p className={styles.muted}>{t('StreamReadme.noManifest')}</p>
              ) : manifest.topics.length ? (
                <ul className={styles['resource-list']}>
                  {manifest.topics.map((target) => {
                    const id = target.collectionId ?? target.topicId!;
                    const key = programResourceKey(target);
                    return (
                      <li key={key}>
                        <strong>{names['resource:' + key] ?? id}</strong>
                        <span>
                          {target.collectionId ? t('UnsMeta.collection') : t('uns.topic')} ·{' '}
                          {t('StreamReadme.access.' + (target.access ?? 'unknown'))}
                        </span>
                      </li>
                    );
                  })}
                </ul>
              ) : (
                <p className={styles.muted}>{t('StreamReadme.noResources')}</p>
              )}
            </section>
          </article>
          <aside className={styles.aside}>
            <section>
              <h2>
                <Plug size={18} />
                {t('Workflow.connections')}
              </h2>
              {labels?.key === labelKey && labels.loading && <Spin size="small" />}
              {!manifest ? (
                <p className={styles.muted}>{t('StreamReadme.noManifest')}</p>
              ) : manifest.sources.length ? (
                <ul className={styles['source-list']}>
                  {manifest.sources.map((source) => (
                    <li key={source.id}>
                      <ComButton
                        type="link"
                        disabled={!sourceAllowed}
                        onClick={() => go({ view: 'source', sourceId: Number(source.id) })}
                      >
                        {names['source:' + source.id] ??
                          references.find((item) => item.id === source.id)?.name ??
                          source.id}
                      </ComButton>
                      {source.resources.map((resource) => (
                        <code key={resource}>{resource}</code>
                      ))}
                    </li>
                  ))}
                </ul>
              ) : (
                <p className={styles.muted}>{t('StreamReadme.noSources')}</p>
              )}
              {!!manifest?.sources.length && <p className={styles.muted}>{t('Workflow.sourceOwnsAuth')}</p>}
            </section>
            {!!document.timeoutMs && (
              <section>
                <h2>{t('StreamReadme.execution')}</h2>
                <dl className={styles.facts}>
                  <dt>{t('StreamReadme.timeout')}</dt>
                  <dd>{t('StreamReadme.seconds', { seconds: document.timeoutMs / 1000 })}</dd>
                </dl>
              </section>
            )}
            {labels?.key === labelKey && labels.failed && (
              <Alert type="warning" message={t('StreamReadme.namesFailed')} />
            )}
          </aside>
        </div>
      )}
    </section>
  );
}
