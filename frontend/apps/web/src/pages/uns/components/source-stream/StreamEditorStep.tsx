import type { ReactNode } from 'react';
import { Collapse } from 'antd';
import styles from './index.module.scss';

export default function StreamEditorStep({
  number,
  title,
  summary,
  defaultOpen = false,
  compact = false,
  children,
}: {
  number: number;
  title: string;
  summary?: ReactNode;
  defaultOpen?: boolean;
  compact?: boolean;
  children: ReactNode;
}) {
  if (compact && defaultOpen) return <>{children}</>;
  return (
    <Collapse
      className={styles['editor-step']}
      defaultActiveKey={defaultOpen ? ['content'] : []}
      expandIconPosition="end"
      items={[
        {
          key: 'content',
          label: (
            <span className={styles['step-heading']}>
              {!compact && <span className={styles['step-number']}>{number}</span>}
              <span className={styles['step-copy']}>
                <strong>{title}</strong>
                {!compact && summary && <span className={styles['step-summary']}>{summary}</span>}
              </span>
            </span>
          ),
          children,
        },
      ]}
    />
  );
}
