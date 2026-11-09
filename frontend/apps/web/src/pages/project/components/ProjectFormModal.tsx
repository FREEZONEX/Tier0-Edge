import { createProject, updateProject } from '@/apis/core-api/project';
import ComButton from '@/components/com-button';
import OperationForm from '@/components/operation-form';
import ProModal from '@/components/pro-modal';
import { useTranslate } from '@/hooks';
import { App, Flex, Form } from 'antd';
import { forwardRef, useImperativeHandle, useState } from 'react';

export interface ProjectFormModalRef {
  onOpen: (type: number, props?: any) => void;
  onClose: () => void;
}

export interface ProjectFormModalProps {
  title?: string;
  refreshRequest?: () => void;
}

const ProjectFormModal = forwardRef<ProjectFormModalRef, ProjectFormModalProps>(({ refreshRequest, title }, ref) => {
  const [visible, setVisible] = useState(false);
  const formatMessage = useTranslate();
  const [form] = Form.useForm();
  const { message } = App.useApp();
  const labelStyle = {
    fontSize: 12,
    lineHeight: '18px',
    fontWeight: 400,
    letterSpacing: '0.16px',
    color: 'var(--ui-text-color)',
    opacity: 0.65,
  } as const;

  const renderLabel = (messageKey: string) => <span style={labelStyle}>{formatMessage(messageKey)}</span>;

  const onOpen = (props: any) => {
    form.setFieldsValue({
      ...(props || {}),
    });
    setVisible(true);
  };

  const onClose = () => {
    form.resetFields();
    setVisible(false);
  };

  const onSave = async () => {
    const value = await form.validateFields();
    const request = value?.id ? updateProject(value.id, value) : createProject(value);
    return request.then(() => {
      onClose?.();
      refreshRequest?.();
      message.success(formatMessage('common.optsuccess'));
    });
  };

  useImperativeHandle(ref, () => ({
    onOpen,
    onClose,
  }));

  return (
    <ProModal
      open={visible}
      onCancel={onClose}
      title={title}
      width={500}
      fullScreenable={false}
      styles={{
        body: { paddingBlockStart: 0 },
      }}
    >
      {() => {
        return (
          <OperationForm
            form={form}
            formConfig={{
              layout: 'vertical',
              labelCol: { span: 24 },
              wrapperCol: { span: 24 },
              requiredMark: true,
            }}
            formItemOptions={[
              {
                name: 'id',
                hidden: true,
              },
              {
                name: 'name',
                label: renderLabel('common.name'),
                rules: [
                  { required: true, message: formatMessage('project.nameRequired') },
                  { pattern: /^[\u4e00-\u9fa5a-zA-Z0-9_-]+$/, message: formatMessage('uns.nameFormat') },
                ],
                style: { marginBottom: 16 },
                properties: {
                  disabled: !!form.getFieldValue('id'),
                  placeholder: formatMessage('project.namePlaceholder'),
                  allowClear: true,
                  maxLength: 63,
                },
              },
              {
                name: 'description',
                label: renderLabel('common.description'),
                style: { marginBottom: 16 },
                properties: {
                  placeholder: formatMessage('project.descriptionPlaceholder'),
                  allowClear: true,
                  maxLength: 255,
                },
              },
            ]}
            style={{ padding: 0 }}
            footer={
              <Flex gap="8px" justify="end" style={{ marginTop: 8 }}>
                <ComButton color="default" variant="filled" onClick={onClose} title={formatMessage('common.cancel')}>
                  {formatMessage('common.cancel')}
                </ComButton>
                <ComButton
                  type="primary"
                  variant="solid"
                  style={{ borderRadius: 3 }}
                  onClick={onSave}
                  title={formatMessage('common.save')}
                >
                  {formatMessage('common.save')}
                </ComButton>
              </Flex>
            }
          />
        );
      }}
    </ProModal>
  );
});

export default ProjectFormModal;
