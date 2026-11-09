import { Button, Checkbox, Select, Tooltip } from 'antd';
import { Renew, Flow, Application, DataBase, Document, Plug } from '@carbon/icons-react';
import { useTranslate } from '@/hooks';
import { groups, type Group } from './model';
import styles from './index.module.scss';

interface Props {
  core: Group;
  selected?: string;
  options: { value: string; label: string; searchText: string }[];
  shown: Group[];
  onCore: (core: Group) => void;
  onLocate: (key?: string) => void;
  onToggle: (group: Group, checked: boolean) => void;
  onRefresh: () => void;
}
const icons = {
  namespace: Document,
  collection: DataBase,
  source: Plug,
  stream: Flow,
  app: Application,
};
export default function CanvasToolbar(props: Props) {
  const t = useTranslate();
  return (
    <div className={styles['canvas-toolbar']}>
      <div className={styles['toolbar-primary']}>
        <div className={styles['core-picker']}>
          <Select
            aria-label={t('UnsGraph.core')}
            value={props.core}
            onChange={props.onCore}
            options={groups.map((group) => ({ value: group, label: t(`UnsGraph.${group}`) }))}
          />
        </div>
        <Select
          className={styles['locate-select']}
          aria-label={t('UnsGraph.object')}
          value={props.selected}
          onChange={props.onLocate}
          allowClear
          showSearch
          placeholder={t('UnsGraph.locateObject')}
          options={props.options}
          filterOption={(query, option) =>
            (option?.searchText || '').toLocaleLowerCase().includes(query.toLocaleLowerCase())
          }
        />
        <div className={styles['category-pills']} role="group" aria-label={t('UnsGraph.categories')}>
          {groups
            .filter((group) => group !== props.core)
            .map((group) => {
              const Icon = icons[group];
              return (
                <Checkbox
                  key={group}
                  className={props.shown.includes(group) ? styles['checked-category'] : ''}
                  checked={props.shown.includes(group)}
                  onChange={(event) => props.onToggle(group, event.target.checked)}
                >
                  <Icon size={14} />
                  {t(`UnsGraph.${group}`)}
                </Checkbox>
              );
            })}
        </div>
        <Tooltip title={t('UnsGraph.refresh')}>
          <Button
            className={styles['toolbar-utilities']}
            icon={<Renew size={16} />}
            aria-label={t('UnsGraph.refresh')}
            onClick={props.onRefresh}
          />
        </Tooltip>
      </div>
    </div>
  );
}
