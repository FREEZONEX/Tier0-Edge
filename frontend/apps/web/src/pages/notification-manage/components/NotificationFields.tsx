import { Form, Input, Space, Switch, Tag } from 'antd';
import HelpTooltip from '@/components/help-tooltip';
import { useTranslate } from '@/hooks';
import { MAX_LENGTHS } from '@/utils/limits';
import { characterCount, isValidNotificationLink } from '../notification-form';
import styles from '../index.module.scss';

const NotificationFields = () => {
  const t = useTranslate('notificationManage');
  const titleRules = [
    { required: true, whitespace: true, message: t('titleRequired') },
    {
      validator: (_: unknown, value: string = '') =>
        characterCount(value.trim()) <= MAX_LENGTHS.notificationTitle
          ? Promise.resolve()
          : Promise.reject(new Error(t('titleTooLong', { count: MAX_LENGTHS.notificationTitle }))),
    },
  ];
  const contentRules = [
    { required: true, whitespace: true, message: t('contentRequired') },
    {
      validator: (_: unknown, value: string = '') =>
        characterCount(value) <= MAX_LENGTHS.notificationContent
          ? Promise.resolve()
          : Promise.reject(new Error(t('contentTooLong', { count: MAX_LENGTHS.notificationContent }))),
    },
  ];
  const linkRules = [
    {
      validator: (_: unknown, value: string = '') =>
        isValidNotificationLink(value) ? Promise.resolve() : Promise.reject(new Error(t('linkInvalid'))),
    },
  ];

  return (
    <>
      <div className={styles['content-heading']}>
        <h3>{t('contentSettings')}</h3>
        <Tag>{t('typeInbox')}</Tag>
      </div>
      <Form.Item name="type" hidden>
        <Input />
      </Form.Item>
      <Form.Item name="title" label={t('formTitle')} rules={titleRules}>
        <Input
          placeholder={t('titleRequired')}
          count={{ show: true, max: MAX_LENGTHS.notificationTitle, strategy: characterCount }}
        />
      </Form.Item>
      <Form.Item name="content" label={t('formContent')} rules={contentRules}>
        <Input.TextArea
          autoSize={{ minRows: 4, maxRows: 8 }}
          placeholder={t('contentRequired')}
          count={{ show: true, max: MAX_LENGTHS.notificationContent, strategy: characterCount }}
        />
      </Form.Item>
      <Form.Item
        name="link"
        label={
          <Space>
            {t('formLink')}
            <HelpTooltip title={t('linkHelp')} />
          </Space>
        }
        rules={linkRules}
      >
        <Input placeholder={t('formLinkPlaceholder')} />
      </Form.Item>
      <Form.Item
        name="asModal"
        label={
          <Space>
            {t('formAsModal')}
            <HelpTooltip title={t('modalHelp')} />
          </Space>
        }
        valuePropName="checked"
      >
        <Switch />
      </Form.Item>
    </>
  );
};

export default NotificationFields;
