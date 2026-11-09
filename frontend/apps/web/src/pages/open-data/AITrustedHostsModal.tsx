import { useCallback, useEffect, useState } from 'react';
import { Add, TrashCan } from '@carbon/icons-react';
import { Alert, App, Button, Empty, Form, Input, Spin } from 'antd';
import { queryAITrustedHosts, saveAITrustedHosts } from '@/apis/core-api/open-data';
import ProModal from '@/components/pro-modal';
import { useTranslate } from '@/hooks';
import styles from './index.module.scss';

type AITrustedHostsModalProps = {
  open: boolean;
  onCancel: () => void;
};

type TrustedHostsFormValues = {
  hosts: Array<{ value: string }>;
};

const AITrustedHostsModal = ({ open, onCancel }: AITrustedHostsModalProps) => {
  const formatMessage = useTranslate('OpenData');
  const formatCommon = useTranslate();
  const { message } = App.useApp();
  const [form] = Form.useForm<TrustedHostsFormValues>();
  const [loading, setLoading] = useState(false);
  const [loadFailed, setLoadFailed] = useState(false);
  const [saving, setSaving] = useState(false);

  const loadHosts = useCallback(async () => {
    setLoading(true);
    setLoadFailed(false);
    try {
      const result = await queryAITrustedHosts();
      form.setFieldsValue({ hosts: (result?.hosts || []).map((value: string) => ({ value })) });
    } catch {
      setLoadFailed(true);
      message.error(formatMessage('aiTrustedHostsLoadFailed'));
    } finally {
      setLoading(false);
    }
  }, [form, formatMessage, message]);

  useEffect(() => {
    if (!open) return;
    const timer = window.setTimeout(() => void loadHosts(), 0);
    return () => window.clearTimeout(timer);
  }, [loadHosts, open]);

  const saveHosts = async () => {
    const values = await form.validateFields();
    setSaving(true);
    try {
      await saveAITrustedHosts((values.hosts || []).map(({ value }) => value.trim()));
      message.success(formatMessage('aiTrustedHostsSaveSuccess'));
      onCancel();
    } catch {
      // The shared request interceptor displays localized validation errors.
    } finally {
      setSaving(false);
    }
  };

  return (
    <ProModal
      open={open}
      title={formatMessage('aiTrustedHostsTitle')}
      width={640}
      fullScreenable={false}
      destroyOnHidden
      maskClosable={false}
      onCancel={onCancel}
    >
      <p className={styles.aiTrustedHostsDesc}>{formatMessage('aiTrustedHostsDesc')}</p>
      {loadFailed ? (
        <Alert
          className={styles.aiTrustedHostsLoadError}
          type="error"
          showIcon
          message={formatMessage('aiTrustedHostsLoadFailed')}
          action={
            <Button size="small" onClick={() => void loadHosts()}>
              {formatCommon('common.refresh')}
            </Button>
          }
        />
      ) : null}
      <Spin spinning={loading}>
        <Form form={form} layout="vertical" requiredMark={false} preserve={false} initialValues={{ hosts: [] }}>
          <Form.List name="hosts">
            {(fields, { add, remove }) => (
              <div className={styles.aiTrustedHostsList}>
                {fields.length === 0 && !loading && !loadFailed ? (
                  <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={formatMessage('aiTrustedHostsEmpty')} />
                ) : null}
                {fields.map(({ key, name, ...field }) => (
                  <div className={styles.aiTrustedHostRow} key={key}>
                    <Form.Item
                      {...field}
                      name={[name, 'value']}
                      label={formatMessage('aiTrustedHostLabel')}
                      rules={[
                        {
                          required: true,
                          whitespace: true,
                          message: formatMessage('aiTrustedHostRequired'),
                        },
                      ]}
                    >
                      <Input maxLength={253} placeholder={formatMessage('aiTrustedHostPlaceholder')} />
                    </Form.Item>
                    <Button
                      className={styles.aiTrustedHostDelete}
                      type="text"
                      danger
                      icon={<TrashCan size={18} />}
                      aria-label={formatCommon('common.delete')}
                      title={formatCommon('common.delete')}
                      onClick={() => remove(name)}
                    />
                  </div>
                ))}
                <Button
                  type="dashed"
                  icon={<Add size={18} />}
                  disabled={fields.length >= 100}
                  onClick={() => add({ value: '' })}
                >
                  {formatMessage('aiTrustedHostsAdd')}
                </Button>
              </div>
            )}
          </Form.List>
        </Form>
      </Spin>
      <div className={styles.aiTrustedHostsFooter}>
        <Button disabled={saving} onClick={onCancel}>
          {formatCommon('common.cancel')}
        </Button>
        <Button type="primary" loading={saving} disabled={loading || loadFailed} onClick={() => void saveHosts()}>
          {formatCommon('common.save')}
        </Button>
      </div>
    </ProModal>
  );
};

export default AITrustedHostsModal;
