import { type FC, type ReactNode, useEffect, useState } from 'react';
import {
  Popover,
  Divider,
  Flex,
  type PopoverProps,
  type SelectProps,
  Button,
  Form,
  Input,
  App,
  ConfigProvider,
  Tabs,
} from 'antd';
import { useTranslate } from '@/hooks';
import { productName } from '@/utils/product-edition';
import ComSelect from '../com-select';
import ProModal from '../pro-modal';
import { setHomePageApi, updateCurrentUserProfile, userResetPwd } from '@/apis/core-api/user-manage';
import { LOGIN_URL, OMC_MODEL } from '@/common-types/constans';
import { APP_USER_GUIDE_ROUTES, APP_USER_TIPS_ENABLE } from '@/common-types/constans';
import { removeToken } from '@/utils/auth';
import { storageOpt } from '@/utils/storage';
import { passwordRegex, validNameRegex } from '@/utils/pattern';
import { fetchBaseStore, fetchSystemInfo, updateForUserInfo, useBaseStore } from '@/stores/base';
import { setTheme, ThemeType, useThemeStore } from '@/stores/theme-store.ts';
import Cookies from 'js-cookie';
import { updatePersonConfigApi } from '@/apis/core-api/uns';
import { logoutApi } from '@/apis/core-api/auth';
import { initI18n, useI18nStore } from '@/stores/i18n-store.ts';
import { preloadPluginLang } from '@/utils/plugin.ts';
import { Contrast, Globe, LogOut, Settings2 } from 'lucide-react';
import { useNavigate } from 'react-router';
import HomePageSelect from '@/components/home-page-select';
import { resolveProductVersion } from '@/utils/product-version';
import { beginLaunchpadExplicitLogout, withLaunchpadStandaloneQuery } from '@/utils/launchpad-site';

const logout = (path?: string) => {
  beginLaunchpadExplicitLogout();
  logoutApi().then(() => {
    removeToken();
    Cookies.remove(OMC_MODEL, { path: '/' });
    // 退出时删除guide routes信息
    storageOpt.remove(APP_USER_GUIDE_ROUTES);
    // 退出时重置tips信息
    storageOpt.remove(APP_USER_TIPS_ENABLE);
    // 清空
    storageOpt.remove('personInfo');
    window.location.replace(path || LOGIN_URL);
  });
};

const ComList: FC<{
  list: {
    icon?: ReactNode;
    label?: ReactNode;
    children?: ReactNode;
    key: string;
    onClick?: () => void;
    disabled?: boolean;
  }[];
}> = ({ list }) => {
  return (
    <>
      {list?.map((item) => {
        return (
          <Flex
            key={item.key}
            justify="space-between"
            align="center"
            style={{
              width: '100%',
              padding: '6px 8px',
              cursor: item?.disabled ? 'not-allowed' : 'pointer',
              opacity: item?.disabled ? 0.5 : undefined,
            }}
            onClick={!item?.disabled ? item?.onClick : undefined}
          >
            <Flex justify="flex-start" align="center" gap={8} style={{ flex: 1 }}>
              {item.icon}
              {item.label}
            </Flex>
            {item.children && (
              <div onClick={(event) => event.stopPropagation()} onMouseDown={(event) => event.stopPropagation()}>
                {item.children}
              </div>
            )}
          </Flex>
        );
      })}
    </>
  );
};
const popoverSelectProps: Pick<SelectProps, 'getPopupContainer' | 'styles'> = {
  getPopupContainer: () => document.body,
  styles: { popup: { root: { zIndex: 10050 } } },
};

const UserPopover: FC<PopoverProps> = ({ children, ...restProps }) => {
  const formatMessage = useTranslate();
  const navigate = useNavigate();
  const { currentUserInfo, systemInfo, pluginList, menuGroupNoSub } = useBaseStore((state) => ({
    currentUserInfo: state.currentUserInfo,
    systemInfo: state.systemInfo,
    pluginList: state.pluginList,
    menuGroupNoSub: state.menuGroup?.filter((f) => !f.subMenu),
  }));
  const { _theme } = useThemeStore((state) => ({
    _theme: state._theme,
  }));
  const { lang, langList } = useI18nStore((state) => ({
    lang: state.lang,
    langList: state.langList
      ?.filter((f) => f.hasUsed)
      ?.map((m: any) => ({ ...m, value: m?.value?.replace?.('_', '-') })),
  }));
  const [popoverOpen, setPopoverOpen] = useState(false);
  const [open, setOpen] = useState(false);
  const [loading, setLoading] = useState(false);
  const [form1] = Form.useForm();
  const [form2] = Form.useForm();
  const [form3] = Form.useForm();
  const { message } = App.useApp();

  const toggleTheme = (v: string) => {
    setTheme(v as ThemeType);
  };
  const name = currentUserInfo.firstName || currentUserInfo.preferredUsername;
  const version = formatMessage('common.productVersionLabel', {
    product: productName(systemInfo),
    version: resolveProductVersion(systemInfo),
  });
  const userContent = (
    <div className="userPopoverWrap">
      <div className="userAvatar">{name?.slice(0, 1)?.toLocaleUpperCase()}</div>
      <div className="userName">{name}</div>
      {currentUserInfo.roleString?.trim() ? (
        <Flex
          title={currentUserInfo.roleString}
          className="userRole"
          justify="center"
          align="center"
          style={{ width: '100%' }}
        >
          <div
            style={{
              overflow: 'hidden',
              whiteSpace: 'nowrap',
              textOverflow: 'ellipsis',
            }}
          >
            {currentUserInfo.roleString}
          </div>
        </Flex>
      ) : null}
      {currentUserInfo.email && (
        <div className="userEmail" title={currentUserInfo.email}>
          {currentUserInfo.email}
        </div>
      )}
      <Divider
        style={{
          background: '#c6c6c6',
          margin: '15px auto',
        }}
      />
      <ComList
        list={[
          {
            icon: <Contrast color="var(--ui-text-color)" size={18} strokeWidth={1.75} aria-hidden />,
            label: <div style={{ color: 'var(--ui-text-color)' }}>{formatMessage('common.theme')}</div>,
            key: 'theme',
            children: (
              <ComSelect
                value={_theme}
                style={{ height: 28, width: 94, backgroundColor: 'var(--ui-bg-color) !important' }}
                onChange={toggleTheme}
                {...popoverSelectProps}
                options={[
                  {
                    label: formatMessage('common.light'),
                    value: ThemeType.Light,
                  },
                  {
                    label: formatMessage('common.dark'),
                    value: ThemeType.Dark,
                  },
                  {
                    label: formatMessage('common.followSystem'),
                    value: ThemeType.System,
                  },
                ]}
              />
            ),
          },
          {
            icon: <Globe color="var(--ui-text-color)" size={18} strokeWidth={1.75} aria-hidden />,
            label: <div style={{ color: 'var(--ui-text-color)' }}>{formatMessage('common.language')}</div>,
            key: 'language',
            children: (
              <ComSelect
                disabled={!currentUserInfo?.sub}
                onChange={(v) => {
                  if (currentUserInfo?.sub) {
                    // 重新过滤插件国际化文件;
                    updatePersonConfigApi({ userId: currentUserInfo.sub!, mainLanguage: v }).then(async () => {
                      const pluginLang = await preloadPluginLang(
                        pluginList
                          ?.filter((f: any) => f.installStatus === 'installed')
                          ?.filter((f: any) => f?.plugInfoYml?.route?.name)
                          ?.map((m: any) => ({ name: `/${m?.plugInfoYml?.route?.name}`, backendName: m?.name })) || [],
                        v
                      );
                      // 先切换前端国际化，再刷新依赖后端国际化的菜单/路由名称
                      await initI18n(v, pluginLang);
                      return fetchSystemInfo(true);
                    });
                  }
                }}
                value={lang}
                style={{ height: 28, width: 94, backgroundColor: 'var(--ui-bg-color) !important' }}
                {...popoverSelectProps}
                options={langList}
              />
            ),
          },
        ]}
      />
      <Divider
        style={{
          background: '#c6c6c6',
          margin: '15px auto',
        }}
      />
      <ComList
        list={[
          // {
          //   icon: (
          //     <Badge count={100} size={'small'} styles={{ indicator: { fontSize: 10, padding: '0 2px' } }}>
          //       <Alarm color="var(--ui-text-color)" size={18} />
          //     </Badge>
          //   ),
          //   label: <div style={{ color: 'var(--ui-text-color)' }}>{formatMessage('common.information')}</div>,
          //   key: 'information',
          //   onClick: () => {
          //     setInformationOpen(true);
          //   },
          // },
          {
            icon: <Settings2 color="var(--ui-text-color)" size={18} strokeWidth={1.75} aria-hidden />,
            label: <div style={{ color: 'var(--ui-text-color)' }}>{formatMessage('common.settings')}</div>,
            key: 'setting',
            onClick: () => {
              setPopoverOpen(false);
              navigate(withLaunchpadStandaloneQuery('/settings/profile'));
            },
          },
          {
            icon: <LogOut color="var(--ui-text-color)" size={18} strokeWidth={1.75} aria-hidden />,
            label: <div style={{ color: 'var(--ui-text-color)' }}>{formatMessage('common.logout')}</div>,
            key: 'layout',
            onClick: () => {
              setPopoverOpen(false);
              logout(systemInfo.loginPath);
            },
          },
        ]}
      />
      <span style={{ marginTop: 10 }} className="userEmail" title={version}>
        {version}
      </span>
    </div>
  );
  const rest = () => {
    form1.resetFields();
    form2.resetFields();
  };
  const onSave1 = async () => {
    const info = await form1.validateFields();
    setLoading(true);
    updateCurrentUserProfile(info)
      .then(() => {
        message.success(formatMessage('common.settingSuccess'));
        // 修改用户名，手动去更新
        updateForUserInfo({
          ...info,
        });
        setOpen(false);
        form1.resetFields();
      })
      .finally(() => {
        setLoading(false);
      });
  };
  const onSave2 = async () => {
    const info = await form2.validateFields();
    setLoading(true);
    userResetPwd({
      newPassword: info.password,
      password: info.oldPassword,
      userId: currentUserInfo?.sub,
      username: currentUserInfo?.preferredUsername,
    })
      .then(() => {
        message.success(formatMessage('common.settingSuccess'));
        logout(systemInfo.loginPath);
      })
      .finally(() => {
        setLoading(false);
      });
  };
  const onSave3 = async () => {
    const info = await form3.validateFields();
    if (info) {
      setHomePageApi({ homePage: info.homePage })?.then((config: any) => {
        const homePage = config?.homePage || info.homePage;
        updateForUserInfo({ homePage });
        message.success(formatMessage('common.settingSuccess'));
      });
    }
  };

  useEffect(() => {
    if (open) {
      // 更新路由
      fetchBaseStore?.().then(() => {
        form3.setFieldValue('homePage', currentUserInfo?.homePage);
      });
    }
  }, [open]);

  const items: any[] = [
    {
      label: formatMessage('account.profile'),
      key: 1,
      children: (
        <Flex style={{ height: 300 }} vertical>
          <Form layout="vertical" form={form1} style={{ flex: 1 }}>
            <Form.Item
              label={formatMessage('account.updateDisplayName')}
              name="firstName"
              rules={[
                {
                  required: true,
                  message: formatMessage('rule.required'),
                },
                {
                  type: 'string',
                  min: 1,
                  max: 200,
                  message: formatMessage('rule.characterLimit'),
                },
                {
                  pattern: validNameRegex,
                  message: formatMessage('rule.invalidChars'),
                },
              ]}
            >
              <Input className={'input'} placeholder={formatMessage('account.displayName')} />
            </Form.Item>
            <Form.Item label={formatMessage('common.updateEmail')} name="email" rules={[{ type: 'email' }]}>
              <Input placeholder={formatMessage('account.email')} />
            </Form.Item>
            <Form.Item label={formatMessage('common.updatePhone')} name="phone">
              <Input placeholder={formatMessage('account.phone')} />
            </Form.Item>
          </Form>
          <Button onClick={onSave1} style={{ height: 32 }} block type="primary" loading={loading}>
            {formatMessage('common.save')}
          </Button>
        </Flex>
      ),
    },
    {
      label: formatMessage('common.password'),
      key: 2,
      children: (
        <Flex style={{ height: 300 }} vertical>
          <Form layout="vertical" form={form2} style={{ flex: 1 }}>
            <Form.Item
              label={formatMessage('account.oldPassWord')}
              name="oldPassword"
              rules={[
                {
                  required: true,
                  message: '',
                },
                {
                  max: 10,
                  message: formatMessage('uns.labelMaxLength', {
                    label: formatMessage('appGui.password'),
                    length: 10,
                  }),
                },
                { pattern: passwordRegex, message: formatMessage('rule.password') },
              ]}
            >
              <Input.Password placeholder={formatMessage('appGui.password')} />
            </Form.Item>
            <Form.Item
              label={formatMessage('account.newpassWord')}
              name="password"
              dependencies={['oldPassword']}
              rules={[
                {
                  required: true,
                  message: '',
                },
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
                    if (!value || getFieldValue('oldPassword') !== value) {
                      return Promise.resolve();
                    }
                    return Promise.reject(new Error(formatMessage('account.passwordSame')));
                  },
                }),
              ]}
            >
              <Input.Password placeholder={formatMessage('appGui.password')} />
            </Form.Item>
            <Form.Item
              label={formatMessage('account.confirmpassWord')}
              name="confirm_password"
              dependencies={['password']}
              rules={[
                {
                  required: true,
                  message: '',
                },
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
                    if (!value || getFieldValue('password') === value) {
                      return Promise.resolve();
                    }
                    return Promise.reject(new Error(formatMessage('account.passwordMatch')));
                  },
                }),
              ]}
            >
              <Input.Password placeholder={formatMessage('appGui.password')} />
            </Form.Item>
          </Form>
          <Button onClick={onSave2} style={{ height: 32 }} block type="primary" loading={loading}>
            {formatMessage('common.save')}
          </Button>
        </Flex>
      ),
    },
    {
      label: formatMessage('account.homePage'),
      key: 3,
      children: (
        <Form
          layout="vertical"
          form={form3}
          style={{ height: 300, display: 'flex', flexDirection: 'column', justifyContent: 'space-between' }}
        >
          <Form.Item label={formatMessage('account.homePage')} name="homePage">
            <HomePageSelect
              enabled={open}
              resources={menuGroupNoSub}
              placeholder={formatMessage('common.searchPage')}
              allowClear
              showSearch
            />
          </Form.Item>
          <Form.Item shouldUpdate={(pre, cur) => pre.homePage !== cur.homePage} noStyle>
            {({ getFieldValue }) => {
              return (
                <Button
                  disabled={!getFieldValue('homePage')}
                  onClick={onSave3}
                  style={{ height: 32 }}
                  block
                  type="primary"
                  loading={loading}
                >
                  {formatMessage('common.save')}
                </Button>
              );
            }}
          </Form.Item>
        </Form>
      ),
    },
  ];
  return (
    <>
      <Popover
        rootClassName="userPopover"
        placement="bottomRight"
        trigger="click"
        {...restProps}
        open={popoverOpen}
        onOpenChange={setPopoverOpen}
        content={userContent}
      >
        {children}
      </Popover>
      <ProModal
        size="sm"
        forceRender
        onCancel={() => {
          setOpen(false);
          rest();
        }}
        title={formatMessage('account.settings')}
        open={open}
        // open
        maskClosable={false}
      >
        <ConfigProvider
          theme={{
            components: {
              Form: {
                itemMarginBottom: 12,
              },
            },
          }}
        >
          <Tabs tabPosition="left" items={items.map((item) => ({ ...item, forceRender: true }))} />

          {/*<Divider*/}
          {/*  style={{*/}
          {/*    background: '#c6c6c6',*/}
          {/*    margin: '16px auto',*/}
          {/*  }}*/}
          {/*/>*/}

          {/*<Divider*/}
          {/*  style={{*/}
          {/*    background: '#c6c6c6',*/}
          {/*    margin: '16px auto',*/}
          {/*  }}*/}
          {/*/>*/}
        </ConfigProvider>
      </ProModal>
    </>
  );
};

export default UserPopover;
