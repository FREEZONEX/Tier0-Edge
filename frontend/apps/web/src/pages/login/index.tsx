import { useCallback, useEffect, useRef, useState } from 'react';
import { Alert, Button, Divider, Form, Input, Select, message } from 'antd';
import { useTranslate } from '@/hooks';
import {
  checkExternalSSOUsername,
  completeExternalSSO,
  getExternalSSOPending,
  getUserInfo,
  linkExternalSSO,
  loginApi,
  externalSSOStartURL,
  type ExternalSSOPending,
} from '@/apis/core-api/auth';
import { getSystemConfig } from '@/apis/core-api/system-config';
import { useBaseStore } from '@/stores/base';
import { productLogo, productName } from '@/utils/product-edition';
import type { SystemInfoProps } from '@/stores/types';
import { updatePersonConfigApi } from '@/apis/core-api/uns';
import { getToken, removeToken } from '@/utils/auth';
import { LOGIN_URL } from '@/common-types/constans.ts';
import {
  getUserDefaultHomePath,
  isLaunchpadStandaloneAllowedPath,
  isLaunchpadStandaloneSite,
  withLaunchpadStandaloneQuery,
} from '@/utils/launchpad-site';
import { I18nEnum, initI18n, useI18nStore } from '@/stores/i18n-store.ts';
import localEnUS from '@/locale/en-US.json';
import localZhCN from '@/locale/zh-CN.json';
import './index.scss';

const loginBackground = '/login/login-background-reference.svg';

type LoginFormValues = {
  username: string;
  password: string;
};

type RegistrationFormValues = {
  username: string;
};

type UsernameAvailability = 'idle' | 'checking' | 'available' | 'taken' | 'invalid' | 'error';

const defaultPostLoginPath = '/home';
const launchpadPostLoginPath = '/launchpad';
const getDefaultPostLoginPath = () =>
  isLaunchpadStandaloneSite() ? withLaunchpadStandaloneQuery(launchpadPostLoginPath) : defaultPostLoginPath;
const loginFallbackMessages: Record<string, Record<string, string>> = {
  [I18nEnum.EnUS]: localEnUS as Record<string, string>,
  [I18nEnum.ZhCN]: localZhCN as Record<string, string>,
};
const usernamePattern = /^[\p{L}\p{N}._@-]{3,60}$/u;
const microsoftAPIErrorMessages: Record<string, string> = {
  'auth.microsoftIdentityConflict': 'login.externalError.identity_conflict',
  'auth.microsoftIdentityInvalid': 'login.externalError.identity_rejected',
  'auth.microsoftLinkAttemptsExceeded': 'login.externalPendingInvalid',
  'auth.microsoftLinkUnauthorized': 'login.invalidCredentials',
  'auth.microsoftPendingInvalid': 'login.externalPendingInvalid',
  'auth.microsoftRegistrationBusy': 'login.externalUsernameCheckFailed',
  'auth.microsoftSsoDisabled': 'login.externalError.disabled',
  'auth.microsoftSsoUnavailable': 'login.externalError.unavailable',
  'auth.microsoftStateInvalid': 'login.externalError.invalid_state',
  'auth.microsoftUsernameInvalid': 'login.externalUsernameInvalid',
  'auth.microsoftUsernameTaken': 'login.externalUsernameTaken',
};

const normalizeRedirectUri = (redirectUri?: string | null) => {
  const next = redirectUri?.trim();
  if (!next || !next.startsWith('/') || next.startsWith('//') || next.includes('\\') || /%5c/i.test(next)) {
    return '';
  }
  const targetUrl = new URL(next, window.location.origin);
  const pathname = targetUrl.pathname;
  const isStandaloneContext = isLaunchpadStandaloneSite() || isLaunchpadStandaloneSite(pathname, targetUrl.search);
  if (
    next === '/' ||
    next === '/?isLogin=true' ||
    next === '/login' ||
    next === LOGIN_URL ||
    (isStandaloneContext && pathname !== '/' && !isLaunchpadStandaloneAllowedPath(pathname))
  ) {
    return '';
  }
  return isStandaloneContext ? withLaunchpadStandaloneQuery(next) : next;
};

const LoginPage = () => {
  const systemInfo = useBaseStore((state) => state.systemInfo);
  const searchParams = new URLSearchParams(window.location.search);
  const ssoProvider = searchParams.get('ssoProvider')?.trim() || '';
  const ssoTicket = searchParams.get('ssoTicket')?.trim() || '';
  const ssoError = searchParams.get('ssoError')?.trim() || '';
  const redirectUri = normalizeRedirectUri(searchParams.get('redirectUri'));
  const [loading, setLoading] = useState(false);
  const [ssoLoading, setSsoLoading] = useState(false);
  const [loginReady, setLoginReady] = useState(false);
  const [externalLoginProviders, setExternalLoginProviders] = useState<{ id: string; name: string }[]>([]);
  const [pending, setPending] = useState<ExternalSSOPending | null>(null);
  const [pendingLoading, setPendingLoading] = useState(Boolean(ssoTicket));
  const [pendingError, setPendingError] = useState('');
  const [ssoMode, setSsoMode] = useState<'register' | 'link'>('register');
  const [usernameAvailability, setUsernameAvailability] = useState<UsernameAvailability>('idle');
  const availabilityRequest = useRef(0);
  const availabilityTimer = useRef<number | null>(null);
  const [registrationForm] = Form.useForm<RegistrationFormValues>();
  const formatMessage = useTranslate();
  const lang = useI18nStore((state) => state.lang);
  const [selectedLanguage, setSelectedLanguage] = useState(lang);
  const loginMessage = useCallback(
    (id: string, values?: Record<string, string>) => {
      const translated = formatMessage(id, values);
      if (translated && translated !== id) {
        return translated;
      }
      const fallbackLang = selectedLanguage === I18nEnum.ZhCN ? I18nEnum.ZhCN : I18nEnum.EnUS;
      let fallback = loginFallbackMessages[fallbackLang]?.[id] || loginFallbackMessages[I18nEnum.EnUS]?.[id] || id;
      for (const [key, value] of Object.entries(values || {})) fallback = fallback.replaceAll(`{${key}}`, value);
      return fallback;
    },
    [formatMessage, selectedLanguage]
  );

  const languageOptions = [
    { label: loginMessage('login.zhCN'), value: I18nEnum.ZhCN },
    { label: loginMessage('login.enUS'), value: I18nEnum.EnUS },
  ];

  const getLoginErrorMessage = useCallback(
    (error: unknown) => {
      const rawMsg = String(
        (error as { response?: { data?: { msg?: string } }; msg?: string })?.response?.data?.msg ||
          (error as { msg?: string })?.msg ||
          ''
      ).trim();

      if (!rawMsg || rawMsg === 'invalid username or password') {
        return loginMessage('login.invalidCredentials');
      }
      const translated = loginMessage(microsoftAPIErrorMessages[rawMsg] || rawMsg);
      return translated === rawMsg ? rawMsg : translated;
    },
    [loginMessage]
  );

  const updateLoginLanguage = useCallback(
    async (userInfo: { sub?: string }) => {
      if (!userInfo?.sub) {
        return;
      }
      try {
        await updatePersonConfigApi({ userId: userInfo.sub, mainLanguage: selectedLanguage });
      } catch (error) {
        console.error('Failed to update user language after login:', error);
      }
    },
    [selectedLanguage]
  );

  useEffect(() => {
    setSelectedLanguage(lang);
  }, [lang]);

  useEffect(
    () => () => {
      if (availabilityTimer.current !== null) {
        window.clearTimeout(availabilityTimer.current);
      }
      availabilityRequest.current += 1;
    },
    []
  );

  useEffect(() => {
    let active = true;
    const redirectIfTokenValid = async () => {
      if (ssoTicket || !getToken()) {
        return false;
      }
      try {
        const userInfo = await getUserInfo();
        if (active) {
          window.location.replace(
            redirectUri || normalizeRedirectUri(getUserDefaultHomePath(userInfo, '')) || getDefaultPostLoginPath()
          );
        }
        return true;
      } catch (error) {
        console.error('Stored token is invalid, clearing token before login:', error);
        removeToken();
        return false;
      }
    };
    const init = async () => {
      const systemConfigPromise = getSystemConfig()
        .then((config: SystemInfoProps) => {
          if (active) {
            useBaseStore.setState((state) => ({ systemInfo: { ...state.systemInfo, ...config } }));
          }
          return config;
        })
        .catch((error) => {
          console.error('Failed to load login configuration:', error);
          return {};
        });
      const systemConfig = (await systemConfigPromise) as { externalLoginProviders?: { id: string; name: string }[] };
      if (!active) return;
      setExternalLoginProviders(systemConfig.externalLoginProviders || []);
      if (await redirectIfTokenValid()) return;
      if (active) setLoginReady(true);
    };
    void init();
    return () => {
      active = false;
    };
  }, [redirectUri, ssoTicket]);

  useEffect(() => {
    if (!loginReady || !ssoTicket || !ssoProvider) {
      return;
    }
    let active = true;
    getExternalSSOPending(ssoProvider, ssoTicket)
      .then((value) => {
        if (!active) {
          return;
        }
        setPending(value);
        setPendingError('');
        const suggestedUsername = value.suggestedUsername || '';
        registrationForm.setFieldsValue({ username: suggestedUsername });
        if (usernamePattern.test(suggestedUsername)) {
          const requestID = availabilityRequest.current + 1;
          availabilityRequest.current = requestID;
          setUsernameAvailability('checking');
          void checkExternalSSOUsername(ssoProvider, ssoTicket, suggestedUsername)
            .then((result) => {
              if (active && availabilityRequest.current === requestID) {
                setUsernameAvailability(result.available ? 'available' : 'taken');
              }
            })
            .catch(() => {
              if (active && availabilityRequest.current === requestID) {
                setUsernameAvailability('error');
              }
            });
        }
      })
      .catch((error) => {
        if (active) {
          setPendingError(getLoginErrorMessage(error));
        }
      })
      .finally(() => {
        if (active) {
          setPendingLoading(false);
        }
      });
    return () => {
      active = false;
    };
  }, [getLoginErrorMessage, loginReady, registrationForm, ssoTicket, ssoProvider]);

  const checkUsernameOnChange = (rawUsername: string) => {
    if (availabilityTimer.current !== null) {
      window.clearTimeout(availabilityTimer.current);
    }
    const requestID = availabilityRequest.current + 1;
    availabilityRequest.current = requestID;
    const username = rawUsername.trim();
    if (!username) {
      setUsernameAvailability('idle');
      return;
    }
    if (!usernamePattern.test(username)) {
      setUsernameAvailability('invalid');
      return;
    }
    setUsernameAvailability('checking');
    availabilityTimer.current = window.setTimeout(() => {
      checkExternalSSOUsername(ssoProvider, ssoTicket, username)
        .then((result) => {
          if (availabilityRequest.current === requestID) {
            setUsernameAvailability(result.available ? 'available' : 'taken');
          }
        })
        .catch(() => {
          if (availabilityRequest.current === requestID) {
            setUsernameAvailability('error');
          }
        });
    }, 350);
  };

  const onFinish = async (values: LoginFormValues) => {
    setLoading(true);
    try {
      const userInfo = await loginApi(values);
      if (userInfo?.sub) {
        try {
          await updatePersonConfigApi({ userId: userInfo.sub, mainLanguage: selectedLanguage });
        } catch (error) {
          console.error('登录后更新用户语言失败:', error);
        }
      }
      window.location.replace(
        redirectUri || normalizeRedirectUri(getUserDefaultHomePath(userInfo, '')) || getDefaultPostLoginPath()
      );
    } catch (error) {
      message.error(getLoginErrorMessage(error));
    } finally {
      setLoading(false);
    }
  };

  const onRegistrationFinish = async (values: RegistrationFormValues) => {
    const username = values.username.trim();
    setSsoLoading(true);
    try {
      const availability = await checkExternalSSOUsername(ssoProvider, ssoTicket, username);
      if (!availability.available) {
        setUsernameAvailability('taken');
        return;
      }
      const userInfo = await completeExternalSSO(ssoProvider, ssoTicket, username);
      await updateLoginLanguage(userInfo);
      window.location.replace(
        normalizeRedirectUri(pending?.redirectUri) ||
          normalizeRedirectUri(getUserDefaultHomePath(userInfo, '')) ||
          getDefaultPostLoginPath()
      );
    } catch (error) {
      message.error(getLoginErrorMessage(error));
    } finally {
      setSsoLoading(false);
    }
  };

  const onLinkFinish = async (values: LoginFormValues) => {
    setSsoLoading(true);
    try {
      const userInfo = await linkExternalSSO(ssoProvider, ssoTicket, values.username, values.password);
      await updateLoginLanguage(userInfo);
      window.location.replace(
        normalizeRedirectUri(pending?.redirectUri) ||
          normalizeRedirectUri(getUserDefaultHomePath(userInfo, '')) ||
          getDefaultPostLoginPath()
      );
    } catch (error) {
      message.error(getLoginErrorMessage(error));
    } finally {
      setSsoLoading(false);
    }
  };

  const resetToLocalLogin = () => {
    const query = redirectUri ? `?redirectUri=${encodeURIComponent(redirectUri)}` : '';
    window.location.replace(`${LOGIN_URL}${query}`);
  };

  const availabilityHelp = {
    checking: loginMessage('login.externalUsernameChecking'),
    available: loginMessage('login.externalUsernameAvailable'),
    taken: loginMessage('login.externalUsernameTaken'),
    invalid: loginMessage('login.externalUsernameInvalid'),
    error: loginMessage('login.externalUsernameCheckFailed'),
    idle: undefined,
  }[usernameAvailability];
  const availabilityStatus =
    usernameAvailability === 'available'
      ? 'success'
      : usernameAvailability === 'checking'
        ? 'validating'
        : ['taken', 'invalid', 'error'].includes(usernameAvailability)
          ? 'error'
          : undefined;
  const browserSSOError = ssoError ? loginMessage(`login.externalError.${ssoError}`) : '';

  const renderMicrosoftRegistration = () => {
    if (pendingLoading) {
      return <Button className="tier0-login__pending-loading" block loading />;
    }
    if (!pending || pendingError) {
      return (
        <div className="tier0-login__sso-error">
          <Alert type="error" showIcon message={pendingError || loginMessage('login.externalPendingInvalid')} />
          <Button block onClick={resetToLocalLogin}>
            {loginMessage('login.backToLocal')}
          </Button>
        </div>
      );
    }
    return (
      <div className="tier0-login__sso">
        <div className="tier0-login__identity">
          <span className="tier0-login__identity-name">{pending.displayName || pending.email}</span>
          {pending.email && pending.email !== pending.displayName ? (
            <span className="tier0-login__identity-email">{pending.email}</span>
          ) : null}
        </div>
        <Alert
          className="tier0-login__mfa"
          type="info"
          showIcon
          message={loginMessage(pending.mfaAuthenticated ? 'login.externalMfaVerified' : 'login.externalMfaManaged')}
        />
        <div className="tier0-login__mode-switch">
          <Button type={ssoMode === 'register' ? 'primary' : 'default'} onClick={() => setSsoMode('register')}>
            {loginMessage('login.externalCreateAccount')}
          </Button>
          <Button type={ssoMode === 'link' ? 'primary' : 'default'} onClick={() => setSsoMode('link')}>
            {loginMessage('login.externalLinkAccount')}
          </Button>
        </div>
        {ssoMode === 'register' ? (
          <Form<RegistrationFormValues>
            form={registrationForm}
            layout="vertical"
            requiredMark={false}
            onFinish={onRegistrationFinish}
            autoComplete="off"
            className="tier0-login__form"
          >
            <Form.Item
              label={loginMessage('login.externalChooseUsername')}
              name="username"
              validateStatus={availabilityStatus}
              help={availabilityHelp}
              hasFeedback
              rules={[
                { required: true, message: loginMessage('login.usernameRequired') },
                { pattern: usernamePattern, message: loginMessage('login.externalUsernameInvalid') },
              ]}
            >
              <Input
                size="large"
                maxLength={60}
                autoComplete="username"
                onChange={(event) => checkUsernameOnChange(event.target.value)}
              />
            </Form.Item>
            <Button
              type="primary"
              htmlType="submit"
              size="large"
              block
              loading={ssoLoading}
              disabled={usernameAvailability !== 'available'}
            >
              {loginMessage('login.externalContinue')}
            </Button>
          </Form>
        ) : (
          <Form<LoginFormValues>
            layout="vertical"
            requiredMark={false}
            onFinish={onLinkFinish}
            autoComplete="off"
            className="tier0-login__form"
          >
            <Form.Item
              label={loginMessage('login.username')}
              name="username"
              rules={[{ required: true, message: loginMessage('login.usernameRequired') }]}
            >
              <Input size="large" autoComplete="username" />
            </Form.Item>
            <Form.Item
              label={loginMessage('login.password')}
              name="password"
              rules={[{ required: true, message: loginMessage('login.passwordRequired') }]}
            >
              <Input.Password size="large" autoComplete="current-password" />
            </Form.Item>
            <Button type="primary" htmlType="submit" size="large" block loading={ssoLoading}>
              {loginMessage('login.externalLinkAndContinue')}
            </Button>
          </Form>
        )}
        <Button type="link" block className="tier0-login__local-link" onClick={resetToLocalLogin}>
          {loginMessage('login.backToLocal')}
        </Button>
      </div>
    );
  };

  return (
    <div className="tier0-login">
      <Select
        className="tier0-login__language"
        popupClassName="tier0-login__language-popup"
        aria-label={loginMessage('login.language')}
        value={selectedLanguage}
        options={languageOptions}
        onChange={(value) => {
          setSelectedLanguage(value);
          void initI18n(value, {}, { skipRemoteMessages: true });
        }}
      />
      <div className="tier0-login__layout">
        <aside
          className="tier0-login__visual"
          style={{
            backgroundImage: `url(${loginBackground})`,
          }}
        >
          <img className="tier0-login__logo" src={productLogo(systemInfo, true)} alt={productName(systemInfo)} />
        </aside>

        <section className="tier0-login__content">
          <main className="tier0-login__card" id="main">
            <h1 className="tier0-login__title" id="kc-page-title">
              {loginMessage(ssoTicket ? 'login.externalCompleteTitle' : 'login.title')}
            </h1>
            {browserSSOError && browserSSOError !== `login.externalError.${ssoError}` ? (
              <Alert className="tier0-login__warning" type="error" showIcon message={browserSSOError} />
            ) : null}
            {ssoTicket ? (
              renderMicrosoftRegistration()
            ) : (
              <>
                <Form<LoginFormValues>
                  layout="vertical"
                  requiredMark={false}
                  onFinish={onFinish}
                  autoComplete="off"
                  className="tier0-login__form"
                >
                  <Form.Item
                    label={loginMessage('login.username')}
                    name="username"
                    rules={[{ required: true, message: loginMessage('login.usernameRequired') }]}
                  >
                    <Input size="large" autoComplete="username" disabled={!loginReady} />
                  </Form.Item>
                  <Form.Item
                    label={loginMessage('login.password')}
                    name="password"
                    rules={[{ required: true, message: loginMessage('login.passwordRequired') }]}
                  >
                    <Input.Password size="large" autoComplete="current-password" disabled={!loginReady} />
                  </Form.Item>
                  <Button
                    type="primary"
                    htmlType="submit"
                    size="large"
                    block
                    loading={loading}
                    disabled={!loginReady}
                  >
                    {loginMessage('login.signIn')}
                  </Button>
                </Form>
                {externalLoginProviders.length > 0 ? (
                  <>
                    <Divider plain>{loginMessage('login.or')}</Divider>
                    {externalLoginProviders.map((provider) => (
                      <Button
                        key={provider.id}
                        className="tier0-login__microsoft-button"
                        size="large"
                        block
                        disabled={!loginReady}
                        onClick={() => window.location.assign(externalSSOStartURL(provider.id, redirectUri))}
                      >
                        {provider.id === 'microsoft' && (
                          <span className="tier0-login__microsoft-mark" aria-hidden="true">
                            <i />
                            <i />
                            <i />
                            <i />
                          </span>
                        )}
                        {loginMessage('login.externalSignIn', { provider: provider.name })}
                      </Button>
                    ))}
                    <p className="tier0-login__sso-note">{loginMessage('login.externalMfaNote')}</p>
                  </>
                ) : null}
              </>
            )}
          </main>
        </section>
      </div>
    </div>
  );
};

export default LoginPage;
