import { useParams } from 'react-router';
import AppDetailPage from './app-detail';
import styles from './mini-app.module.scss';

// Native navigation owns this view; reuse the authorized app loader without
// mounting the Enterprise workspace shell.
const MiniAppPage = () => {
  const { projectName, appName } = useParams();
  return (
    <main className={styles['mini-app']}>
      <AppDetailPage projectName={projectName} appName={appName} appOnly />
    </main>
  );
};

export default MiniAppPage;
