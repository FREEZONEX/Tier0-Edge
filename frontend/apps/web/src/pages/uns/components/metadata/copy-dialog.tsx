import { useState } from 'react';
import { App, Form, Input, TreeSelect } from 'antd';
import ProModal from '@/components/pro-modal';
import { openConfirmModal } from '@/components/confirm-modal';
import { useTranslate } from '@/hooks';
import { MAX_LENGTHS } from '@/utils/limits';
import { metadataApi, type UnsMetadata } from '@/apis/core-api/uns-metadata';
import { useIntegrationDirty } from '../source-stream/IntegrationTabs';

export default function CopyDialog({
  source,
  parentId,
  items,
  onClose,
  onSaved,
}: {
  source: UnsMetadata;
  parentId?: string;
  items: UnsMetadata[];
  onClose: () => void;
  onSaved: (meta: UnsMetadata) => void;
}) {
  const t = useTranslate();
  const { modal, message } = App.useApp();
  const [form] = Form.useForm();
  const [saving, setSaving] = useState(false);
  const [dirty, setDirty] = useState(false);
  useIntegrationDirty('namespace-copy', dirty);
  const blocked = new Set(source.kind === 'folder' ? [source.id] : []);
  for (const id of blocked) {
    items.forEach((item) => {
      if (item.data.parentId === id) blocked.add(item.id);
    });
  }
  const namesAt = (target?: string) =>
    new Set(
      items
        .filter(
          (item) =>
            (item.kind === 'folder' || item.kind === 'topic') &&
            !item.deletedAt &&
            (item.data.parentId || '') === (target || '')
        )
        .map((item) => item.data.name.toLowerCase())
    );
  const names = namesAt(parentId);
  let name = source.data.name;
  for (let n = 2; names.has(name.toLowerCase()); n++) {
    const suffix = ` (${n})`;
    name = [...source.data.name].slice(0, MAX_LENGTHS.name - [...suffix].length).join('') + suffix;
  }
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
    const values = await form.validateFields();
    setSaving(true);
    try {
      const saved = await metadataApi.copy(source, {
        name: values.name.trim(),
        parentId: values.parentId || undefined,
        description: values.description?.trim() || '',
      });
      setDirty(false);
      message.success(t('uns.pasteSuccess'));
      onSaved(saved);
    } catch {
      // Keep the draft on a failed request; the interceptor displays the error.
    } finally {
      setSaving(false);
    }
  };
  return (
    <ProModal
      footer={undefined}
      open
      title={t('common.pasteAndEdit')}
      width={640}
      onCancel={close}
      onOk={save}
      confirmLoading={saving}
      cancelButtonProps={{ disabled: saving }}
      closable={!saving}
      keyboard={!saving}
      maskClosable={false}
      okText={t('common.save')}
      cancelText={t('common.cancel')}
    >
      <Form
        form={form}
        layout="vertical"
        disabled={saving}
        onValuesChange={() => setDirty(true)}
        initialValues={{ name, parentId, description: source.data.description }}
      >
        <Form.Item
          name="name"
          label={t('UnsMeta.name')}
          dependencies={['parentId']}
          rules={[
            { required: true, whitespace: true, message: t('UnsMeta.requiredValue') },
            {
              validator: async (_, value: string) => {
                if (value && namesAt(form.getFieldValue('parentId')).has(value.trim().toLowerCase())) {
                  throw new Error(t('common.duplicateResource'));
                }
              },
            },
          ]}
        >
          <Input maxLength={MAX_LENGTHS.name} />
        </Form.Item>
        <Form.Item name="parentId" label={t('UnsMeta.parent')}>
          <TreeSelect
            allowClear
            showSearch
            placeholder={t('UnsMeta.root')}
            treeDefaultExpandAll
            treeDataSimpleMode
            treeNodeFilterProp="title"
            treeData={items
              .filter((item) => item.kind === 'folder' && !blocked.has(item.id))
              .map((item) => ({ id: item.id, pId: item.data.parentId, value: item.id, title: item.data.name }))}
          />
        </Form.Item>
        <Form.Item name="description" label={t('UnsMeta.description')}>
          <Input.TextArea rows={2} maxLength={MAX_LENGTHS.description} showCount />
        </Form.Item>
      </Form>
    </ProModal>
  );
}
