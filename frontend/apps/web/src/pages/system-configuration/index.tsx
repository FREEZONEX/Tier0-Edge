import { useEffect, useState, type FC } from 'react';
import { App } from 'antd';
import ComContent from '@/components/com-layout/ComContent';
import ComLayout from '@/components/com-layout';
import ComSelect from '@/components/com-select';
import HelpTooltip from '@/components/help-tooltip';
import { useTranslate } from '@/hooks';
import {
  DEFAULT_LOGIN_SESSION_DURATION,
  getLoginSessionDuration,
  updateLoginSessionDuration,
  type LoginSessionDurationValue,
} from '@/apis/core-api/system-config';
import styles from './index.module.scss';

type SystemConfigurationProps = {
  title: string;
};

const SystemConfiguration: FC<SystemConfigurationProps> = ({ title }) => {
  const formatMessage = useTranslate();
  const { message } = App.useApp();
  const [sessionDuration, setSessionDuration] = useState<LoginSessionDurationValue>(DEFAULT_LOGIN_SESSION_DURATION);
  const [durationLoading, setDurationLoading] = useState(false);
  const sessionDurationOptions = [
    { value: '12h', label: formatMessage('settings.session12Hours') },
    { value: '1d', label: formatMessage('settings.session1Day') },
    { value: '7d', label: formatMessage('settings.session7Days') },
    { value: '30d', label: formatMessage('settings.session30Days') },
    { value: 'permanent', label: formatMessage('settings.sessionPermanent') },
  ];

  useEffect(() => {
    let mounted = true;
    getLoginSessionDuration()
      .then((value) => {
        if (mounted) {
          setSessionDuration(value);
        }
      })
      .catch(() => {
        // 保持默认展示,不阻塞页面其余部分
        if (mounted) {
          message.error(formatMessage('settings.sessionLoadFailed'));
        }
      });
    return () => {
      mounted = false;
    };
    // 初载仅请求一次;formatMessage/message 实例稳定,避免因 identity 变化重复请求
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const onSessionDurationChange = (value: LoginSessionDurationValue) => {
    // 选择后立即保存;失败时保留用户所选项并显示可读错误
    setSessionDuration(value);
    setDurationLoading(true);
    updateLoginSessionDuration(value)
      .then(() => {
        message.success(formatMessage('common.settingSuccess'));
      })
      .catch((error: any) => {
        // 优先后端返回的可读错误(如权限/参数校验),否则使用通用文案
        const reason = String(error?.msg || error?.message || '').trim();
        message.error(reason || formatMessage('settings.sessionSaveFailed'));
      })
      .finally(() => {
        setDurationLoading(false);
      });
  };

  return (
    <ComLayout>
      <ComContent title={title} hasBack={false} className={styles['system-configuration']}>
        <section className={styles.section}>
          <h2 className={styles['section-title']}>{formatMessage('settings.login')}</h2>
          <div className={styles.row}>
            <div className={styles.label}>
              <span>{formatMessage('settings.sessionValidity')}</span>
              <HelpTooltip title={formatMessage('settings.sessionGlobalDesc')} />
            </div>
            <ComSelect
              className={styles.select}
              value={sessionDuration}
              options={sessionDurationOptions}
              aria-label={formatMessage('settings.sessionValidity')}
              loading={durationLoading}
              disabled={durationLoading}
              onChange={(value) => onSessionDurationChange(value as LoginSessionDurationValue)}
            />
          </div>
        </section>
      </ComContent>
    </ComLayout>
  );
};

export default SystemConfiguration;
