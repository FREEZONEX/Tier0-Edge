import { useEffect, useState } from 'react';
import { Alert, App, Checkbox, Collapse, Empty, Form, Input, Segmented, Space, Steps, TreeSelect } from 'antd';
import { TrashCan } from '@carbon/icons-react';
import ComButton from '@/components/com-button';
import ProModal from '@/components/pro-modal';
import HelpTooltip from '@/components/help-tooltip';
import { openConfirmModal } from '@/components/confirm-modal';
import { useTranslate } from '@/hooks';
import { metadataApi, type FieldBinding, type UnsMetadata, type UnsRecord } from '@/apis/core-api/uns-metadata';
import { MAX_LENGTHS } from '@/utils/limits';
import { useIntegrationDirty } from '../source-stream/IntegrationTabs';
import { fieldPaths } from './schema-editor';
import {
  bindingKey,
  bindingSchema,
  fieldsValid,
  topicDocument,
  topicNameTooLong,
  TOPIC_FIELD_LIMIT,
} from './topic-composer-model';
import CollectionSelect from './collection-select';
import CollectionPreview from './collection-preview';
import TopicRecordPicker from './topic-record-picker';
import TopicComposerPreview from './topic-composer-preview';
import styles from './topic-composer.module.scss';

export default function TopicComposer({
  items,
  parentId,
  onClose,
  onSaved,
}: {
  items: UnsMetadata[];
  parentId?: string;
  onClose: () => void;
  onSaved: (meta: UnsMetadata) => void;
}) {
  const t = useTranslate();
  const { modal, message } = App.useApp();
  const [form] = Form.useForm();
  const [step, setStep] = useState(0);
  const [mode, setMode] = useState<'records' | 'fields'>('records');
  const [collectionId, setCollectionId] = useState<string>();
  const [search, setSearch] = useState('');
  const [fields, setFields] = useState<FieldBinding[]>([]);
  const [records, setRecords] = useState<Record<string, UnsRecord>>({});
  const [dirty, setDirty] = useState(false);
  const [saving, setSaving] = useState(false);
  useIntegrationDirty('topic-composer', dirty);
  useEffect(() => {
    const guard = (event: BeforeUnloadEvent) => {
      if (dirty) event.preventDefault();
    };
    window.addEventListener('beforeunload', guard);
    return () => window.removeEventListener('beforeunload', guard);
  }, [dirty]);
  const collections = items.filter((item) => item.kind === 'collection' && !item.deletedAt);
  const collection = collections.find((item) => item.id === collectionId);
  const selectedIds = [...new Set(fields.map((field) => field.collectionId))];
  const valid = mode === 'records' ? !!collection : fieldsValid(fields);
  const recordsReady = mode === 'records' || selectedIds.every((id) => !!records[id]);
  const lastStep = mode === 'records' ? 1 : 2;
  const steps = mode === 'records' ? ['collection', 'preview'] : ['fields', 'records', 'preview'];
  const update = (next: FieldBinding[]) => {
    setFields(next);
    setDirty(true);
  };
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
  const save = async () => {
    if (!valid || !recordsReady || saving) return;
    const values = await form.validateFields();
    setSaving(true);
    try {
      const saved = await metadataApi.save({
        kind: 'topic',
        expectedRevision: 0,
        document: topicDocument({ mode, name: values.name, parentId: values.parentId, collectionId, fields, records }),
      });
      setDirty(false);
      message.success(t('UnsMeta.saved'));
      onSaved(saved);
    } catch {
      /* API interceptor reports validation and permission errors; retain the draft. */
    } finally {
      setSaving(false);
    }
  };
  const catalog = collections
    .map((collection) => {
      const entries = (['record', 'sample'] as const).flatMap((kind) =>
        fieldPaths(kind === 'record' ? collection.data.recordSchema : collection.data.sampleSchema).map(
          (fieldPath) => ({
            name: fieldPath.split('.').at(-1)!,
            kind,
            collectionId: collection.id,
            recordId: '',
            fieldPath,
          })
        )
      );
      const matches = entries.filter((field) =>
        `${collection.data.name} ${field.fieldPath}`.toLowerCase().includes(search.toLowerCase())
      );
      return { collection, matches };
    })
    .filter(({ matches }) => matches.length);
  return (
    <ProModal
      open
      width={1060}
      title={t('UnsUi.createTopic')}
      className={styles.composer}
      maskClosable={false}
      onCancel={close}
      closable={!saving}
      keyboard={!saving}
      footer={
        <div className={styles.footer}>
          <ComButton disabled={saving} onClick={step ? () => setStep(step - 1) : close}>
            {t(step ? 'TopicCompose.back' : 'common.cancel')}
          </ComButton>
          {step < lastStep ? (
            <ComButton
              type="primary"
              disabled={!valid || (step === 1 && !recordsReady)}
              onClick={() => setStep(step + 1)}
            >
              {t('TopicCompose.next')}
            </ComButton>
          ) : (
            <ComButton type="primary" loading={saving} disabled={!valid || !recordsReady} onClick={save}>
              {t('UnsUi.createTopic')}
            </ComButton>
          )}
        </div>
      }
    >
      {step === 0 && (
        <div className={styles['mode-picker']}>
          <Segmented
            aria-label={t('TopicCompose.content')}
            value={mode}
            options={[
              { value: 'records', label: t('TopicCompose.wholeCollection') },
              { value: 'fields', label: t('TopicCompose.fields') },
            ]}
            onChange={(value) => setMode(value as 'records' | 'fields')}
          />
          <HelpTooltip title={t('TopicCompose.contentHint')} />
        </div>
      )}
      <Steps
        className={styles.steps}
        size="small"
        current={step}
        items={steps.map((key) => ({ title: t(`TopicCompose.${key}`) }))}
      />
      {mode === 'records' && step === 0 && (
        <section className={styles.panel}>
          <CollectionSelect
            items={items}
            value={collectionId}
            onChange={(id) => {
              setCollectionId(id);
              setDirty(true);
              if (!form.getFieldValue('name') || form.getFieldValue('name') === collection?.data.name)
                form.setFieldValue('name', collections.find((item) => item.id === id)?.data.name);
            }}
          />
          {!collections.length && (
            <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={t('TopicCompose.noCollections')} />
          )}
          {collection && <CollectionPreview collection={collection} />}
        </section>
      )}
      {mode === 'fields' && step === 0 && (
        <div className={styles.columns}>
          <section className={styles.panel}>
            <Input.Search
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              placeholder={t('TopicCompose.searchFields')}
              aria-label={t('TopicCompose.searchFields')}
            />
            <div className={styles.catalog}>
              {!catalog.length && (
                <Empty
                  image={Empty.PRESENTED_IMAGE_SIMPLE}
                  description={t(collections.length ? 'TopicCompose.noFields' : 'TopicCompose.noCollections')}
                />
              )}
              <Collapse
                items={catalog.map(({ collection, matches }) => ({
                  key: collection.id,
                  label: collection.data.name,
                  children: (
                    <div className={styles['field-list']}>
                      {matches.map((field) => {
                        const selected = fields.some((item) => bindingKey(item) === bindingKey(field));
                        return (
                          <Checkbox
                            key={bindingKey(field)}
                            checked={selected}
                            disabled={!selected && fields.length >= TOPIC_FIELD_LIMIT}
                            onChange={(event) =>
                              update(
                                event.target.checked
                                  ? [...fields, field]
                                  : fields.filter((item) => bindingKey(item) !== bindingKey(field))
                              )
                            }
                          >
                            {field.fieldPath} · {bindingSchema(field, items)?.type} ·{' '}
                            {t(field.kind === 'sample' ? 'UnsUi.sampleData' : 'UnsUi.recordData')}
                          </Checkbox>
                        );
                      })}
                    </div>
                  ),
                }))}
              />
            </div>
          </section>
          <section className={styles.panel}>
            <strong>
              {t('TopicCompose.outputFields')} · {fields.length}
            </strong>
            {!fields.length && (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={t('TopicCompose.chooseFields')} />
            )}
            {!!fields.length && !valid && <Alert type="warning" message={t('TopicCompose.invalidNames')} />}
            {fields.map((field, index) => (
              <div className={styles['field-row']} key={bindingKey(field)}>
                <div className={styles['field-info']}>
                  <Input
                    aria-label={t('TopicCompose.outputName')}
                    value={field.name}
                    maxLength={MAX_LENGTHS.name}
                    onChange={(event) =>
                      update(fields.map((item, i) => (i === index ? { ...item, name: event.target.value } : item)))
                    }
                  />
                  <small>
                    {collections.find((item) => item.id === field.collectionId)?.data.name} · {field.fieldPath} ·{' '}
                    {bindingSchema(field, items)?.type}
                  </small>
                </div>
                <ComButton
                  type="text"
                  aria-label={t('common.delete')}
                  icon={<TrashCan />}
                  onClick={() => update(fields.filter((_, i) => i !== index))}
                />
              </div>
            ))}
          </section>
        </div>
      )}
      {mode === 'fields' && step === 1 && (
        <Space direction="vertical" style={{ width: '100%' }}>
          <Alert type="info" message={t('TopicCompose.fixedRecords')} />
          {selectedIds.map((id) => (
            <TopicRecordPicker
              key={id}
              collection={collections.find((item) => item.id === id)!}
              value={records[id]}
              onChange={(record) => {
                setRecords((previous) => ({ ...previous, [id]: record }));
                setDirty(true);
              }}
            />
          ))}
        </Space>
      )}
      {step === lastStep && (
        <div className={styles.columns}>
          <Form
            form={form}
            layout="vertical"
            disabled={saving}
            initialValues={{ parentId }}
            onValuesChange={() => setDirty(true)}
          >
            <Form.Item
              name="name"
              label={t('UnsMeta.name')}
              rules={[
                { required: true, whitespace: true, message: t('UnsMeta.requiredValue') },
                {
                  validator: async (_, value: string) => {
                    if (value?.includes('/') || value?.includes(String.fromCharCode(0)))
                      throw new Error(t('TopicCompose.invalidName'));
                    if (value && topicNameTooLong(value))
                      throw new Error(t('uns.namespaceSegmentTooLong', { length: MAX_LENGTHS.name }));
                  },
                },
              ]}
            >
              <Input />
            </Form.Item>
            <Form.Item name="parentId" label={t('UnsMeta.parent')}>
              <TreeSelect
                allowClear
                showSearch
                treeDataSimpleMode
                treeNodeFilterProp="title"
                placeholder={t('UnsMeta.root')}
                treeData={items
                  .filter((item) => item.kind === 'folder')
                  .map((item) => ({ id: item.id, pId: item.data.parentId, value: item.id, title: item.data.name }))}
              />
            </Form.Item>
            {(mode === 'records' ? [collectionId!] : selectedIds).map((id) => (
              <div key={id} className={styles['record-picker']}>
                <strong>{collections.find((item) => item.id === id)?.data.name}</strong>
                {mode === 'fields' && <span>{records[id].id}</span>}
              </div>
            ))}
          </Form>
          <section className={styles.panel}>
            {mode === 'records' && collection ? (
              <CollectionPreview collection={collection} />
            ) : (
              <TopicComposerPreview fields={fields} items={items} records={records} />
            )}
          </section>
        </div>
      )}
    </ProModal>
  );
}
