import { type FC } from 'react';
import { Descriptions, Typography } from 'antd';
import { Close } from '@carbon/icons-react';
import type { LoginLogItem } from '@/apis/core-api/login-log';
import ComButton from '@/components/com-button';
import ComDrawer from '@/components/com-drawer';
import { useTranslate } from '@/hooks';
import { formatTimestamp } from '@/utils/format';
import {
  getLoginAccountLabel,
  getLoginEventLabel,
  getLoginIpLabel,
  getLoginLocationLabel,
  getLoginMessageLabel,
} from '../presentation';
import LoginLogStatus from './LoginLogStatus';
import styles from '../index.module.scss';

type Props = {
  record: LoginLogItem | null;
  onClose: () => void;
};

const LoginLogDetailDrawer: FC<Props> = ({ record, onClose }) => {
  const t = useTranslate();
  const fields = record
    ? [
        { key: 'time', label: t('loginLog.time', {}, 'Time'), children: formatTimestamp(record.createdAt) || '-' },
        { key: 'event', label: t('loginLog.event', {}, 'Event'), children: getLoginEventLabel(record, t) },
        { key: 'status', label: t('loginLog.status', {}, 'Status'), children: <LoginLogStatus code={record.code} /> },
        { key: 'account', label: t('loginLog.account', {}, 'Account'), children: getLoginAccountLabel(record) },
        { key: 'ip', label: t('loginLog.ip', {}, 'IP Address'), children: getLoginIpLabel(record) },
        { key: 'location', label: t('loginLog.location', {}, 'Location'), children: getLoginLocationLabel(record) },
        { key: 'browser', label: t('loginLog.browser', {}, 'Browser'), children: record.browser || '-' },
        { key: 'os', label: t('loginLog.os', {}, 'OS'), children: record.os || '-' },
        {
          key: 'message',
          label: t('loginLog.message', {}, 'Message'),
          children: (
            <Typography.Paragraph className={styles['detail-message']}>
              {getLoginMessageLabel(record, t)}
            </Typography.Paragraph>
          ),
        },
      ]
    : [];

  return (
    <ComDrawer
      open={Boolean(record)}
      title={t('loginLog.title', {}, 'Login Log')}
      width={560}
      onClose={onClose}
      destroyOnHidden
      styles={{ body: { padding: 24 } }}
      extra={
        <ComButton
          type="text"
          aria-label={t('common.close', {}, 'Close')}
          icon={<Close size={20} />}
          onClick={onClose}
        />
      }
    >
      <Descriptions className={styles['detail-fields']} column={1} layout="horizontal" items={fields} />
    </ComDrawer>
  );
};

export default LoginLogDetailDrawer;
