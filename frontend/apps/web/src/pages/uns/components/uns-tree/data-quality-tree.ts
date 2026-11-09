import type { DataQualityActiveSummaryItem } from '@/apis/core-api/data-quality';
import type { UnsTreeNode } from '@/pages/uns/types';

export type QualityTreeSeverity = 'attention' | 'critical';

export interface QualityTreeStatus {
  count: number;
  severity: QualityTreeSeverity;
}

const normalizeNamespace = (namespace?: string) => (namespace || '').split('/').filter(Boolean).join('/');

const severityRank = (severity: QualityTreeSeverity) => (severity === 'critical' ? 2 : 1);

const toTreeSeverity = (severity: DataQualityActiveSummaryItem['severity']): QualityTreeSeverity =>
  severity === 'critical' ? 'critical' : 'attention';

const mergeSeverity = (current: QualityTreeSeverity | undefined, incoming: QualityTreeSeverity): QualityTreeSeverity =>
  !current || severityRank(incoming) > severityRank(current) ? incoming : current;

const namespacePrefixes = (namespace: string) => {
  const segments = normalizeNamespace(namespace).split('/').filter(Boolean);
  return segments.map((_, index) => segments.slice(0, index + 1).join('/'));
};

export const buildQualityTreeStatus = (issues: DataQualityActiveSummaryItem[]) => {
  const leafStatusById = new Map<string, QualityTreeStatus>();
  const namespacesById = new Map<string, Set<string>>();

  for (const issue of issues) {
    const id = String(issue.unsId);
    const severity = toTreeSeverity(issue.severity);
    const current = leafStatusById.get(id);
    leafStatusById.set(id, {
      count: 1,
      severity: mergeSeverity(current?.severity, severity),
    });

    const namespace = normalizeNamespace(issue.namespace);
    if (namespace) {
      const namespaces = namespacesById.get(id) || new Set<string>();
      namespaces.add(namespace);
      namespacesById.set(id, namespaces);
    }
  }

  const descendantIdsByPath = new Map<string, Set<string>>();
  const descendantSeverityByPath = new Map<string, QualityTreeSeverity>();
  for (const [id, namespaces] of namespacesById) {
    const severity = leafStatusById.get(id)?.severity;
    if (!severity) continue;

    for (const namespace of namespaces) {
      for (const prefix of namespacePrefixes(namespace)) {
        const ids = descendantIdsByPath.get(prefix) || new Set<string>();
        ids.add(id);
        descendantIdsByPath.set(prefix, ids);
        descendantSeverityByPath.set(prefix, mergeSeverity(descendantSeverityByPath.get(prefix), severity));
      }
    }
  }

  const folderStatusByPath = new Map<string, QualityTreeStatus>();
  for (const [path, ids] of descendantIdsByPath) {
    const severity = descendantSeverityByPath.get(path);
    if (severity) {
      folderStatusByPath.set(path, { count: ids.size, severity });
    }
  }

  return { folderStatusByPath, leafStatusById };
};

export const qualityStatusForNode = (
  node: UnsTreeNode,
  status: ReturnType<typeof buildQualityTreeStatus>
): QualityTreeStatus | undefined => {
  if (node.pathType === 2) {
    return status.leafStatusById.get(String(node.id));
  }
  if (node.pathType === 0) {
    return status.folderStatusByPath.get(normalizeNamespace(node.path));
  }
  return undefined;
};
