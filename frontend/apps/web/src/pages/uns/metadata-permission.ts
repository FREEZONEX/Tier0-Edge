import type { UserInfoProps } from '@/stores/types';

// metadata:find、namespace/query 都要求 uns.read；编辑按钮或页面权限不能替代它。
export function canReadUnsMetadata(
  user: Pick<UserInfoProps, 'superAdmin' | 'resourceKeys'> | undefined,
  authEnable: boolean | undefined
): boolean {
  return authEnable === false || user?.superAdmin === true || user?.resourceKeys?.includes('uns.read') === true;
}
