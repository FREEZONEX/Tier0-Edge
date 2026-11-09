import type { ReactNode } from 'react';
import { Collapse } from 'antd';
import { CaretRight } from '@/components/lucide-icon/carbon';
import styles from './data-section.module.scss';

export default function DataSection({
  title,
  extra,
  children,
  compact = false,
}: {
  title: string;
  extra?: ReactNode;
  children: ReactNode;
  compact?: boolean;
}) {
  if (compact)
    return (
      <section>
        <div className={styles['compact-actions']}>{extra}</div>
        {children}
      </section>
    );
  return (
    <Collapse
      className={styles.section}
      bordered={false}
      defaultActiveKey={['content']}
      expandIcon={({ isActive }) => (
        <CaretRight size={20} style={{ rotate: isActive ? '90deg' : '0deg', transition: '200ms' }} />
      )}
      items={[{ key: 'content', label: title, extra, children }]}
    />
  );
}
