/**
 * Launchpad 置顶（Pinned）的 resType 单一事实源。
 *
 * 与云端 saas（apps/saas-dashboard/types/enums/favorite.ts 的 PinnedResTypeEnum）
 * 以及移动端保持同一套取值，三端共用 sys_user_favorite 存储、只靠 res_type 区分。
 * 改名必须同步这三处与后端一次性迁移脚本，否则用户置顶会静默丢失。
 */
export const PinnedResType = {
  App: 'LaunchpadPinnedApp',
  Project: 'LaunchpadPinnedProject',
} as const;

export type PinnedResTypeValue = (typeof PinnedResType)[keyof typeof PinnedResType];
