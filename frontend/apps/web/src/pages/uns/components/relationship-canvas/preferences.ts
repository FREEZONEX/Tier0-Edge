import { groups, type Group } from './model';

const storageKey = 'tier0.uns.topology.preferences.v1';
export interface CanvasPreferences {
  core: Group;
  enabled: Group[];
}
const defaults = (): CanvasPreferences => ({ core: 'app', enabled: ['app', 'collection', 'stream'] });
const isGroup = (value: unknown): value is Group => typeof value === 'string' && groups.includes(value as Group);

export function loadCanvasPreferences(): CanvasPreferences {
  try {
    const cached = localStorage.getItem(storageKey);
    if (!cached) return defaults();
    const value = JSON.parse(cached);
    if (value?.version !== 1 || !isGroup(value.core) || !Array.isArray(value.enabled)) return defaults();
    return { core: value.core, enabled: [...new Set<Group>(value.enabled.filter(isGroup))] };
  } catch {
    // 缓存损坏或浏览器禁用存储时，页面仍按默认组合正常打开。
    return defaults();
  }
}

export function saveCanvasPreferences(value: CanvasPreferences) {
  try {
    // 只保存显示偏好，不缓存对象、搜索词、账号或授权数据。
    localStorage.setItem(storageKey, JSON.stringify({ version: 1, ...value }));
  } catch {
    // 受限存储不能阻止本轮切换核心对象和勾选分类。
  }
}
