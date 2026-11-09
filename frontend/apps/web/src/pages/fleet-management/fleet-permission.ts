import { useBaseStore } from '@/stores/base';

// 使用后端 License 能力与既有业务权限，不根据 Cluster 名称推断节点角色。
export function useFleetConnectionAccess() {
  const isHost = useBaseStore((state) => state.systemInfo?.cluster?.isHost === true);
  const user = useBaseStore((state) => state.currentUserInfo);
  const authEnable = useBaseStore((state) => state.systemInfo?.authEnable);
  const allowed =
    authEnable === false ||
    user?.superAdmin === true ||
    !!user?.resourceKeys?.includes(isHost ? 'cluster.edge.manage' : 'fleet.connection.manage');
  return { isHost, allowed };
}
