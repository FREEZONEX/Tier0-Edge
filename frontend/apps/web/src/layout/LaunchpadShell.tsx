import { useRef } from 'react';
import ComGroupButton from '@/components/com-group-button';
import { User } from '@/components/lucide-icon/carbon';
import NotificationCenter from '@/components/notification-center';
import { TabsContext, type TabsContextProps } from '@/contexts/tabs-context';
import { useTranslate } from '@/hooks';
import TabsLayout from '@/layout/components/TabsLayout';
import LogoImg from '@/layout/custom-menu-header/components/LogoImg';
import { MenuTypeEnum, ThemeType, useThemeStore } from '@/stores/theme-store.ts';
import { useNavigate } from 'react-router';
import { withLaunchpadStandaloneQuery } from '@/utils/launchpad-site';
import styles from './LaunchpadShell.module.scss';

const LaunchpadShell = () => {
  const tabsContextRef = useRef<TabsContextProps>(null);
  const navigate = useNavigate();
  const theme = useThemeStore((state) => state.theme);
  const formatMessage = useTranslate();

  return (
    <TabsContext.Provider value={tabsContextRef as any}>
      <div className={styles['launchpad-shell']}>
        <header className={styles['shell-header']}>
          <div className={styles['shell-logo']}>
            <LogoImg
              isDark={theme === ThemeType.Dark}
              width={136}
              onClick={() => {
                navigate(withLaunchpadStandaloneQuery('/launchpad'));
              }}
            />
          </div>
          <nav id="custom-header-container" className={styles['shell-tabs']} aria-label="Launchpad tabs" />
          <div className={styles['user-area']}>
            <ComGroupButton
              options={[
                {
                  label: <NotificationCenter />,
                  title: formatMessage('messageCenter.title', undefined, 'Message Center'),
                  key: 'notifications',
                },
                {
                  label: <User size={20} style={{ color: 'var(--ui-text-color)' }} />,
                  title: 'user',
                  key: 'user',
                },
              ]}
            />
          </div>
        </header>
        <main className={styles['shell-main']}>
          <TabsLayout menuType={MenuTypeEnum.Top} tabsContextRef={tabsContextRef} />
        </main>
      </div>
    </TabsContext.Provider>
  );
};

export default LaunchpadShell;
