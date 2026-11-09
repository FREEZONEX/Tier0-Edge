import { useEffect, useState } from 'react';
import { useTranslate } from '@/hooks';
import { App, Button, Col, Form, Input, Row, Switch } from 'antd';
import { createUser, getRoleList, updateUser } from '@/apis/core-api/user-manage';
import styles from './RoleSetting.module.scss';
import ComSelect from '@/components/com-select';
import ProModal from '@/components/pro-modal';
import { validNameRegex, passwordRegex, passwordStrengthRegex } from '@/utils/pattern';
import { useBaseStore } from '@/stores/base';
import HomePageSelect from '@/components/home-page-select';
import HelpTooltip from '@/components/help-tooltip';

const useAddUser = ({ onSaveBack }: any) => {
  const { message } = App.useApp();
  const [open, setOpen] = useState(false);
  const [isEdit, setEdit] = useState(false);
  const [options, setOptions] = useState([]);
  const [form] = Form.useForm();
  const formatMessage = useTranslate();
  const [loading, setLoading] = useState(false);
  const [isLocalEditableUser, setLocalEditableUser] = useState(false);
  const ldapEnable = useBaseStore((state) => state?.systemInfo?.ldapEnable);
  const menuGroupNoSub = useBaseStore((state) => state.menuGroup?.filter((item) => !item.subMenu));
  const editingUserId = Form.useWatch('userId', form);

  useEffect(() => {
    if (open) {
      getRoleList().then((data: any) => {
        setOptions(
          data?.map((d: any) => ({
            label: d?.roleName,
            value: d?.roleId,
          }))
        );
      });
    }
  }, [open]);

  const onAddOpen = (data?: any) => {
    if (data) {
      setEdit(true);
      setLocalEditableUser(data?.source !== 'external' || data?.preferredUsername === 'tier0');
      form.setFieldsValue({
        ...data,
        username: data.preferredUsername,
        userId: data.id,
        roleList:
          data?.roleList?.length > 0
            ? {
                label: data?.roleList?.[0]?.roleName,
                value: data?.roleList?.[0]?.roleId,
              }
            : undefined,
      });
    } else {
      setEdit(false);
      setLocalEditableUser(false);
      form.setFieldsValue({ forceChangePassword: false });
    }
    setOpen(true);
  };

  const onClose = () => {
    setOpen(false);
    form.resetFields();
  };
  const onSave = async () => {
    const info = await form.validateFields();
    setLoading(true);
    const api = isEdit ? updateUser : createUser;
    try {
      await api({
        ...info,
        roleList: info?.roleList ? [{ roleId: info?.roleList?.value, roleName: info?.roleList?.label }] : [],
        enabled: true,
        operateRole: isEdit ? true : undefined,
      });
      message.success(formatMessage('common.optsuccess'));
      onClose();
      onSaveBack?.();
    } catch {
      // 请求封装会统一提示接口错误，这里只负责收束保存流程。
    } finally {
      setLoading(false);
    }
  };
  const Dom = (
    <ProModal
      size="xs"
      open={open}
      maskClosable={false}
      onCancel={onClose}
      className={styles['use-add-modal']}
      title={formatMessage(isEdit ? 'account.editUsers' : 'account.newUsers')}
    >
      <Form layout="vertical" form={form} initialValues={{ forceChangePassword: false }}>
        <Form.Item name="userId" hidden>
          <Input />
        </Form.Item>
        <Row gutter={16}>
          <Col span={12}>
            <Form.Item
              label={formatMessage('account.account')}
              name="username"
              rules={
                ldapEnable && !isLocalEditableUser
                  ? [
                      {
                        required: true,
                        message: formatMessage('rule.required'),
                      },
                    ]
                  : [
                      {
                        required: true,
                        message: formatMessage('rule.required'),
                      },
                      {
                        type: 'string',
                        min: 1,
                        max: 60,
                        message: formatMessage('uns.labelMaxLength', {
                          label: formatMessage('account.account'),
                          length: 60,
                        }),
                      },
                      {
                        pattern: validNameRegex,
                        message: formatMessage('rule.invalidChars'),
                      },
                    ]
              }
            >
              <Input
                className="username"
                disabled={isEdit || (ldapEnable && !isLocalEditableUser)}
                placeholder={formatMessage('account.account')}
              />
            </Form.Item>
          </Col>
          {!isEdit && (
            <Col span={12}>
              <Form.Item
                label={formatMessage('appGui.password')}
                name="password"
                rules={[
                  {
                    required: true,
                    message: formatMessage('rule.required'),
                  },
                  {
                    pattern: passwordRegex,
                    message: formatMessage('rule.password'),
                  },
                  { pattern: passwordStrengthRegex, message: formatMessage('rule.passwordStrength') },
                ]}
              >
                <Input.Password placeholder={formatMessage('appGui.password')} autoComplete="new-password" />
              </Form.Item>
            </Col>
          )}
          <Col span={12}>
            <Form.Item
              label={formatMessage('account.displayName')}
              name="firstName"
              rules={
                ldapEnable && !isLocalEditableUser
                  ? []
                  : [
                      {
                        type: 'string',
                        min: 1,
                        max: 60,
                        message: formatMessage('uns.labelMaxLength', {
                          label: formatMessage('account.displayName'),
                          length: 60,
                        }),
                      },
                      {
                        pattern: validNameRegex,
                        message: formatMessage('rule.invalidChars'),
                      },
                    ]
              }
            >
              <Input disabled={ldapEnable && !isLocalEditableUser} placeholder={formatMessage('account.displayName')} />
            </Form.Item>
          </Col>

          <Col span={12}>
            <Form.Item label={formatMessage('account.phone')} name="phone">
              <Input disabled={ldapEnable && !isLocalEditableUser} placeholder={formatMessage('account.phone')} />
            </Form.Item>
          </Col>
          <Col span={12}>
            <Form.Item
              label={formatMessage('account.email')}
              name="email"
              rules={ldapEnable && !isLocalEditableUser ? [] : [{ type: 'email' }]}
            >
              <Input disabled={ldapEnable && !isLocalEditableUser} placeholder={formatMessage('account.email')} />
            </Form.Item>
          </Col>
          <Col span={12}>
            <Form.Item
              label={formatMessage('account.role')}
              name="roleList"
              rules={[
                {
                  required: true,
                  message: formatMessage('rule.required'),
                },
              ]}
            >
              <ComSelect
                placeholder={formatMessage('account.role')}
                options={options}
                // mode="multiple"
                allowClear
                onClick={(e) => {
                  e.preventDefault();
                }}
                labelInValue
              />
            </Form.Item>
          </Col>
          {!isEdit && (
            <Col span={24}>
              <Form.Item
                label={
                  <span className="flex items-center gap-1">
                    {formatMessage('account.requirePasswordReset')}
                    <HelpTooltip title={formatMessage('account.passwordResetHint')} />
                  </span>
                }
                name="forceChangePassword"
                valuePropName="checked"
              >
                <Switch checkedChildren={formatMessage('common.yes')} unCheckedChildren={formatMessage('common.no')} />
              </Form.Item>
            </Col>
          )}
          {isEdit && (
            <Col span={12}>
              <Form.Item label={formatMessage('account.homePage')} name="homePage">
                <HomePageSelect
                  enabled={open && Boolean(editingUserId)}
                  resources={menuGroupNoSub}
                  targetUserId={editingUserId}
                  placeholder={formatMessage('common.searchPage')}
                  allowClear
                  showSearch
                />
              </Form.Item>
            </Col>
          )}
        </Row>

        <Button
          onClick={onSave}
          style={{ height: 32 }}
          block
          type="primary"
          loading={loading}
          title={formatMessage('common.save')}
        >
          {formatMessage('common.save')}
        </Button>
      </Form>
    </ProModal>
  );
  return {
    ModalAddDom: Dom,
    onAddOpen,
  };
};

export default useAddUser;
