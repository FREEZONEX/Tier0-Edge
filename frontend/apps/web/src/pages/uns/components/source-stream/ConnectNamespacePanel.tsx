import { useEffect, useMemo, useState } from 'react';
import { Input } from 'antd';
import { ArrowRight, ChevronLeft, ChevronRight, Document, Folder, Search } from '@carbon/icons-react';
import ProTree from '@/components/pro-tree';
import ComButton from '@/components/com-button';
import { useTranslate } from '@/hooks';
import type { ConnectResource } from '@/apis/core-api/connect-workspace';
import type { ConnectTarget } from './connect-model';
import { buildConnectNamespace, type ConnectNamespaceNode } from './connect-namespace';
import styles from './connect-workspace.module.scss';

type Props = {
  targets: ConnectTarget[];
  resources: ConnectResource[];
  highlighted: ReadonlySet<string>;
  onSelect: (key: string) => void;
  onOpen: (target: ConnectTarget) => void;
};
type NamespaceTreeNode = { key: string; title: string; target: ConnectTarget; children?: NamespaceTreeNode[] };

export default function ConnectNamespacePanel({ targets, resources, highlighted, onSelect, onOpen }: Props) {
  const t = useTranslate();
  const [collapsed, setCollapsed] = useState(false);
  const [search, setSearch] = useState('');
  const [selected, setSelected] = useState<string[]>([]);
  const [expanded, setExpanded] = useState<string[]>([]);
  const model = useMemo(() => {
    const query = search.trim().toLocaleLowerCase();
    return buildConnectNamespace(
      targets.filter((target) => !query || `${target.name} ${target.id}`.toLocaleLowerCase().includes(query)),
      resources
    );
  }, [targets, resources, search]);
  const rootKeys = JSON.stringify(model.roots.filter((node) => node.children.length).map((node) => node.key));
  useEffect(() => {
    setExpanded((previous) => [...new Set([...previous, ...(JSON.parse(rootKeys) as string[])])]);
  }, [rootKeys]);
  useEffect(() => {
    if (search.trim()) setExpanded([...model.nodes.keys()]);
  }, [model, search]);
  const project = (nodes: ConnectNamespaceNode[]): NamespaceTreeNode[] =>
    nodes.map((node) => ({
      key: node.key,
      title: node.target.name,
      target: node.target,
      children: node.children.length ? project(node.children) : undefined,
    }));
  return (
    <aside className={`${styles['namespace-panel']} ${collapsed ? styles['namespace-collapsed'] : ''}`}>
      <header className={styles['namespace-heading']}>
        {!collapsed && <strong>{t('SourceStream.namespace')}</strong>}
        <ComButton
          type="text"
          aria-label={t(collapsed ? 'UnsConnect.expand' : 'UnsConnect.collapse')}
          aria-expanded={!collapsed}
          icon={collapsed ? <ChevronLeft size={18} /> : <ChevronRight size={18} />}
          onClick={() => setCollapsed((value) => !value)}
        />
      </header>
      {!collapsed && (
        <>
          <Input
            allowClear
            value={search}
            prefix={<Search size={16} />}
            aria-label={t('SourceStream.namespace') + ' · ' + t('SourceStream.search')}
            placeholder={t('SourceStream.search')}
            onChange={(event) => setSearch(event.target.value)}
          />
          <div className={styles['namespace-tree']} data-connect-namespace>
            <ProTree
              treeData={project(model.roots)}
              expandedKeys={expanded}
              selectedKeys={selected}
              onExpand={(keys) => setExpanded(keys.map(String))}
              onSelect={(keys) => {
                const key = keys[0]?.toString();
                setSelected(key ? [key] : []);
                if (key) onSelect(key);
              }}
              treeNodeIcon={(node) =>
                (node as NamespaceTreeNode).target.kind === 'directory' ? <Folder size={16} /> : <Document size={16} />
              }
              titleRender={(node) => {
                const target = (node as NamespaceTreeNode).target;
                return (
                  <span
                    data-connect-anchor={target.key}
                    className={highlighted.has(target.key) ? styles['namespace-highlight'] : undefined}
                    title={`${target.name} · ${target.id}`}
                  >
                    <span className={styles['namespace-port']} />
                    {target.name}
                    <small className={styles['namespace-kind']}>
                      {t(target.missing ? 'UnsConnect.unavailableRef' : `UnsConnect.kind.${target.kind}`)}
                      {target.space === 'legacy' && ` · ${t('UnsConnect.legacy')}`}
                    </small>
                  </span>
                );
              }}
              treeNodePersistentExtra={(node) => {
                const target = (node as NamespaceTreeNode).target;
                return (
                  <ComButton
                    type="text"
                    size="small"
                    icon={<ArrowRight size={16} />}
                    aria-label={`${t('common.detail')} · ${target.name}`}
                    disabled={target.missing}
                    onClick={(event) => {
                      event.stopPropagation();
                      onOpen(target);
                    }}
                  />
                );
              }}
            />
          </div>
          <footer className={styles['namespace-footer']}>
            {t('UnsConnect.resources')} · {targets.length}
          </footer>
        </>
      )}
    </aside>
  );
}
