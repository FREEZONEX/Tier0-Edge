import { Button, Tooltip } from 'antd';
import { Launch } from '@carbon/icons-react';
import { useNavigate } from 'react-router';
import { useTranslate } from '@/hooks';
import type { TopologyMeta } from '@/apis/core-api/uns-topology';
import styles from './index.module.scss';

export default function AppEntry({ app, compact = false }: { app: TopologyMeta; compact?: boolean }) {
  const t = useTranslate();
  const navigate = useNavigate();
  const href = app.launch?.status === 'ready' ? app.launch.url : undefined;
  const entry = (
    <Tooltip title={href || t(`AppEntry.${app.launch?.status || 'unconfigured'}`)}>
      <span
        className={compact ? `${styles['row-action']} nodrag nopan` : undefined}
        onClick={(event) => event.stopPropagation()}
      >
        <Button
          type={compact ? 'text' : 'primary'}
          size={compact ? 'small' : 'middle'}
          icon={<Launch size={16} />}
          aria-label={compact ? `${t('AppEntry.open')} ${app.data.name}` : undefined}
          disabled={!href}
          onClick={(event) => {
            event.stopPropagation();
            navigate(`/uns/apps/${encodeURIComponent(app.id)}`, {
              state: { tabName: app.data.name, tabParentPathname: '/uns/topology' },
            });
          }}
        >
          {compact ? null : t('AppEntry.open')}
        </Button>
      </span>
    </Tooltip>
  );
  return compact ? entry : <section className={styles['detail-section']}>{entry}</section>;
}
