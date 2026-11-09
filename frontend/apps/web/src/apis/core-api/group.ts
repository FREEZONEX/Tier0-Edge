import { coreApi, mapFlow } from './core-adapter';

const normalizeType = (value: any) => Number(value || 1);

const flowTypeFor = (type: any) => (normalizeType(type) === 2 ? 'event' : 'source');

const flowGroupPayload = (data: any) => ({
  parentId: 0,
  flowType: flowTypeFor(data?.type),
  nodeType: 'folder',
  name: data?.name,
  description: data?.description || '',
  unsNodeIds: [],
});

export const addGroup = async (data: any) => {
  return mapFlow(await coreApi.post('/flows', flowGroupPayload(data)));
};

export const editGroup = async (data: any) => {
  return mapFlow(await coreApi.put(`/flows/${data?.id}`, flowGroupPayload(data)));
};

export const deleteGroup = async (uid: string) => coreApi.delete(`/flows/${uid}`);

export const markGroup = async (id: string, status: boolean) => {
  return mapFlow(await coreApi.put(`/flows/${id}/mark`, { pinned: status }));
};

export const getGroupDetail = async (id: string) => mapFlow(await coreApi.get(`/flows/${id}`));

export const optGroup = async (params: { type?: number; bizId: string; id: string; status: boolean }) => {
  const detail = await coreApi.get(`/flows/${params.bizId}`);
  return mapFlow(
    await coreApi.put(`/flows/${params.bizId}`, {
      parentId: params.status ? Number(params.id || 0) : 0,
      flowType: flowTypeFor(params.type),
      nodeType: 'flow',
      name: detail?.name || detail?.flowName,
      description: detail?.description || '',
      unsNodeIds: detail?.unsNodeIds || [],
    })
  );
};

export const getGroupList = async (params?: Record<string, unknown>) => {
  const keyword = String(params?.name || params?.keyword || '').trim();

  const resp = await coreApi.get('/flows', {
    params: {
      parentId: 0,
      keyword,
    },
  });
  return (resp?.list || []).map(mapFlow).filter((item: any) => item.category === 'group');
};
