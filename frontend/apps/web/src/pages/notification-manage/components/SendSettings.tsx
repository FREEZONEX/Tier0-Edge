import { useEffect, useRef, useState } from 'react';
import { Alert, DatePicker, Form, Radio, Space, type FormInstance } from 'antd';
import dayjs from 'dayjs';
import ComButton from '@/components/com-button';
import HelpTooltip from '@/components/help-tooltip';
import { estimateUsers } from '@/apis/core-api/notify';
import { useTranslate } from '@/hooks';
import RecipientSelector, { resolveRecipientUserIds } from './RecipientSelector';
import type { SendFormValues } from '../notification-form';
import styles from '../index.module.scss';

type Props = { form: FormInstance; disabled: boolean };

const SendSettings = ({ form, disabled }: Props) => {
  const t = useTranslate('notificationManage');
  const audienceType = Form.useWatch('audienceType', form);
  const recipients = Form.useWatch('recipientSubjects', form);
  const sendMode = Form.useWatch('sendMode', form);
  const selectionKey = JSON.stringify([audienceType, recipients]);
  const [estimate, setEstimate] = useState<{ key: string; count: number } | null>(null);
  const count = estimate?.key === selectionKey ? estimate.count : null;
  const requestVersion = useRef(0);

  useEffect(() => {
    requestVersion.current += 1;
    return () => {
      requestVersion.current += 1;
    };
  }, [audienceType, recipients]);

  const handleEstimate = async () => {
    const version = ++requestVersion.current;
    const values = await form.validateFields(['audienceType', 'recipientSubjects']);
    const userIds = values.audienceType === 2 ? await resolveRecipientUserIds(values.recipientSubjects) : undefined;
    if (userIds?.length === 0) {
      form.setFields([{ name: 'recipientSubjects', errors: [t('recipientEmpty')] }]);
      return;
    }
    const result = await estimateUsers({ audienceType: values.audienceType, userIds });
    if (version === requestVersion.current) setEstimate({ key: selectionKey, count: result.count });
  };

  return (
    <>
      <Form.Item name="audienceType" label={t('formAudience')} rules={[{ required: true }]}>
        <Radio.Group
          options={[
            { value: 1, label: t('audienceAll') },
            { value: 2, label: t('audienceUsers') },
          ]}
        />
      </Form.Item>
      {audienceType === 2 ? (
        <Form.Item
          name="recipientSubjects"
          label={t('formRecipients')}
          preserve={false}
          rules={[{ required: true, message: t('formRecipientsRequired') }]}
        >
          <RecipientSelector disabled={disabled} />
        </Form.Item>
      ) : (
        <Alert className={styles['audience-warning']} type="warning" showIcon message={t('allUsersWarning')} />
      )}
      <Space className={styles['recipient-estimate']}>
        <ComButton size="small" disabled={disabled} onClick={handleEstimate}>
          {t('estimate')}
        </ComButton>
        {count !== null && <span role="status">{t('estimateResult', { count })}</span>}
      </Space>
      <Form.Item
        name="sendMode"
        label={
          <Space>
            {t('sendTime')}
            <HelpTooltip title={t('scheduleHelp')} />
          </Space>
        }
      >
        <Radio.Group
          options={[
            { value: 'now', label: t('sendNow') },
            { value: 'scheduled', label: t('sendScheduled') },
          ]}
        />
      </Form.Item>
      {sendMode === 'scheduled' && (
        <Form.Item
          name="scheduledAt"
          label={t('formSchedule')}
          preserve={false}
          rules={[
            { required: true, message: t('scheduleRequired') },
            {
              validator: (_, value: SendFormValues['scheduledAt']) =>
                !value || value.valueOf() > Date.now()
                  ? Promise.resolve()
                  : Promise.reject(new Error(t('scheduleFuture'))),
            },
          ]}
        >
          <DatePicker
            className={styles['schedule-picker']}
            showTime={{ format: 'HH:mm' }}
            format="YYYY-MM-DD HH:mm"
            placeholder={t('scheduleRequired')}
            disabledDate={(date) => date.isBefore(dayjs(), 'day')}
          />
        </Form.Item>
      )}
    </>
  );
};

export default SendSettings;
