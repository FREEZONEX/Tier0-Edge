import { Button, Result } from 'antd';
import { useTranslate } from '@/hooks';
import useNavigateForIframe from '@/hooks/useNavigateForIframe';
import { useBaseStore } from '@/stores/base';
import { getUserDefaultHomePath } from '@/utils/launchpad-site';

const NotFoundPage = () => {
  const formatMessage = useTranslate();
  const currentUserInfo = useBaseStore((state) => state.currentUserInfo);
  const homePage = getUserDefaultHomePath(currentUserInfo);
  const { security, onClick } = useNavigateForIframe({ path: homePage, replaceCurrent: true });

  return (
    <Result
      status="404"
      title={<span style={{ color: 'var(--ui-text-color)' }}>{formatMessage('common.notFound')}</span>}
      subTitle={<span style={{ color: 'var(--ui-text-color)' }}>{formatMessage('common.pageNotFound')}</span>}
      style={{ backgroundColor: 'var(--ui-bg-color)' }}
      extra={
        security && (
          <Button type="primary" onClick={onClick}>
            {formatMessage('common.goHome')}
          </Button>
        )
      }
    />
  );
};

export default NotFoundPage;
