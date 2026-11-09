/* eslint-disable @typescript-eslint/no-unused-vars */
import { CustomAxiosConfigEnum } from '@/utils/request';
import { authApi, coreApi, mapUnsNode, topicTypeNumber } from './core-adapter';

const nodeNameFromData = (data: any) => {
  const raw = data?.name || data?.alias || data?.path || data?.pathName || 'untitled';
  return String(raw).split('/').filter(Boolean).pop() || String(raw);
};

const rawNodeNameFromData = (data: any) => {
  const raw = data?.name || data?.path || data?.pathName || data?.alias || 'untitled';
  return String(raw).trim();
};

const topicTypeName = (value: unknown) => {
  const normalized = topicTypeNumber(value);
  if (normalized === 1) return 'State';
  if (normalized === 2) return 'Action';
  if (normalized === 3) return 'Metric';
  return '';
};

const toUnsPayload = (data: any) => {
  const pathType = Number(data?.pathType ?? data?.type);
  const explicitType = String(data?.type || data?.nodeType || '').toLowerCase();
  const isFile =
    pathType === 2 ||
    explicitType === 'file' ||
    (data?.pathType === undefined && data?.type === undefined && Array.isArray(data?.fields));
  const rawName = rawNodeNameFromData(data);
  const name = rawName.includes('/') ? rawName : nodeNameFromData(data);
  const topicType = isFile
    ? topicTypeName(data?.topicType) || topicTypeName(data?.parentDataType) || topicTypeName(data?.dataType) || 'State'
    : '';
  const addFlow = Boolean(data?.addFlow || data?.withFlow || data?.mockData);
  const mockData = Boolean(data?.mockData || data?.addFlow || data?.withFlow);
  const persistence = Boolean(data?.persistence);
  return {
    parentId: Number(data?.parentId || data?.targetId || 0),
    name,
    alias: data?.alias || '',
    displayName: data?.displayName || data?.showName || nodeNameFromData(data),
    type: isFile ? 'file' : 'folder',
    topicType,
    schema: JSON.stringify(data?.fields || data?.jsonFields || []),
    description: data?.description || data?.remark || '',
    extendProperties: JSON.stringify(data?.extendProperties || data?.extend || {}),
    withFlow: addFlow,
    addFlow,
    mockData,
    persistence,
    dataQualityPolicy: data?.dataQualityPolicy,
  };
};

const buildUnsTree = (list: any[]) => {
  const nodeMap = new Map<string, any>();
  const roots: any[] = [];
  list.forEach((item) => {
    nodeMap.set(String(item.id), { ...item, children: [] });
  });
  nodeMap.forEach((node) => {
    const parentKey = String(node.parentId || '');
    const parent = parentKey ? nodeMap.get(parentKey) : undefined;
    if (parent) {
      parent.children.push(node);
      parent.hasChildren = true;
      parent.isLeaf = false;
    } else {
      roots.push(node);
    }
  });
  const normalize = (node: any): any => {
    node.children = (node.children || []).map(normalize);
    node.countChildren = Number(node.countChildren ?? node.children.length);
    node.hasChildren = Boolean(node.hasChildren || node.children.length > 0 || node.countChildren > 0);
    node.isLeaf = node.pathType === 2 || !node.hasChildren;
    return node;
  };
  return roots.map(normalize);
};

const flattenUnsNodes = (nodes: any[]): any[] =>
  nodes.flatMap((node) => [node, ...flattenUnsNodes(Array.isArray(node.children) ? node.children : [])]);

const isFileNodeType = (value: unknown) => {
  const normalized = String(value || '')
    .trim()
    .toLowerCase();
  return normalized === '2' || normalized === 'file';
};

const isRecycleActionableNode = (node: any) =>
  Number(node?.deletedTime || 0) > 0 && Number(node?.recycleIsDel ?? 2) === 2;

const normalizeNodeId = (value: unknown) => {
  const id = String(value ?? '').trim();
  return id && id !== 'undefined' && id !== 'null' ? id : '';
};

const resolveNodeId = async (data: any) => {
  const directId = normalizeNodeId(data?.id || data?.unsId || data?.nodeId || data?.key);
  if (directId) {
    return directId;
  }
  const key = String(data?.alias || data?.path || data?.namespace || '').trim();
  if (!key) {
    return '';
  }
  const nodes = flattenUnsNodes(await listUns({ keyword: key }));
  return normalizeNodeId(
    nodes.find((item) => [item.alias, item.path, item.namespace].some((value) => String(value || '') === key))?.id
  );
};

const topLevelRecycleTargets = (nodes: any[], hasDeletedAncestor = false): any[] =>
  nodes.flatMap((node) => {
    const actionable = isRecycleActionableNode(node);
    const children = topLevelRecycleTargets(node.children || [], hasDeletedAncestor || actionable);
    return actionable && !hasDeletedAncestor ? [node] : children;
  });

const listUns = async (params?: Record<string, any>, options?: { directChildren?: boolean; tree?: boolean }) => {
  const projectId = params?.projectId;
  if (projectId) {
    const resp = await coreApi.get(`/projects/${projectId}/uns`);
    const keyword = String(params?.keyword || params?.key || '')
      .trim()
      .toLowerCase();
    let list = (resp?.list || []).map(mapUnsNode);
    if (keyword) {
      list = list.filter((item: any) =>
        [item.name, item.displayName, item.alias, item.path, item.namespace]
          .filter(Boolean)
          .some((value) => String(value).toLowerCase().includes(keyword))
      );
    }
    return options?.tree ? buildUnsTree(list) : list;
  }
  const keyword = params?.keyword || params?.key || params?.k;
  const parentId = params?.parentId ?? params?.targetId;
  const requestParams: Record<string, any> = {
    keyword,
    includeRecycle: params?.includeRecycle,
  };
  if (options?.directChildren && !keyword) {
    requestParams.parentId = parentId === undefined || parentId === null || parentId === '' ? 0 : parentId;
  } else if (parentId && parentId !== '0') {
    requestParams.parentId = parentId;
  }
  const resp = await coreApi.get('/uns/nodes', {
    params: requestParams,
  });
  let list = (resp?.list || resp?.nodes || []).map(mapUnsNode);
  if (isFileNodeType(params?.type)) {
    list = flattenUnsNodes(list).filter((item: any) => item.pathType === 2);
  }
  return options?.tree ? buildUnsTree(list) : list;
};

export const searchTreeData = async (params?: Record<string, unknown>) => listUns(params, { tree: true });
export const getTreeData = async (params?: Record<string, unknown>) => listUns(params, { tree: true });
export const getTypes = async (_params?: any): Promise<any> => [
  'INTEGER',
  'LONG',
  'FLOAT',
  'DOUBLE',
  'BOOLEAN',
  'STRING',
  'DATETIME',
  'BLOB',
  'LBLOB',
];
export const getLastMsg = async (_params?: any): Promise<any> => {
  const id = normalizeNodeId(_params?.id || _params?.unsId);
  if (!id) return {};
  const detail = await coreApi.get(`/uns/nodes/${id}`);
  return detail?.lastPayload || {};
};

export const addModel = async (data: any) => {
  const created = await coreApi.post('/uns/nodes', toUnsPayload(data));
  return { ...created, id: String(created?.id), parentId: String(created?.parentId || '') };
};

export const detectModel = async (_data?: any): Promise<any> => ({ valid: true });

export const editModel = async (data: any) => {
  const id = await resolveNodeId(data);
  if (!id) {
    return Promise.reject(new Error('无效的 UNS 节点 ID'));
  }
  const updated = await coreApi.put(`/uns/nodes/${id}`, toUnsPayload(data));
  return mapUnsNode(updated);
};

export const getModelInfo = async (params?: Record<string, unknown>) => {
  const id = normalizeNodeId(params?.id || params?.unsId);
  if (!id) return {};
  return mapUnsNode(await coreApi.get(`/uns/nodes/${id}`));
};

export const getInstanceInfo = async (params?: Record<string, unknown>) => getModelInfo(params);

export const getUnsDashboardData = async (params: {
  nodeId: string | number;
  timeStart?: number;
  timeEnd?: number;
  limit?: number;
}) => coreApi.get('/uns/dashboard', { params });

export const deleteTreeNode = async (params?: Record<string, unknown>) => {
  const id = normalizeNodeId(params?.id || params?.unsId);
  if (!id) {
    return Promise.reject(new Error('无效的 UNS 节点 ID'));
  }
  return coreApi.delete(`/uns/nodes/${id}`);
};

export const getRecycleTreeData = async () => {
  const resp = await coreApi.get('/uns/recycle');
  const list = (resp?.list || resp?.nodes || []).map(mapUnsNode);
  return buildUnsTree(list);
};

export const restoreRecycleNode = async (params: { id: string | number; confirm?: boolean }) =>
  coreApi.post(`/uns/nodes/${params.id}/restore`, { confirm: Boolean(params.confirm) });

export const forceDeleteRecycleNode = async (params: { id: string | number; deleteFlow?: boolean }) =>
  coreApi.delete(`/uns/nodes/${params.id}/force`, {
    params: { deleteFlow: Boolean(params.deleteFlow) },
  });

export const clearRecycleTree = async (params?: { deleteFlow?: boolean }) => {
  const roots = await getRecycleTreeData();
  const targets = topLevelRecycleTargets(roots);
  await Promise.all(
    targets.map((node: any) => forceDeleteRecycleNode({ id: node.id, deleteFlow: Boolean(params?.deleteFlow) }))
  );
  return { forceDeleted: targets.length };
};

export const exportExcel = async (data: any) =>
  coreApi.post('/uns/export', data || {}, {
    [CustomAxiosConfigEnum.NoCode]: true,
  });
export const exportExcelGlobal = async (data: any) =>
  coreApi.post('/uns/export/global', data || {}, {
    responseType: 'blob',
    [CustomAxiosConfigEnum.NoCode]: true,
  });
export const importUnsFile = async (data: FormData) =>
  coreApi.post('/uns/import', data, {
    headers: { 'Content-Type': 'multipart/form-data' },
  });
export const searchRestField = async (_data?: any): Promise<any> => [];

export const getAlertList = async (_params?: any): Promise<any> => ({ data: [], pageNo: 1, pageSize: 20, total: 0 });
export const getAlertForSelect = async (_params?: any): Promise<any> => [];
export const getTopologyStatus = async (_params?: any): Promise<any> => ({ status: 'done', nodes: [] });

export const getAllLabel = async (_params?: Record<string, unknown>) => {
  const resp = await coreApi.get('/uns/labels', {
    params: { keyword: _params?.keyword || _params?.key },
  });
  return (resp?.list || []).map((item: any) => ({
    ...item,
    id: String(item.id),
    labelName: item.labelName || item.name,
    title: item.labelName || item.name,
  }));
};
export const addLabel = async (_name?: any): Promise<any> => {
  const name = typeof _name === 'string' ? _name : _name?.name;
  return coreApi.post('/uns/labels', { name, color: _name?.color, description: _name?.description });
};
export const deleteLabel = async (_id?: any): Promise<any> => coreApi.delete(`/uns/labels/${_id}`);

export const makeLabel = async (_unsId?: any, _data?: any): Promise<any> => ({});

export const getLabelDetail = async (_id?: any): Promise<any> => coreApi.get(`/uns/labels/${_id}`);
export const getLabelPath = async (): Promise<any> => listUns({ includeRecycle: false });
export const updateLabel = async (_data?: any): Promise<any> => {
  const current = await getLabelDetail(_data?.id);
  return coreApi.put(`/uns/labels/${_data?.id}`, {
    name: _data?.name ?? current?.name,
    color: _data?.color ?? current?.color ?? '',
    description: _data?.description ?? current?.description ?? '',
  });
};
export const getLabelUnsId = async (path: any) => getModelInfo({ id: path });
export const verifyFileName = async (_params?: any): Promise<any> => ({ duplicate: false });
export const triggerRestApi = async (_params?: any): Promise<any> => ({});
const inferFieldType = (value: any) => {
  if (typeof value === 'boolean') return 'BOOLEAN';
  if (typeof value === 'number') return Number.isInteger(value) ? 'LONG' : 'DOUBLE';
  if (value instanceof Date) return 'DATETIME';
  if (typeof value === 'string') {
    if (/^\d{4}-\d{2}-\d{2}[T\s]/.test(value)) return 'DATETIME';
    return 'STRING';
  }
  return 'STRING';
};

const normalizeFieldName = (name: string, index = 0) => {
  const cleaned = String(name || '')
    .replace(/[^A-Za-z0-9_]/g, '_')
    .replace(/^[^A-Za-z]+/, '');
  return cleaned || `field_${index + 1}`;
};

const valueToFields = (value: any) => {
  const sample = Array.isArray(value) ? value.find((item) => item && typeof item === 'object') || value[0] : value;
  if (!sample || typeof sample !== 'object' || Array.isArray(sample)) {
    return [
      { name: 'value', type: inferFieldType(sample), maxLen: inferFieldType(sample) === 'STRING' ? 255 : undefined },
    ];
  }
  return Object.entries(sample).map(([key, val], index) => {
    const type = inferFieldType(val);
    return {
      name: normalizeFieldName(key, index),
      displayName: key,
      type,
      maxLen: type === 'STRING' ? 255 : undefined,
    };
  });
};

const collectJsonLeaves = (value: any, path = 'default'): any[] => {
  if (Array.isArray(value)) {
    return [{ dataPath: path, fields: valueToFields(value) }];
  }
  if (!value || typeof value !== 'object') {
    return [{ dataPath: path, fields: valueToFields(value) }];
  }
  const entries = Object.entries(value);
  if (entries.length === 0) return [{ dataPath: path, fields: [] }];
  const childLeaves = entries.flatMap(([key, val]) =>
    val && typeof val === 'object' ? collectJsonLeaves(val, path === 'default' ? key : `${path}.${key}`) : []
  );
  return childLeaves.length ? childLeaves : [{ dataPath: path, fields: valueToFields(value) }];
};

const jsonValueToTree = (value: any, path = ''): any[] => {
  if (!value || typeof value !== 'object') return [];
  const entries = Array.isArray(value) ? value.entries() : Object.entries(value);
  return Array.from(entries as Iterable<[string | number, any]>).map(([rawKey, val], index: number) => {
    const key = Array.isArray(value) ? `item${index + 1}` : String(rawKey);
    const dataPath = path ? `${path}.${key}` : key;
    const isBranch = val && typeof val === 'object' && !Array.isArray(val);
    const arrayObject = Array.isArray(val) && val.some((item) => item && typeof item === 'object');
    return {
      key: dataPath,
      dataPath,
      name: key,
      title: key,
      fields: valueToFields(val),
      children: isBranch ? jsonValueToTree(val, dataPath) : arrayObject ? [] : undefined,
    };
  });
};

export const ds2fs = async (_data?: any): Promise<any> => {
  const fields = Array.isArray(_data?.fields) ? _data.fields : [];
  return fields.map((field: any, index: number) => {
    const type = String(field?.type || field?.dataType || '').toLowerCase();
    const mappedType = type.includes('int')
      ? 'LONG'
      : type.includes('double') || type.includes('float') || type.includes('numeric') || type.includes('decimal')
        ? 'DOUBLE'
        : type.includes('bool')
          ? 'BOOLEAN'
          : type.includes('date') || type.includes('time')
            ? 'DATETIME'
            : 'STRING';
    return {
      name: normalizeFieldName(field?.name || field?.columnName || field?.fieldName, index),
      displayName: field?.comment || field?.displayName || field?.name || field?.columnName,
      type: mappedType,
      maxLen: mappedType === 'STRING' ? Number(field?.length || 255) : undefined,
    };
  });
};
export const json2fs = async (_data?: any): Promise<any> => collectJsonLeaves(_data);
export const json2fsTree = async (_data?: any): Promise<any> => jsonValueToTree(_data);
export const batchReverser = async (_data?: any): Promise<any> => {
  const nodes = Array.isArray(_data) ? _data : [];
  const tree = await listUns({ includeRecycle: false });
  const idByAlias = new Map<string, string>();
  tree.forEach((item: any) => {
    if (item.alias) idByAlias.set(item.alias, item.id);
    if (item.path) idByAlias.set(item.path, item.id);
  });
  for (const node of nodes.filter((item: any) => item.pathType === 0)) {
    const parentId = idByAlias.get(node.parentAlias) || '0';
    const created = await addModel({ ...node, parentId, pathType: 0 });
    if (node.alias) idByAlias.set(node.alias, created.id);
  }
  for (const node of nodes.filter((item: any) => item.pathType !== 0)) {
    const parentId = idByAlias.get(node.parentAlias) || '0';
    const created = await addModel({ ...node, parentId, pathType: 2 });
    if (node.alias) idByAlias.set(node.alias, created.id);
  }
  return { code: 200, data: nodes };
};

export const modifyModel = async (data: any) => editModel(data);
export const modifyDetail = async (data: any) => editModel(data);
export const modifyMountedHistory = async (data: any) => {
  const id = await resolveNodeId(data);
  if (!id) {
    return Promise.reject(new Error('无效的 UNS 节点 ID'));
  }
  const updated = await coreApi.put(`/uns/nodes/${id}`, {
    persistence: Boolean(data?.persistence),
  });
  return mapUnsNode(updated);
};

const findUnsNodeById = (nodes: any[], id: string): any | undefined => {
  for (const node of nodes) {
    if (String(node?.id) === id) {
      return node;
    }
    const found = findUnsNodeById(node?.children || [], id);
    if (found) {
      return found;
    }
  }
  return undefined;
};

const copyNodePayload = (node: any, parentId: string | number, override?: any) => {
  const payload = {
    parentId,
    pathType: node?.pathType,
    type: node?.pathType === 2 ? 'file' : 'folder',
    name: node?.pathName || node?.name,
    displayName: node?.displayName || '',
    description: node?.description || '',
    topicType: node?.topicType || node?.parentDataType,
    dataType: node?.dataType,
    parentDataType: node?.parentDataType,
    fields: node?.fields || node?.jsonFields || [],
    jsonFields: node?.jsonFields || node?.fields || [],
    extendProperties: node?.extendProperties || {},
    persistence: Boolean(node?.persistence),
    addFlow: Boolean(node?.addFlow || node?.withFlow || node?.mockData),
    mockData: Boolean(node?.mockData || node?.addFlow || node?.withFlow),
  };
  if (!override) {
    return payload;
  }
  const overrideName = override?.name || override?.pathName;
  return {
    ...payload,
    ...override,
    parentId,
    pathType: payload.pathType,
    type: payload.type,
    name: overrideName || payload.name,
    fields: override?.fields || override?.jsonFields || payload.fields,
    jsonFields: override?.jsonFields || override?.fields || payload.jsonFields,
    extendProperties: override?.extendProperties || override?.extend || payload.extendProperties,
    addFlow: Boolean(override?.addFlow ?? override?.withFlow ?? payload.addFlow),
    mockData: Boolean(override?.mockData ?? override?.addFlow ?? override?.withFlow ?? payload.mockData),
  };
};

const copyUnsNodeRecursive = async (node: any, parentId: string | number, override?: any): Promise<any> => {
  const created = await addModel(copyNodePayload(node, parentId, override));
  const createdId = created?.id;
  for (const child of node?.children || []) {
    await copyUnsNodeRecursive(child, createdId);
  }
  return created;
};

export const getUnsLazyTree = async (
  data: { parentId?: string; keyword?: string; key?: string; pageNo: number; pageSize: number; searchType?: number },
  _config?: any
) => {
  const list = await listUns(data, { directChildren: true });
  return {
    data: list,
    pageNo: data?.pageNo || 1,
    pageSize: data?.pageSize || list.length || 20,
    total: list.length,
  };
};

export const pageListUnsByLabel = async (_params?: any): Promise<any> => ({
  code: 0,
  data: [],
  pageNo: 1,
  pageSize: 20,
  total: 0,
});
export const cancelLabel = async (_id?: any, _data?: any): Promise<any> => ({});
export const makeSingleLabel = async (_unsId?: any, _labelId?: any): Promise<any> => ({});

export const updatePersonConfigApi = async (_data: { userId: string; mainLanguage: string }) =>
  authApi.put('/config', { mainLanguage: _data.mainLanguage });
export const getPersonConfigApi = async (_userId: string) => authApi.get('/config');
export const getPlugI18Api = async (_lang: string, _pluginId: string[]): Promise<any> => ({
  message: {},
  messages: {},
});

export const getUnsExportRecordsApi = async (_params?: any): Promise<any> => [];
export const unsExportRecordConfirmApi = async (_params?: any): Promise<any> => ({});
export const downloadUnsFile = async (_params?: any): Promise<any> => new Blob();
export const detectIfRemoveApi = async (_params: { id: any }): Promise<any> => ({ refs: 0 });

export const updateModelSubscribe = async (_params?: any): Promise<any> => ({});
export const updateLabelSubscribe = async (_params?: any): Promise<any> => ({});
export const subscribeFolderPage = async (_params?: any): Promise<any> => ({
  code: 0,
  data: [],
  pageNo: 1,
  pageSize: 20,
  total: 0,
});
export const subscribeFilePage = async (_params?: any): Promise<any> => ({
  code: 0,
  data: [],
  pageNo: 1,
  pageSize: 20,
  total: 0,
});
export const subscribeLabelPage = async (_params?: any): Promise<any> => ({
  code: 0,
  data: [],
  pageNo: 1,
  pageSize: 20,
  total: 0,
});

export const batchWriteFileValue = async (_data?: any): Promise<any> => ({ code: 0, data: {} });

const schema = {
  type: 'object',
  properties: {},
};
export const getFileSchema = async (): Promise<any> => schema;
export const getFolderSchema = async (): Promise<any> => schema;
export const getLabelSchema = async (): Promise<any> => schema;
export const getEmptyFolder = async (): Promise<any> => [];
export const saveMount = async (_data?: any): Promise<any> => ({ code: 0, data: {} });
export const getCollectorList = async (_params?: any): Promise<any> => [];
export const getSourceList = async (_params?: any): Promise<any> => [];
export const pasteUns = async (_data?: any): Promise<any> => {
  const sourceId = normalizeNodeId(_data?.sourceId);
  if (!sourceId) {
    return Promise.reject(new Error('invalid source node'));
  }
  const targetId = normalizeNodeId(_data?.targetId);
  const tree = await listUns({ includeRecycle: false }, { tree: true });
  const sourceNode = findUnsNodeById(tree, sourceId);
  if (!sourceNode) {
    return Promise.reject(new Error('source node not found'));
  }
  const created = await copyUnsNodeRecursive(sourceNode, targetId || 0, _data?.newF);
  return {
    code: 0,
    data: {
      id: created?.id,
      parentId: created?.parentId || targetId || '',
    },
  };
};

export { CustomAxiosConfigEnum };

export type UnsImportJob = {
  jobKey: string;
  status: 'pending' | 'running' | 'succeeded' | 'failed' | 'canceled';
  progress: number;
  resultJson?: { imported?: number; updated?: number; skipped?: number; phase?: string };
  errorMessage?: string;
};

export const uploadUnsImportFile = async (file: File): Promise<number> => {
  const response = await coreApi.uploads('/assets', [{ name: 'files', value: file, fileName: file.name }], {
    method: 'post',
  });
  const list = response?.list ?? response?.data?.list;
  const fileId = Number(Array.isArray(list) ? list[0]?.fileId : 0);
  if (!fileId) throw new Error('uns.importFailed');
  return fileId;
};

export const createUnsImportJob = async (sourceFileId: number): Promise<UnsImportJob> => {
  const response = await coreApi.post('/uns/import-jobs', { sourceFileId, dryRun: false });
  return response.job;
};

export const getUnsImportJob = async (jobKey: string, signal?: AbortSignal): Promise<UnsImportJob> => {
  const response = await coreApi.get(`/uns/import-jobs/${encodeURIComponent(jobKey)}`, {
    signal,
    timeout: 15000,
    [CustomAxiosConfigEnum.NoMessage]: true,
  });
  return response.job;
};
