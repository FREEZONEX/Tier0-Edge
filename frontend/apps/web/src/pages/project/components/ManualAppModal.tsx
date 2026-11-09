import { createManualProjectApp, uploadAttachment } from '@/apis/core-api';
import ProModal from '@/components/pro-modal';
import { useTranslate } from '@/hooks';
import { Box } from '@/components/lucide-icon/carbon';
import {
  App,
  Button,
  Form,
  Input,
  InputNumber,
  Segmented,
  Switch,
  Upload,
  type UploadFile,
  type UploadProps,
} from 'antd';
import type { RcFile } from 'antd/es/upload';
import { forwardRef, useImperativeHandle, useMemo, useState, type CSSProperties } from 'react';
import { useParams } from 'react-router';
import styles from './ManualAppModal.module.scss';

const DEFAULT_CONTAINER_NAME = 'tier0-prediction';
const DEFAULT_CONTAINER_PORT = 80;
const DEFAULT_TARGET_PATH = '/';
const CONTAINER_NAME_RE = /^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,252}$/;
const COMPACT_ITEM_STYLE: CSSProperties = { marginBottom: 12 };
const MODE_GRID_STYLE: CSSProperties = {
  display: 'grid',
  gridTemplateColumns: 'minmax(0, 1fr) max-content',
  columnGap: 16,
  alignItems: 'end',
};
const PROXY_GRID_STYLE: CSSProperties = {
  display: 'grid',
  gridTemplateColumns: 'minmax(0, 1fr) 112px 150px',
  columnGap: 12,
};

export interface ManualAppModalRef {
  onOpen: () => void;
  onClose: () => void;
}

export interface ManualAppModalProps {
  refreshRequest?: () => void;
}

const getUploadedItem = (res: any) => {
  const list = res?.list ?? res?.data?.list;
  return Array.isArray(list) ? list[0] : null;
};

const ManualAppModal = forwardRef<ManualAppModalRef, ManualAppModalProps>(({ refreshRequest }, ref) => {
  const { projectId } = useParams<{ projectId: string }>();
  const formatMessage = useTranslate();
  const { message } = App.useApp();
  const [form] = Form.useForm();
  const [visible, setVisible] = useState(false);
  const [saving, setSaving] = useState(false);
  const [iconAssetId, setIconAssetId] = useState<number>();
  const [iconFileList, setIconFileList] = useState<UploadFile[]>([]);

  const onOpen = () => {
    form.setFieldsValue({
      accessMode: 'proxy',
      name: '',
      containerName: '',
      containerPort: DEFAULT_CONTAINER_PORT,
      targetPath: DEFAULT_TARGET_PATH,
      entryUrl: '',
      openInPlatform: true,
    });
    setIconAssetId(undefined);
    setIconFileList([]);
    setVisible(true);
  };

  const onClose = () => {
    setVisible(false);
    setSaving(false);
    form.resetFields();
    setIconAssetId(undefined);
    setIconFileList([]);
  };

  const iconUploadProps: UploadProps = useMemo(
    () => ({
      accept: 'image/*',
      maxCount: 1,
      listType: 'picture-card',
      fileList: iconFileList,
      beforeUpload: async (file: RcFile) => {
        if (!file.type.startsWith('image/')) {
          message.warning(formatMessage('apps.iconImageOnly'));
          return Upload.LIST_IGNORE;
        }
        setIconFileList([{ uid: file.uid, name: file.name, status: 'uploading' }]);
        try {
          const res = await uploadAttachment([{ value: file, name: 'files', fileName: file.name }], {
            ownerType: 'projectManualApp',
            ownerId: projectId,
            source: 'project',
          });
          const uploaded = getUploadedItem(res);
          const fileId = Number(uploaded?.fileId || uploaded?.id || uploaded?.objectName || 0);
          if (!fileId) {
            throw new Error('upload failed');
          }
          setIconAssetId(fileId);
          setIconFileList([
            {
              uid: file.uid,
              name: file.name,
              status: 'done',
              url: `/api/core/assets/${fileId}/download`,
            },
          ]);
        } catch {
          setIconFileList([{ uid: file.uid, name: file.name, status: 'error' }]);
          message.error(formatMessage('common.serverBusy'));
        }
        return false;
      },
      onRemove: () => {
        setIconAssetId(undefined);
        setIconFileList([]);
      },
    }),
    [formatMessage, iconFileList, message, projectId]
  );

  const onSave = async () => {
    const values = await form.validateFields();
    if (!projectId) {
      message.warning(formatMessage('common.serverBusy'));
      return;
    }
    setSaving(true);
    try {
      await createManualProjectApp(projectId, {
        name: values.name.trim(),
        description: values.description?.trim(),
        accessMode: values.accessMode,
        containerName: values.accessMode === 'proxy' ? values.containerName?.trim() : undefined,
        containerPort: values.accessMode === 'proxy' ? values.containerPort : undefined,
        targetPath: values.accessMode === 'proxy' ? values.targetPath?.trim() : undefined,
        entryUrl: values.accessMode === 'external' ? values.entryUrl?.trim() : undefined,
        openInPlatform: values.openInPlatform,
        iconAssetId,
      });
      message.success(formatMessage('common.optsuccess'));
      refreshRequest?.();
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
      title={formatMessage('project.addApp')}
      width={640}
      maskClosable={false}
      fullScreenable={false}
    >
      <Form
        form={form}
        layout="vertical"
        requiredMark={false}
        initialValues={{
          accessMode: 'proxy',
          name: '',
          containerName: '',
          containerPort: DEFAULT_CONTAINER_PORT,
          targetPath: DEFAULT_TARGET_PATH,
          openInPlatform: true,
        }}
      >
        <Form.Item
          name="name"
          label={formatMessage('common.name')}
          rules={[{ required: true }]}
          style={COMPACT_ITEM_STYLE}
        >
          <Input placeholder={formatMessage('apps.namePlaceholder')} maxLength={64} />
        </Form.Item>
        <Form.Item name="description" label={formatMessage('common.description')} style={COMPACT_ITEM_STYLE}>
          <Input.TextArea rows={2} placeholder={formatMessage('apps.descriptionPlaceholder')} maxLength={200} />
        </Form.Item>
        <Form.Item label={formatMessage('apps.icon')} style={COMPACT_ITEM_STYLE}>
          <Upload {...iconUploadProps}>{iconFileList.length ? null : <Box size={28} strokeWidth={1.5} />}</Upload>
        </Form.Item>
        <div style={MODE_GRID_STYLE}>
          <Form.Item
            name="accessMode"
            label={formatMessage('apps.accessMode')}
            rules={[{ required: true }]}
            style={COMPACT_ITEM_STYLE}
          >
            <Segmented
              className={styles.accessModeSegmented}
              options={[
                { label: formatMessage('apps.accessModeProxy'), value: 'proxy' },
                { label: formatMessage('apps.accessModeExternal'), value: 'external' },
              ]}
            />
          </Form.Item>
          <Form.Item style={COMPACT_ITEM_STYLE}>
            <div className={styles.openInPlatformRow}>
              <span>{formatMessage('apps.openInPlatform')}</span>
              <div className={styles.themeSwitch}>
                <Form.Item name="openInPlatform" valuePropName="checked" noStyle>
                  <Switch />
                </Form.Item>
              </div>
            </div>
          </Form.Item>
        </div>
        <Form.Item noStyle shouldUpdate={(prev, next) => prev.accessMode !== next.accessMode}>
          {({ getFieldValue }) =>
            getFieldValue('accessMode') === 'proxy' ? (
              <div style={PROXY_GRID_STYLE}>
                <Form.Item
                  name="containerName"
                  label={formatMessage('apps.containerName')}
                  style={COMPACT_ITEM_STYLE}
                  rules={[
                    { required: true },
                    {
                      validator: (_, value) => {
                        const text = String(value || '').trim();
                        if (!text || !CONTAINER_NAME_RE.test(text) || text.toLowerCase() === 'localhost') {
                          return Promise.reject(new Error(formatMessage('apps.invalidContainerName')));
                        }
                        return Promise.resolve();
                      },
                    },
                  ]}
                >
                  <Input
                    placeholder={formatMessage('apps.containerNamePlaceholder')}
                    maxLength={253}
                    onBlur={(event) => {
                      const containerName = event.target.value.trim();
                      const name = form.getFieldValue('name');
                      if (containerName && (!name || name === DEFAULT_CONTAINER_NAME)) {
                        form.setFieldValue('name', containerName);
                      }
                    }}
                  />
                </Form.Item>
                <Form.Item
                  name="containerPort"
                  label={formatMessage('apps.containerPort')}
                  style={COMPACT_ITEM_STYLE}
                  rules={[{ required: true, message: formatMessage('apps.invalidContainerPort') }]}
                >
                  <InputNumber
                    min={1}
                    max={65535}
                    precision={0}
                    style={{ width: '100%' }}
                    placeholder={formatMessage('apps.containerPortPlaceholder')}
                  />
                </Form.Item>
                <Form.Item
                  name="targetPath"
                  label={formatMessage('apps.targetPath')}
                  style={COMPACT_ITEM_STYLE}
                  rules={[
                    { required: true },
                    {
                      validator: (_, value) => {
                        const text = String(value || '').trim();
                        if (!text.startsWith('/') || text.startsWith('//')) {
                          return Promise.reject(new Error(formatMessage('apps.invalidTargetPath')));
                        }
                        return Promise.resolve();
                      },
                    },
                  ]}
                >
                  <Input placeholder={formatMessage('apps.targetPathPlaceholder')} />
                </Form.Item>
              </div>
            ) : (
              <Form.Item
                name="entryUrl"
                label={formatMessage('apps.entryUrl')}
                style={COMPACT_ITEM_STYLE}
                rules={[
                  { required: true },
                  {
                    validator: (_, value) => {
                      const text = String(value || '').trim();
                      if (!text) {
                        return Promise.resolve();
                      }
                      if (text.startsWith('/') && !text.startsWith('//')) {
                        return Promise.resolve();
                      }
                      try {
                        const url = new URL(text);
                        if (url.protocol === 'http:' || url.protocol === 'https:') {
                          return Promise.resolve();
                        }
                      } catch {
                        return Promise.reject(new Error(formatMessage('project.invalidAppUrl')));
                      }
                      return Promise.reject(new Error(formatMessage('project.invalidAppUrl')));
                    },
                  },
                ]}
              >
                <Input placeholder={formatMessage('apps.externalUrlPlaceholder')} />
              </Form.Item>
            )
          }
        </Form.Item>
      </Form>
      <Button loading={saving} color="primary" variant="solid" block onClick={onSave}>
        {formatMessage('common.save')}
      </Button>
    </ProModal>
  );
});

export default ManualAppModal;
