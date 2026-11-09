import { Popover, Radio, Select } from 'antd';
import { SettingsAdjust } from '@carbon/icons-react';
import ComButton from '@/components/com-button';
import { useTranslate } from '@/hooks';
import styles from './connect-list.module.scss';

export type ConnectGrouping = 'status' | 'type' | 'none';
export default function ConnectDisplay({
  group,
  onGroup,
  cards,
  onCards,
}: {
  group: ConnectGrouping;
  onGroup: (group: ConnectGrouping) => void;
  cards?: boolean;
  onCards?: (cards: boolean) => void;
}) {
  const t = useTranslate();
  return (
    <Popover
      trigger="click"
      placement="bottomRight"
      content={
        <div className={styles.display}>
          {onCards && (
            <div>
              <span>{t('ConnectList.view')}</span>
              <Radio.Group
                size="small"
                value={cards}
                onChange={(event) => onCards(event.target.value)}
                options={[
                  { label: t('ConnectList.list'), value: false },
                  { label: t('ConnectList.cards'), value: true },
                ]}
                optionType="button"
              />
            </div>
          )}
          <div>
            <span>{t('ConnectList.groupBy')}</span>
            <Select
              aria-label={t('ConnectList.groupBy')}
              value={group}
              onChange={onGroup}
              options={(onCards ? ['status', 'type', 'none'] : ['status', 'none']).map((value) => ({
                value,
                label: t(`ConnectList.${value}`),
              }))}
            />
          </div>
        </div>
      }
    >
      <ComButton icon={<SettingsAdjust size={16} />} aria-label={t('ConnectList.display')} />
    </Popover>
  );
}
