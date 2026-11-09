import { forwardRef, useImperativeHandle, useState } from 'react';
import ProModal from '@/components/pro-modal';
import { useTranslate } from '@/hooks';
import OperationForm from '@/components/operation-form';
import { App, Flex, Form } from 'antd';
import ComButton from '@/components/com-button';
import { getGroupList, optGroup } from '@/apis/core-api/group.ts';

export interface MoveGroupModalRef {
  onOpen: (type: number, props: any) => void;
  onClose: () => void;
}

export interface MoveGroupModalProps {
  [key: string]: any;
}

const ROOT_GROUP_ID = '-9999';

const MoveGroupModal = forwardRef<MoveGroupModalRef, MoveGroupModalProps>(({ refreshRequest }, ref) => {
  const [visible, setVisible] = useState(false);
  const [type, setType] = useState(1);
  const [oldId, setOldId] = useState('0');
  const [flowName, setFlowName] = useState('');
  const formatMessage = useTranslate();
  const [form] = Form.useForm();
  const { message } = App.useApp();

  const onOpen = (type: number, props: any) => {
    const currentGroupId = props?.id ? props.id + '' : '0';
    form.setFieldsValue({
      ...props,
      id: currentGroupId === '0' ? ROOT_GROUP_ID : currentGroupId,
    });
    setOldId(currentGroupId);
    setType(type);
    setFlowName(props?.name || '');
    setVisible(true);
  };

  const onClose = () => {
    form.resetFields();
    setFlowName('');
    setVisible(false);
  };

  const onSave = async () => {
    const value = await form.validateFields();
    return optGroup({
      type,
      bizId: value?.bizId,
      id: value?.id === ROOT_GROUP_ID ? oldId : value?.id,
      status: value?.id !== ROOT_GROUP_ID,
    }).then(() => {
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
      destroyOnHidden
      open={visible}
      onCancel={onClose}
      title={
        <span style={{ overflowWrap: 'anywhere' }}>
          {flowName ? formatMessage('flow.moveNamedToGroup', { name: flowName }) : formatMessage('uns.moveToGroup')}
        </span>
      }
      width={500}
      styles={{
        body: {
          paddingBlockStart: 0,
        },
      }}
    >
      {() => {
        return (
          <OperationForm
            form={form}
            onCancel={onClose}
            onSave={onSave}
            formConfig={{
              layout: 'vertical',
              labelCol: { span: 24 },
              wrapperCol: { span: 124 },
            }}
            formItemOptions={[
              {
                name: 'type',
                hidden: true,
              },
              {
                name: 'bizId',
                hidden: true,
              },
              {
                name: 'id',
                type: 'Select',
                label: formatMessage('common.group'),
                rules: [{ required: true, message: formatMessage('common.select') }],
                properties: {
                  isRequest: visible,
                  filterOption: false,
                  // 同步提供根目录标签，避免异步目录加载前直接渲染哨兵值。
                  otherOptions: [
                    {
                      label: formatMessage('uns.rootDirectory'),
                      value: ROOT_GROUP_ID,
                    },
                  ],
                  api: (name?: string) =>
                    getGroupList({ page: 1, pageSize: 1000, name, type }).then(
                      (res) =>
                        res?.map((item: any) => ({
                          ...item,
                          label: item.name,
                          value: item.id + '',
                        })) || []
                    ),
                  showSearch: true,
                },
              },
            ]}
            style={{ padding: 0 }}
            footer={
              <Flex gap="10px" justify="end">
                <ComButton color="default" variant="filled" onClick={onClose} title={formatMessage('common.cancel')}>
                  {formatMessage('common.cancel')}
                </ComButton>
                <ComButton type="primary" variant="solid" onClick={onSave} title={formatMessage('common.save')}>
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

export default MoveGroupModal;
