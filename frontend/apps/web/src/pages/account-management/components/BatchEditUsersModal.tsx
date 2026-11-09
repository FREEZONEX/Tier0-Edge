import { Button, Form, Select } from 'antd';
import { useEffect, type FC } from 'react';

import HomePageSelect from '@/components/home-page-select';
import ProModal from '@/components/pro-modal';
import { useTranslate } from '@/hooks';
import type { ResourceProps } from '@/stores/types';

import styles from './BatchEditUsersModal.module.scss';

type BatchEditUsersModalProps = {
  open: boolean;
  loading: boolean;
  selectedCount: number;
  resources?: ResourceProps[];
  roleOptions: Array<{ label: string; value: string }>;
  onCancel: () => void;
  onSubmit: (values: BatchEditFormValues) => Promise<void>;
};

export type BatchEditFormValues = {
  homePage?: string;
  roleId?: string;
};

const BatchEditUsersModal: FC<BatchEditUsersModalProps> = ({
  open,
  loading,
  selectedCount,
  resources = [],
  roleOptions,
  onCancel,
  onSubmit,
}) => {
  const [form] = Form.useForm<BatchEditFormValues>();
  const formatMessage = useTranslate();

  useEffect(() => {
    if (open) form.resetFields();
  }, [form, open]);

  const handleCancel = () => {
    form.resetFields();
    onCancel();
  };

  const clearChangeErrors = () => {
    if (!form.getFieldValue('homePage') && !form.getFieldValue('roleId')) return;
    form.setFields([
      { name: 'homePage', errors: [] },
      { name: 'roleId', errors: [] },
    ]);
  };

  const handleSubmit = (values: BatchEditFormValues) => {
    const homePage = values.homePage?.trim();
    if (!homePage && !values.roleId) {
      const error = formatMessage('account.bulk.changeRequired');
      form.setFields([
        { name: 'homePage', errors: [error] },
        { name: 'roleId', errors: [error] },
      ]);
      return;
    }
    void onSubmit({ homePage, roleId: values.roleId });
  };

  return (
    <ProModal
      size="xs"
      open={open}
      title={formatMessage('account.bulk.editTitle')}
      fullScreenable={false}
      destroyOnHidden
      maskClosable={false}
      onCancel={handleCancel}
    >
      <div className={styles['selected-summary']}>
        <span>{formatMessage('account.bulk.selectedUsers')}</span>
        <strong>{selectedCount}</strong>
      </div>
      <Form form={form} layout="vertical" onFinish={handleSubmit} onValuesChange={clearChangeErrors}>
        <Form.Item label={formatMessage('account.homePage')} name="homePage">
          <HomePageSelect
            enabled={false}
            resources={resources}
            placeholder={formatMessage('account.bulk.homePagePlaceholder')}
            allowClear
            showSearch
          />
        </Form.Item>
        <Form.Item label={formatMessage('account.role')} name="roleId">
          <Select
            allowClear
            showSearch
            options={roleOptions}
            optionFilterProp="label"
            placeholder={formatMessage('account.bulk.rolePlaceholder')}
          />
        </Form.Item>
      </Form>
      <div className={styles['modal-footer']}>
        <Button disabled={loading} onClick={handleCancel}>
          {formatMessage('common.cancel')}
        </Button>
        <Button type="primary" loading={loading} onClick={() => form.submit()}>
          {formatMessage('common.save')}
        </Button>
      </div>
    </ProModal>
  );
};

export default BatchEditUsersModal;
