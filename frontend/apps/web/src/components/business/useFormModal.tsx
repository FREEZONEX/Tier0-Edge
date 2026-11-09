import { useState } from 'react';
import { Flex, Form } from 'antd';
import OperationForm, { type OperationFormProps } from '../operation-form';
import ProModal from '../pro-modal';
import ComButton from '../com-button';
import { useTranslate } from '@/hooks';

const useFormModal = ({
  onSave,
  formItemOptions,
}: {
  onSave?: () => void;
  formItemOptions: (isEdit?: boolean) => OperationFormProps['formItemOptions'];
}) => {
  const [isEdit, setEdit] = useState(false);
  const [open, setOpen] = useState(false);
  const formatMessage = useTranslate();
  const [form] = Form.useForm();
  const openFormModal = () => {
    setOpen(true);
    setEdit(true);
  };

  const onCancel = () => {
    setOpen(false);
    form.resetFields();
  };

  const FormModalDom = (
    <ProModal open={open} onCancel={onCancel} title={'123'} size="xxs">
      <OperationForm
        form={form}
        onCancel={onCancel}
        onSave={onSave}
        formConfig={{
          layout: 'vertical',
          labelCol: { span: 24 },
          wrapperCol: { span: 124 },
        }}
        formItemOptions={formItemOptions(isEdit)}
        style={{ padding: 0 }}
        footer={
          <Flex gap="10px" justify="end">
            <ComButton color="default" variant="filled" onClick={onCancel} title={formatMessage('common.cancel')}>
              {formatMessage('common.cancel')}
            </ComButton>
            <ComButton type="primary" variant="solid" onClick={onSave} title={formatMessage('common.save')}>
              {formatMessage('common.save')}
            </ComButton>
          </Flex>
        }
      />
    </ProModal>
  );

  return {
    openFormModal,
    FormModalDom,
  };
};

export default useFormModal;
