import { DataTable, Link, Folder } from '@carbon/icons-react';
import type { UnsMetadata } from '@/apis/core-api/uns-metadata';
import type { UnsTreeNode } from '../../types';
import { UnsTreeNodeIcon } from '../uns-tree/tree-icons';

export default function NamespaceNodeIcon({ node, items }: { node: UnsTreeNode; items: UnsMetadata[] }) {
  const item = items.find((value) => value.id === String(node.key));
  if (item?.kind === 'folder') return <Folder size={16} />;
  if (item?.kind === 'collection') return <DataTable size={16} />;
  if (item?.kind === 'topic') return <Link size={16} />;
  return <UnsTreeNodeIcon dataNode={node} />;
}
