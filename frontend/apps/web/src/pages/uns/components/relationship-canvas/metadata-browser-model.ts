import type { UnsMetadata } from '@/apis/core-api/uns-metadata';

export type MetadataRow = Omit<UnsMetadata, 'kind' | 'data'> & { kind: string; data: Record<string, unknown> };

// 与元数据和专用资源列表的读取权限保持一致；页面权限不代表对象读取权限。
const kindPermissions = {
  collection: 'uns.read',
  topic: 'uns.read',
  directory: 'uns.read',
  source: 'uns.source',
  stream: 'uns.stream',
  app: 'uns.app.read',
} as const;

export function readableMetadataKinds(resourceKeys: readonly string[] = []) {
  return Object.entries(kindPermissions)
    .filter(([, permission]) => resourceKeys.includes(permission))
    .map(([kind]) => kind);
}

const scalarFields: Record<string, string[]> = {
  collection: [],
  topic: ['parentId', 'collectionId', 'viewMode'],
  directory: ['parentId'],
  source: ['adapter'],
  stream: ['executionMode', 'status', 'activeVersion', 'pendingVersion'],
  source_state: ['health', 'configRevision', 'activeVersion', 'checkedAt', 'lastDataAt'],
  source_plan: ['sourceId', 'streamId', 'status', 'version'],
  source_version: ['sourceId', 'streamId', 'status', 'version', 'publishedAt'],
  app: ['status'],
};

// 进入表格状态前投影，避免把连接配置、脚本、扩展或运行数据交给 JSON 展开器。
export function metadataRowForDisplay(row: MetadataRow): MetadataRow {
  const data: Record<string, unknown> = {};
  const fields = scalarFields[row.kind];
  if (fields) {
    for (const field of ['schemaVersion', 'name', 'description', ...fields]) {
      const value = row.data?.[field];
      if (value === null || ['string', 'number', 'boolean'].includes(typeof value)) data[field] = value;
    }
    // Collection / Topic 的字段定义是用户需要检查的数据契约。
    const contracts =
      row.kind === 'collection' ? ['recordSchema', 'sampleSchema'] : row.kind === 'topic' ? ['fields'] : [];
    for (const field of contracts) {
      if (row.data?.[field] !== undefined) data[field] = row.data[field];
    }
  }
  return {
    id: row.id,
    kind: row.kind,
    revision: row.revision,
    createdAt: row.createdAt,
    updatedAt: row.updatedAt,
    deletedAt: row.deletedAt,
    data,
  };
}
