import { useEffect, useState, type ComponentProps } from 'react';
import { App, Checkbox, Collapse, Form, Input, Select, Space, TreeSelect } from 'antd';
import { Add, TrashCan } from '@carbon/icons-react';
import ComButton from '@/components/com-button';
import ProModal from '@/components/pro-modal';
import HelpTooltip from '@/components/help-tooltip';
import { openConfirmModal } from '@/components/confirm-modal';
import { useTranslate } from '@/hooks';
import {
  metadataApi,
  type MetaKind,
  type MetaDocument,
  type UnsMetadata,
  type UnsRecord,
} from '@/apis/core-api/uns-metadata';
import { MAX_LENGTHS } from '@/utils/limits';
import { useIntegrationDirty } from '../source-stream/IntegrationTabs';
import SchemaEditor, { emptySchema, fieldPaths, json } from './schema-editor';
import CollectionUsage from './collection-usage';
import CollectionPreview from './collection-preview';
import CollectionSelect from './collection-select';
import { DataValue, recordName } from './data-values';
import { outputAt } from '../source-stream/workflow-data';
import type useConnections from './use-connections';
import styles from './index.module.scss';
import TopicComposer from './topic-composer';

function BindingRow({
  index,
  items,
  remove,
  readRecords,
}: {
  index: number;
  items: UnsMetadata[];
  remove: () => void;
  readRecords: typeof metadataApi.records;
}) {
  const t = useTranslate();
  const form = Form.useFormInstance();
  const binding = Form.useWatch(['fields', index], form);
  const [records, setRecords] = useState<UnsRecord[]>([]);
  const [query, setQuery] = useState('');
  const [loading, setLoading] = useState(false);
  useEffect(() => {
    const controller = new AbortController();
    setRecords([]);
    if (!binding?.collectionId) return;
    const timer = setTimeout(() => {
      setLoading(true);
      readRecords(binding.collectionId, { search: query, size: 50 }, controller.signal)
        .then((result) => {
          if (!controller.signal.aborted) setRecords(result.list);
        })
        .catch(() => {
          /* Request interceptor reports the error. */
        })
        .finally(() => {
          if (!controller.signal.aborted) setLoading(false);
        });
    }, 200);
    return () => {
      controller.abort();
      clearTimeout(timer);
    };
  }, [binding?.collectionId, query, readRecords]);
  const collection = items.find((item) => item.id === binding?.collectionId);
  const paths = fieldPaths(binding?.kind === 'sample' ? collection?.data.sampleSchema : collection?.data.recordSchema);
  const recordIds = [...new Set([...(binding?.recordId ? [binding.recordId] : []), ...records.map((item) => item.id)])];
  const selectedRecord = records.find((record) => record.id === binding?.recordId);
  const required = [{ required: true, message: t('UnsMeta.requiredValue') }];
  return (
    <div className={styles['binding-row']}>
      <Form.Item name={[index, 'name']} label={t('UnsMeta.field')} rules={required}>
        <Input maxLength={MAX_LENGTHS.name} />
      </Form.Item>
      <Form.Item name={[index, 'displayName']} label={t('UnsMeta.displayName')}>
        <Input maxLength={MAX_LENGTHS.name} />
      </Form.Item>
      <Form.Item name={[index, 'kind']} label={t('UnsUi.valueSource')} rules={required}>
        <Select
          options={[
            { value: 'record', label: t('UnsUi.recordData') },
            { value: 'sample', label: t('UnsUi.sampleData') },
          ]}
          onChange={() => form.setFieldValue(['fields', index, 'fieldPath'], undefined)}
        />
      </Form.Item>
      <Form.Item name={[index, 'collectionId']} label={t('UnsMeta.collection')} rules={required}>
        <CollectionSelect
          items={items}
          onChange={() => {
            form.setFieldValue(['fields', index, 'recordId'], undefined);
            form.setFieldValue(['fields', index, 'fieldPath'], undefined);
          }}
        />
      </Form.Item>
      <Form.Item name={[index, 'recordId']} label={t('UnsMeta.recordId')} rules={required}>
        <Select
          showSearch
          filterOption={false}
          onSearch={setQuery}
          loading={loading}
          options={recordIds.map((id) => {
            const record = records.find((record) => record.id === id);
            const name = record ? recordName(record) : id;
            return { value: id, label: name === id ? id : `${name} · ${id}` };
          })}
        />
      </Form.Item>
      <Form.Item
        name={[index, 'fieldPath']}
        label={
          <>
            {t('UnsMeta.fieldPath')} <HelpTooltip title={t('Output.schemaOrigin')} />
          </>
        }
        rules={required}
        extra={
          binding?.kind === 'record' && binding?.fieldPath && selectedRecord ? (
            <span>
              {t('Output.sampleValue')}: <DataValue value={outputAt(selectedRecord.data, binding.fieldPath)} />
            </span>
          ) : undefined
        }
      >
        <Select showSearch options={paths.map((path) => ({ value: path, label: path }))} />
      </Form.Item>
      <ComButton type="text" aria-label={t('common.delete')} icon={<TrashCan />} onClick={remove} />
    </div>
  );
}

function ExistingMetadataEditor({
  kind: initialKind,
  current,
  parentId,
  items,
  onClose,
  onSaved,
  connections,
  standalone = false,
  initialData,
  saveMetadata = metadataApi.save,
  createCollection = metadataApi.createCollection,
  readRecords = metadataApi.records,
}: {
  kind: MetaKind;
  current?: UnsMetadata;
  parentId?: string;
  items: UnsMetadata[];
  onClose: () => void;
  onSaved: (meta: UnsMetadata) => void;
  connections?: ReturnType<typeof useConnections>;
  standalone?: boolean;
  initialData?: Partial<MetaDocument>;
  saveMetadata?: typeof metadataApi.save;
  createCollection?: typeof metadataApi.createCollection;
  readRecords?: typeof metadataApi.records;
}) {
  const t = useTranslate();
  const { modal, message } = App.useApp();
  const [form] = Form.useForm();
  const [dirty, setDirty] = useState(false);
  const [saving, setSaving] = useState(false);
  const kind = initialKind;
  useIntegrationDirty('metadata-editor', dirty);
  useEffect(() => {
    const guard = (event: BeforeUnloadEvent) => {
      if (dirty) event.preventDefault();
    };
    window.addEventListener('beforeunload', guard);
    return () => window.removeEventListener('beforeunload', guard);
  }, [dirty]);
  const sampleEnabled = Form.useWatch('sampleEnabled', form);
  const collectionId = Form.useWatch('collectionId', form);
  const bindings = Form.useWatch('fields', form);
  const selectedCollection = items.find((item) => item.kind === 'collection' && item.id === collectionId);
  const required = [{ required: true, message: t('UnsMeta.requiredValue') }];
  const close = () => {
    if (saving) return;
    if (!dirty) return onClose();
    openConfirmModal(modal, {
      title: t('UnsMeta.unsaved'),
      content: t('UnsMeta.discardHint'),
      okText: t('UnsMeta.discard'),
      cancelText: t('common.cancel'),
      onOk: onClose,
    });
  };
  const blockedParents = new Set(current ? [current.id] : []);
  for (let n = 0; n < items.length; n++) {
    let changed = false;
    items.forEach((item) => {
      if (item.data.parentId && blockedParents.has(item.data.parentId) && !blockedParents.has(item.id)) {
        blockedParents.add(item.id);
        changed = true;
      }
    });
    if (!changed) break;
  }
  const save = async () => {
    const values = await form.validateFields();
    setSaving(true);
    try {
      const document = {
        name: values.name.trim(),
        description: values.description?.trim() || undefined,
        parentId: values.parentId || undefined,
        ...(kind === 'topic' ? { collectionId: values.collectionId || undefined, fields: values.fields || [] } : {}),
        ...(kind === 'collection'
          ? {
              recordSchema: JSON.parse(values.recordSchema),
              ...(values.sampleEnabled ? { sampleSchema: JSON.parse(values.sampleSchema) } : {}),
            }
          : {}),
      };
      if (kind === 'topic' && values.collectionId && values.fields?.length) {
        message.error(t('Collections.chooseOneView'));
        return;
      }
      const saved =
        kind === 'collection' && !current && !standalone
          ? await createCollection(values.parentId, document)
          : await saveMetadata({
              id: current?.id,
              kind,
              expectedRevision: current?.revision || 0,
              document,
            });
      setDirty(false);
      message.success(t('UnsMeta.saved'));
      onSaved(saved);
    } finally {
      setSaving(false);
    }
  };
  const validateSchema = {
    validator: async (_: unknown, value: string) => {
      try {
        if (JSON.parse(value).type !== 'object') throw new Error();
      } catch {
        throw new Error(t('UnsMeta.invalidSchema'));
      }
    },
  };
  return (
    <ProModal
      footer={undefined}
      open
      width={kind === 'folder' ? 640 : 960}
      className={styles['metadata-editor']}
      title={
        standalone
          ? t(current ? 'Collections.edit' : 'Collections.create')
          : t(
              current
                ? kind === 'collection'
                  ? 'UnsUi.editSchema'
                  : 'UnsMeta.nodeSettings'
                : kind === 'folder'
                  ? 'UnsUi.createFolder'
                  : kind === 'collection'
                    ? 'NamespaceGuide.newDataset'
                    : 'UnsUi.createTopic'
            )
      }
      onCancel={close}
      onOk={save}
      confirmLoading={saving}
      cancelButtonProps={{ disabled: saving }}
      closable={!saving}
      keyboard={!saving}
      okText={t('common.save')}
      cancelText={t('common.cancel')}
      maskClosable={false}
    >
      <Form
        form={form}
        layout="vertical"
        disabled={saving}
        onValuesChange={() => setDirty(true)}
        initialValues={{
          ...initialData,
          ...current?.data,
          parentId: current?.data.parentId || initialData?.parentId || parentId,
          fields: current?.data.fields || initialData?.fields || [],
          recordSchema: json(current?.data.recordSchema || initialData?.recordSchema || emptySchema),
          sampleSchema: json(current?.data.sampleSchema || emptySchema),
          sampleEnabled: Boolean(current?.data.sampleSchema),
        }}
      >
        <div className={styles['form-grid']}>
          <Form.Item
            name="name"
            label={t('UnsMeta.name')}
            rules={[...required, { whitespace: true, message: t('UnsMeta.requiredValue') }]}
          >
            <Input maxLength={MAX_LENGTHS.name} />
          </Form.Item>
          {
            <Form.Item name="parentId" label={t('UnsMeta.parent')}>
              <TreeSelect
                allowClear
                showSearch
                placeholder={t('UnsMeta.root')}
                treeDefaultExpandAll
                treeDataSimpleMode
                treeNodeFilterProp="title"
                treeData={items
                  .filter((item) => item.kind === 'folder' && !blockedParents.has(item.id))
                  .map((item) => ({ id: item.id, pId: item.data.parentId, value: item.id, title: item.data.name }))}
              />
            </Form.Item>
          }
        </div>
        <Form.Item name="description" label={t('UnsMeta.description')}>
          <Input.TextArea rows={2} maxLength={MAX_LENGTHS.description} showCount />
        </Form.Item>
        {kind === 'collection' && (
          <>
            <Form.Item name="recordSchema" label={t('UnsUi.schema')} rules={[validateSchema]}>
              <SchemaEditor />
            </Form.Item>
            <Form.Item name="sampleEnabled" valuePropName="checked">
              <Checkbox>{t('UnsUi.enableHistory')}</Checkbox>
            </Form.Item>
            {sampleEnabled && (
              <Form.Item name="sampleSchema" label={t('UnsUi.historySchema')} rules={[validateSchema]}>
                <SchemaEditor />
              </Form.Item>
            )}
            <HelpTooltip title={t('UnsMeta.schemaEvolution')} />
          </>
        )}
        {current?.kind === 'collection' && connections && (
          <Collapse
            items={[
              {
                key: 'usage',
                label: t('UnsUi.usage'),
                children: <CollectionUsage collection={current} items={items} connections={connections} />,
              },
            ]}
          />
        )}
        {kind === 'topic' && (
          <Collapse
            defaultActiveKey={current ? [] : ['storage']}
            items={[
              {
                key: 'storage',
                label: t('UnsUi.dataReuse'),
                forceRender: true,
                children: (
                  <>
                    <Form.Item
                      name="collectionId"
                      hidden={!!bindings?.length}
                      label={t('UnsMeta.defaultCollection')}
                      dependencies={['fields']}
                      rules={[
                        {
                          validator: async (_, value) => {
                            if (!value && !form.getFieldValue('fields')?.length)
                              throw new Error(t('UnsMeta.collectionRequired'));
                          },
                        },
                      ]}
                    >
                      <CollectionSelect items={items} allowClear />
                    </Form.Item>
                    {selectedCollection && !bindings?.length && (
                      <CollectionPreview collection={selectedCollection} readRecords={readRecords} />
                    )}
                    <Collapse
                      defaultActiveKey={current?.data.fields?.length || initialData?.fields?.length ? ['bindings'] : []}
                      items={[
                        {
                          key: 'bindings',
                          label: (
                            <Space>
                              {t('UnsMeta.advancedBindings')}
                              <HelpTooltip title={t('UnsMeta.bindingHint')} />
                            </Space>
                          ),
                          children: (
                            <Form.List name="fields">
                              {(fields, { add, remove }) => (
                                <>
                                  {fields.map(({ key, name }) => (
                                    <BindingRow
                                      key={key}
                                      index={name}
                                      items={items}
                                      readRecords={readRecords}
                                      remove={() => remove(name)}
                                    />
                                  ))}
                                  <Space>
                                    <ComButton
                                      icon={<Add />}
                                      disabled={fields.length >= 128}
                                      onClick={() => {
                                        add({ kind: 'record', collectionId: form.getFieldValue('collectionId') });
                                        form.setFieldValue('collectionId', undefined);
                                        setDirty(true);
                                      }}
                                    >
                                      {t('UnsMeta.addBinding')}
                                    </ComButton>
                                  </Space>
                                </>
                              )}
                            </Form.List>
                          ),
                        },
                      ]}
                    />
                  </>
                ),
              },
            ]}
          />
        )}
      </Form>
    </ProModal>
  );
}

export default function MetadataEditor(props: ComponentProps<typeof ExistingMetadataEditor>) {
  if (!props.current && !props.standalone && !props.initialData && props.kind === 'topic') {
    return (
      <TopicComposer items={props.items} parentId={props.parentId} onClose={props.onClose} onSaved={props.onSaved} />
    );
  }
  return <ExistingMetadataEditor {...props} />;
}
