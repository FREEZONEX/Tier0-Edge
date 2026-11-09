import CustomMenuHeader from './custom-menu-header';
import GlobalNoticeAlerts from './components/global-notice-alerts';
import BusinessSidebar from './BusinessSidebar';
import TabsLayout from './components/TabsLayout';
import { useChangeMenuName } from '@/hooks';
import { useRef } from 'react';
// import { useTips } from '@/hooks/useTips';
// import { tips } from './tips';
import { TabsContext, type TabsContextProps } from '@/contexts/tabs-context';
import IframeMask from '@/components/iframe-mask';
// import { useBaseStore } from '@/stores/base';
import { MenuTypeEnum } from '@/stores/theme-store.ts';

const Module = () => {
  // const systemInfo = useBaseStore((state) => state.systemInfo);
  // 用来接收tabs的公共方法
  const tabsContextRef = useRef<TabsContextProps>(null);
  useChangeMenuName();

  // useTips(tips({ appTitle: systemInfo.appTitle }));
  const tabsContent = <TabsLayout menuType={MenuTypeEnum.Top} tabsContextRef={tabsContextRef} />;

  return (
    <TabsContext.Provider value={tabsContextRef as any}>
      <div style={{ overflow: 'hidden', display: 'flex', height: '100vh' }}>
        <BusinessSidebar />
        <div style={{ flex: 1, minWidth: 0, display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
          <CustomMenuHeader />
          <GlobalNoticeAlerts />
          <div style={{ flex: 1, minHeight: 0, overflow: 'hidden' }}>{tabsContent}</div>
        </div>
        <IframeMask />
      </div>
    </TabsContext.Provider>
  );
};
export default Module;
