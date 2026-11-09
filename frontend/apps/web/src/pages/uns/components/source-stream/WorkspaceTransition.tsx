import { type FC, type ReactNode } from 'react';
import classNames from 'classnames';
import { useIntegrationRoute } from './IntegrationTabs';
import styles from './workspace-transition.module.scss';

/** 工作区切换才滑动；列表、详情和弹窗沿用原来的路由生命周期。 */
const WorkspaceTransition: FC<{ children: ReactNode }> = ({ children }) => {
  const { view } = useIntegrationRoute();
  const connect = view === 'source' || view === 'stream';
  return (
    <div className={styles.viewport}>
      <div
        key={connect ? 'connect' : 'namespace'}
        className={classNames(styles.stage, connect ? styles.connect : styles.namespace)}
      >
        <div className={styles.content}>{children}</div>
      </div>
    </div>
  );
};

export default WorkspaceTransition;
