export type McpAccessPreset = 'read' | 'write' | 'all';

const readScopes = ['uns.read', 'uns.source', 'uns.stream', 'uns.app.read'];
const writeScopes = ['uns.model.manage', 'uns.write', 'uns.source.manage', 'uns.source.credentials', 'uns.stream.manage', 'uns.app.manage'];

// 分组只是呈现方式；只能选择客户端申请且当前用户可授予的范围。
export const mcpPresetScopes = (preset: McpAccessPreset, requested: string[], available: string[]) => {
  const scopes = preset === 'read' ? readScopes : [...readScopes, ...writeScopes];
  const allowed = preset === 'all' ? [...scopes, 'uns.delete'] : scopes;
  return allowed.filter((scope) => requested.includes(scope) && available.includes(scope));
};

export const mcpGrantPreset = (scopes: string[]): McpAccessPreset =>
  scopes.includes('uns.delete') ? 'all' : scopes.some((scope) => writeScopes.includes(scope)) ? 'write' : 'read';
