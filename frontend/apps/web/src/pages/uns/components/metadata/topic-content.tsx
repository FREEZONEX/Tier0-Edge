import type { ReactNode } from 'react';
import { Descriptions, Empty, Tabs } from 'antd';
import { Edit } from '@carbon/icons-react';
import ComButton from '@/components/com-button';
import { ButtonPermission } from '@/common-types/button-permission';
import { useTranslate } from '@/hooks';
import type { UnsMetadata } from '@/apis/core-api/uns-metadata';
import { useIntegrationRoute } from '../source-stream/IntegrationTabs';
import DataPanel, { SchemaView, displayTime } from './data-panel';
import TopicFields from './topic-fields';
import DataSection from './data-section';

export default function TopicContent({
  meta,
  collection,
  items,
  path,
  topology,
  onSelect,
  onEditSchema,
}: {
  meta: UnsMetadata;
  collection?: UnsMetadata;
  items: UnsMetadata[];
  path: string;
  topology: ReactNode;
  onSelect: (id: string) => void;
  onEditSchema: () => void;
}) {
  const t = useTranslate();
  const route = useIntegrationRoute();
  return (
    <Tabs
      activeKey={
        route.detail === 'schema' || route.detail === 'details' || route.detail === 'topology' ? route.detail : 'data'
      }
      onChange={(detail) => route.go({ view: 'namespace', metaId: meta.id, detail })}
      items={[
        {
          key: 'data',
          label: t('NamespaceView.records'),
          children: (
            <>
              {collection ? (
                <DataPanel tableMode key={`${collection.id}-${collection.revision}`} collection={collection} />
              ) : (
                !meta.data.fields?.length && <Empty description={t('UnsMeta.noDefaultCollection')} />
              )}
              {!!meta.data.fields?.length && <TopicFields meta={meta} items={items} onSelect={onSelect} />}
            </>
          ),
        },
        ...(collection
          ? [
              {
                key: 'schema',
                label: t('NamespaceView.schema'),
                children: (
                  <>
                    {collection && (
                      <DataSection
                        compact
                        title={t('UnsUi.schema')}
                        extra={
                          !collection.mountId &&
                          !meta.mountId && (
                            <ComButton
                              type="text"
                              icon={<Edit />}
                              auth={ButtonPermission['uns.metadataWrite']}
                              aria-label={t('UnsUi.editSchema')}
                              onClick={(event) => {
                                event.stopPropagation();
                                onEditSchema();
                              }}
                            />
                          )
                        }
                      >
                        <SchemaView collection={collection} />
                      </DataSection>
                    )}
                  </>
                ),
              },
            ]
          : []),
        {
          key: 'details',
          label: t('common.detail'),
          children: (
            <Descriptions
              column={1}
              bordered
              size="small"
              items={[
                { key: 'name', label: t('UnsMeta.name'), children: meta.data.name },
                { key: 'path', label: t('UnsUi.path'), children: path },
                { key: 'description', label: t('UnsMeta.description'), children: meta.data.description || '—' },
                { key: 'created', label: t('UnsUi.createdAt'), children: displayTime(meta.createdAt) },
                { key: 'updated', label: t('UnsMeta.updatedAt'), children: displayTime(meta.updatedAt) },
              ]}
            />
          ),
        },
        { key: 'topology', label: t('UnsMeta.topology'), children: topology },
      ]}
    />
  );
}
