import { Divider } from 'antd';
import Overview from './Overview.tsx';
// import { useUnsContext } from '@/pages/uns/UnsContext.tsx';
import { useDeepCompareEffect } from 'ahooks';
import { Connect, DataConnected, Package } from '@/components/lucide-icon/carbon';
import { useImmer } from 'use-immer';
import type { OverviewListProps } from './type';
import Icon from '@ant-design/icons';
import PackageTop from '@/components/svg-components/PackageTop.tsx';
import styles from './index.module.scss';
import Functions from './Functions.tsx';
import MQTT from './MQTT.tsx';
import useUnsGlobalWs from '@/pages/uns/useUnsGlobalWs.ts';

const UnsDashboard = () => {
  // const { topologyData } = useUnsContext();
  const { topologyData = {} } = useUnsGlobalWs();
  const [overviewList, setOverviewList] = useImmer<OverviewListProps[]>([
    { key: 'messageInThroughput', label: 'uns.messageIn', icon: <Package size={24} />, value: 0, unit: 'uns.msgUnit' },
    {
      key: 'messageOutThroughput',
      label: 'uns.messageOut',
      icon: <Icon component={PackageTop} style={{ fontSize: 24 }} />,
      value: 0,
      unit: 'uns.msgUnit',
    },
    {
      key: 'allConnections',
      label: 'uns.allConnections',
      icon: <Connect size={24} />,
      value: 0,
    },
    {
      key: 'liveConnections',
      label: 'uns.liveConnections',
      icon: <DataConnected size={24} />,
      value: 0,
    },
  ]);

  useDeepCompareEffect(() => {
    const result: { [key: string]: string } = {};
    Object.keys(topologyData).forEach((key) => {
      result[key.toLowerCase()] = topologyData[key];
    });

    setOverviewList((draft) => {
      return draft.map((item: any) => {
        const key = item.key.split(' ').pop().toLowerCase();
        if (Object.prototype.hasOwnProperty.call(result, key)) {
          return {
            ...item,
            value: result[key],
          };
        }
        return item;
      });
    });
  }, [topologyData]);

  return (
    <div className={styles['uns-dashboard']}>
      <Overview overviewList={overviewList} />
      <Divider className={styles['dashboard-divider']} />
      <div className={styles['dashboard-content']}>
        <div className={styles['access-column']}>
          <div className={styles['mqtt-wrapper']}>
            <MQTT />
          </div>
        </div>
        <div className={styles['functions-wrapper']}>
          <Functions />
        </div>
      </div>
    </div>
  );
};

export default UnsDashboard;
