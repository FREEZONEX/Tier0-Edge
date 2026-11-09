import { type FC } from 'react';
import { Tag } from 'antd';
import { useTranslate } from '@/hooks';
import { LOGIN_SUCCESS } from '../filters';
import { getLoginStatusLabel } from '../presentation';
import styles from '../index.module.scss';

const LoginLogStatus: FC<{ code: number }> = ({ code }) => {
  const t = useTranslate();
  return (
    <Tag className={`${styles.status} ${code === LOGIN_SUCCESS ? styles.success : styles.failed}`}>
      {getLoginStatusLabel(code, t)}
    </Tag>
  );
};

export default LoginLogStatus;
