import { type ReactNode, useMemo, useState } from 'react';
import { App, Form, Input, Modal, Typography, theme } from 'antd';
import { useTranslate } from '@/hooks';
import { userResetPwd } from '@/apis/core-api/user-manage';
import { updateForUserInfo, useBaseStore } from '@/stores/base';
import { useLocation } from 'react-router';
import { isMiniAppRoutePath } from '@/utils/launchpad-site';
import { getToken } from '@/utils/auth';
import { passwordRegex } from '@/utils/pattern';
import { LOGIN_URL } from '@/common-types/constans';
import { ForegroundDialogContext } from '@/contexts/foreground-dialog-context';

const tokenFingerprint = (token?: string) => {
  let hash = 5381;
  const value = token || '';
  for (let index = 0; index < value.length; index += 1) {
    hash = (hash * 33) ^ value.charCodeAt(index);
  }
  return (hash >>> 0).toString(36);
};

const suppressKey = (userId?: string, token?: string) =>
  userId && token ? `tier0.defaultPassword.suppress:${userId}:${tokenFingerprint(token)}` : '';

const isSuppressed = (key: string) => {
  if (!key) return false;
  try {
    return window.sessionStorage.getItem(key) === '1';
  } catch {
    return false;
  }
};

const suppressForCurrentSession = (key: string) => {
  if (!key) return;
  try {
    window.sessionStorage.setItem(key, '1');
  } catch {
    // Ignore storage failures in strict privacy modes.
  }
};

const DefaultPasswordReminder = ({ children }: { children?: ReactNode }) => {
  const { pathname, search } = useLocation();
  const isMiniApp = isMiniAppRoutePath(pathname);
  const isMcpAuthorization =
    pathname === '/mcp-authorize' ||
    (pathname === LOGIN_URL && new URLSearchParams(search).get('redirectUri')?.split(/[?#]/)[0] === '/mcp-authorize');
  const formatMessage = useTranslate();
  const { message } = App.useApp();
  const [form] = Form.useForm();
  const currentUserInfo = useBaseStore((state) => state.currentUserInfo);
  const [dismissedKey, setDismissedKey] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const { token: themeToken } = theme.useToken();
  const token = getToken();
  const storageKey = useMemo(() => suppressKey(currentUserInfo?.sub, token), [currentUserInfo?.sub, token]);

  // Derive this before child dialogs mount, rather than opening in a later effect.
  const open = Boolean(
    !isMiniApp &&
    !isMcpAuthorization &&
    currentUserInfo?.forceChangePassword &&
    storageKey &&
    dismissedKey !== storageKey &&
    !isSuppressed(storageKey)
  );

  const handleLater = () => {
    suppressForCurrentSession(storageKey);
    setDismissedKey(storageKey);
    form.resetFields();
  };

  const handleSubmit = async () => {
    const values = await form.validateFields();
    setSubmitting(true);
    try {
      await userResetPwd({
        oldPassword: values.oldPassword,
        newPassword: values.password,
      });
      message.success(formatMessage('common.settingSuccess'));
      updateForUserInfo({ forceChangePassword: false });
      setDismissedKey(storageKey);
      form.resetFields();
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <ForegroundDialogContext.Provider value={open}>
      {children}
      <Modal
        open={open}
        zIndex={themeToken.zIndexPopupBase + 200}
        title={formatMessage('account.passwordResetTitle')}
        onCancel={handleLater}
        onOk={handleSubmit}
        okText={formatMessage('account.changePasswordNow')}
        cancelText={formatMessage('common.later')}
        confirmLoading={submitting}
        destroyOnHidden
      >
        <Typography.Paragraph>{formatMessage('account.passwordResetDesc')}</Typography.Paragraph>
        <Form name="passwordResetReminder" form={form} layout="vertical">
          <Form.Item
            label={formatMessage('account.oldPassWord')}
            name="oldPassword"
            rules={[{ required: true, message: formatMessage('rule.required') }]}
          >
            <Input.Password placeholder={formatMessage('account.oldPassWord')} autoComplete="current-password" />
          </Form.Item>
          <Form.Item
            label={formatMessage('account.newpassWord')}
            name="password"
            dependencies={['oldPassword']}
            rules={[
              { required: true, message: formatMessage('rule.required') },
              {
                max: 10,
                message: formatMessage('uns.labelMaxLength', {
                  label: formatMessage('appGui.password'),
                  length: 10,
                }),
              },
              { pattern: passwordRegex, message: formatMessage('rule.password') },
              ({ getFieldValue }) => ({
                validator(_, value) {
                  if (!value || value !== getFieldValue('oldPassword')) {
                    return Promise.resolve();
                  }
                  return Promise.reject(new Error(formatMessage('account.passwordSame')));
                },
              }),
            ]}
          >
            <Input.Password placeholder={formatMessage('appGui.password')} autoComplete="new-password" />
          </Form.Item>
          <Form.Item
            label={formatMessage('account.confirmpassWord')}
            name="confirm_password"
            dependencies={['password']}
            rules={[
              { required: true, message: formatMessage('rule.required') },
              ({ getFieldValue }) => ({
                validator(_, value) {
                  if (!value || getFieldValue('password') === value) {
                    return Promise.resolve();
                  }
                  return Promise.reject(new Error(formatMessage('account.passwordMatch')));
                },
              }),
            ]}
          >
            <Input.Password placeholder={formatMessage('appGui.password')} autoComplete="new-password" />
          </Form.Item>
        </Form>
      </Modal>
    </ForegroundDialogContext.Provider>
  );
};

export default DefaultPasswordReminder;
