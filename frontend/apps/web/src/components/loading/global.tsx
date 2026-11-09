import { type FC } from 'react';
import useTranslate from '@/hooks/useTranslate';
import styles from './global.module.scss';

const GlobalLoading: FC = () => {
  const t = useTranslate();

  return (
    <div className={styles['global-loading']} role="status" aria-label={t('common.loading')}>
      <span className={styles.spinner} aria-hidden="true" />
    </div>
  );
};

export default GlobalLoading;
