import { useState } from 'react';
import { v4 as uuidv4 } from 'uuid';
import { App, Form, Input, InputNumber, Modal, Select } from 'antd';
import { useTranslate } from '@/hooks';
import { metadataApi, type UnsMetadata, type UnsRecord } from '@/apis/core-api/uns-metadata';
import { MAX_LENGTHS } from '@/utils/limits';
import styles from '../index.module.scss';
export default function RecordEditor({
  collection,
  record,
  onClose,
  onSaved,
}: {
  collection: UnsMetadata;
  record?: UnsRecord;
  onClose: () => void;
  onSaved: () => void;
}) {
  const t = useTranslate();
  const { message } = App.useApp();
  const [form] = Form.useForm();
  const [saving, setSaving] = useState(false);
  const schema = collection.data.recordSchema!;
  const save = async () => {
    const values = await form.validateFields();
    setSaving(true);
    try {
      const data = { ...record?.data, ...values.data };
      for (const [name, field] of Object.entries(schema.properties || {})) {
        if (field.type === 'null') data[name] = null;
        if ((field.type === 'object' || field.type === 'array') && typeof data[name] === 'string')
          data[name] = JSON.parse(data[name] as string);
        if (data[name] === undefined) delete data[name];
      }
      const saved = await metadataApi.saveRecord(collection.id, {
        id: record?.id || values.id || uuidv4(),
        expectedRevision: record?.revision || 0,
        data,
      });
      if ('deliveryState' in saved) message.info(t('collectionStorage.accepted'));
      else message.success(t('UnsMeta.saved'));
      onSaved();
    } finally {
      setSaving(false);
    }
  };
  const initial = Object.fromEntries(
    Object.entries(record?.data || {}).map(([name, value]) => [
      name,
      value !== null && typeof value === 'object' ? JSON.stringify(value, null, 2) : value,
    ])
  );
  return (
    <Modal
      open
      width={700}
      title={t(record ? 'Collections.editRecord' : 'Collections.addRecord')}
      onCancel={onClose}
      onOk={save}
      confirmLoading={saving}
      maskClosable={!saving}
    >
      <Form form={form} layout="vertical" initialValues={{ id: record?.id, data: initial }} disabled={saving}>
        <Form.Item name="id" label={t('UnsMeta.recordId')}>
          <Input disabled={!!record} maxLength={128} placeholder={t('Collections.autoId')} />
        </Form.Item>
        {Object.entries(schema.properties || {}).map(([name, field]) => (
          <Form.Item
            key={name}
            name={['data', name]}
            label={field.title || name}
            rules={
              schema.required?.includes(name) && field.type !== 'null'
                ? [{ required: true, message: t('UnsMeta.requiredValue') }]
                : []
            }
          >
            {field.enum ? (
              <Select options={field.enum.map((value) => ({ value: value as string, label: String(value) }))} />
            ) : field.type === 'boolean' ? (
              <Select
                options={[
                  { value: true, label: t('UnsMeta.trueValue') },
                  { value: false, label: t('UnsMeta.falseValue') },
                ]}
              />
            ) : field.type === 'number' || field.type === 'integer' ? (
              <InputNumber
                className={styles.full}
                precision={field.type === 'integer' ? 0 : undefined}
                min={field.minimum}
                max={field.maximum}
              />
            ) : field.type === 'object' || field.type === 'array' ? (
              <Input.TextArea rows={4} />
            ) : (
              <Input disabled={field.type === 'null'} maxLength={MAX_LENGTHS.description} />
            )}
          </Form.Item>
        ))}
      </Form>
    </Modal>
  );
}
