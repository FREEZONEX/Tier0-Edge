import { updateProjectApp } from '@/apis/core-api';
import ProModal from '@/components/pro-modal';
import { useTranslate } from '@/hooks';
import type { AppItem } from '@/pages/project/types';
import { projectAppMediaUrl, uploadProjectAppMedia } from '@/utils/project-app-media';
import { App, Button, Form, Input, Typography } from 'antd';
import type { RcFile } from 'antd/es/upload';
import { forwardRef, useImperativeHandle, useState, type CSSProperties } from 'react';
import { useParams } from 'react-router';
import AppMediaFields, { type AppMediaKind } from './AppMediaFields';
import styles from './EditAppModal.module.scss';

const COMPACT_ITEM_STYLE: CSSProperties = { marginBottom: 12 };

export interface EditAppModalRef {
  onOpen: (app: AppItem) => void;
  onClose: () => void;
}

export interface EditAppModalProps {
  refreshRequest?: () => void | Promise<void>;
}

const EditAppModal = forwardRef<EditAppModalRef, EditAppModalProps>(({ refreshRequest }, ref) => {
  const { projectId } = useParams<{ projectId: string }>();
  const formatMessage = useTranslate();
  const { message } = App.useApp();
  const [form] = Form.useForm();
  const [visible, setVisible] = useState(false);
  const [saving, setSaving] = useState(false);
  const [editingApp, setEditingApp] = useState<AppItem>();
  const [iconAssetId, setIconAssetId] = useState<number>();
  const [coverAssetId, setCoverAssetId] = useState<number>();
  const [iconUrl, setIconUrl] = useState<string>();
  const [coverUrl, setCoverUrl] = useState<string>();
  const [uploadingKind, setUploadingKind] = useState<AppMediaKind>();

  const onOpen = (app: AppItem) => {
    setEditingApp(app);
    setIconAssetId(app.iconAssetId);
    setCoverAssetId(app.coverAssetId);
    setIconUrl(app.iconUrl || projectAppMediaUrl(app.iconAssetId));
    setCoverUrl(app.coverUrl || projectAppMediaUrl(app.coverAssetId));
    form.setFieldsValue({
      name: app.name,
      description: app.description || '',
    });
    setVisible(true);
  };

  const onClose = () => {
    setVisible(false);
    setSaving(false);
    setEditingApp(undefined);
    setIconAssetId(undefined);
    setCoverAssetId(undefined);
    setIconUrl(undefined);
    setCoverUrl(undefined);
    setUploadingKind(undefined);
    form.resetFields();
  };

  const onMediaSelect = async (kind: AppMediaKind, file: RcFile) => {
    if (!editingApp) {
      return;
    }
    setUploadingKind(kind);
    try {
      const assetId = await uploadProjectAppMedia(file, editingApp.appId);
      if (kind === 'icon') {
        setIconAssetId(assetId);
        setIconUrl(projectAppMediaUrl(assetId));
      } else {
        setCoverAssetId(assetId);
        setCoverUrl(projectAppMediaUrl(assetId));
      }
    } catch {
      message.error(formatMessage('common.serverBusy'));
    } finally {
      setUploadingKind(undefined);
    }
  };

  const onSave = async () => {
    const values = await form.validateFields();
    if (!projectId || !editingApp) {
      message.warning(formatMessage('common.serverBusy'));
      return;
    }
    setSaving(true);
    try {
      await updateProjectApp(projectId, String(editingApp.appId), {
        name: values.name.trim(),
        description: values.description?.trim() || '',
        iconAssetId: iconAssetId || 0,
        coverAssetId: coverAssetId || 0,
      });
      message.success(formatMessage('common.optsuccess'));
      await refreshRequest?.();
      onClose();
    } finally {
      setSaving(false);
    }
  };

  useImperativeHandle(ref, () => ({
    onOpen,
    onClose,
  }));

  return (
    <ProModal
      open={visible}
      onCancel={onClose}
      title={formatMessage('project.appSetting', {}, 'Edit App')}
      width={544}
      fullScreenable={false}
      destroyOnHidden
      styles={{ body: { maxHeight: '72vh', overflowY: 'auto' } }}
    >
      <Form form={form} layout="vertical" requiredMark={false}>
        <Form.Item
          name="name"
          label={formatMessage('common.name')}
          rules={[{ required: true }]}
          style={COMPACT_ITEM_STYLE}
        >
          <Input placeholder={formatMessage('apps.namePlaceholder')} maxLength={64} />
        </Form.Item>
        <div className={styles['meta-grid']} style={COMPACT_ITEM_STYLE}>
          <div>
            <label className={styles['meta-label']}>{formatMessage('common.appId')}</label>
            <Input value={editingApp ? editingApp.sourceAppId || String(editingApp.appId) : ''} disabled />
          </div>
          <div>
            <label className={styles['meta-label']}>{formatMessage('common.version')}</label>
            <Input value={editingApp?.version || ''} disabled />
          </div>
        </div>
        {editingApp?.internalUrl ? (
          <Form.Item label={formatMessage('project.internalUrl')} style={COMPACT_ITEM_STYLE}>
            <Typography.Text copyable>{editingApp.internalUrl}</Typography.Text>
          </Form.Item>
        ) : null}
        <Form.Item name="description" label={formatMessage('common.description')} className={styles.descriptionItem}>
          <Input.TextArea
            rows={3}
            placeholder={formatMessage('apps.descriptionPlaceholder')}
            maxLength={200}
            showCount
          />
        </Form.Item>
      </Form>
      <AppMediaFields
        coverUrl={coverUrl}
        iconUrl={iconUrl}
        disabled={saving || Boolean(uploadingKind)}
        uploadingKind={uploadingKind}
        onSelect={onMediaSelect}
      />
      <div className={styles.actions}>
        <Button
          color="default"
          variant="filled"
          disabled={saving || Boolean(uploadingKind)}
          title={formatMessage('common.cancel')}
          onClick={onClose}
        >
          {formatMessage('common.cancel')}
        </Button>
        <Button loading={saving} disabled={Boolean(uploadingKind)} color="primary" variant="solid" onClick={onSave}>
          {formatMessage('common.save')}
        </Button>
      </div>
    </ProModal>
  );
});

EditAppModal.displayName = 'EditAppModal';

export default EditAppModal;
