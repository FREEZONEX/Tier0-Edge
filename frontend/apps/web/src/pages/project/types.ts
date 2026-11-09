/**
 * Project 相关类型定义
 */

/**
 * 应用对象
 */
export interface AppItem {
  /** 应用 ID */
  appId: number;
  /** 本地安装的稳定授权主体 UUID，与导入来源及部署 UUID 分离 */
  appPrincipalId?: string;
  /** 应用稳定键 */
  appKey?: string;
  /** 来源应用的 App ID（导入/替换时记录在 manifest 中的包身份） */
  sourceAppId?: string;
  /** 来源项目的 Project ID（导入/替换时记录在 manifest 中的包身份） */
  sourceProjectId?: string;
  /** 来源工作空间的 Workspace ID（导入/替换时记录在 manifest 中的包身份） */
  sourceWorkspaceId?: string;
  /** 项目 ID */
  projectId: number;
  /** 应用名称（系统名） */
  name: string;
  /** 应用展示名 */
  displayName?: string;
  /** 应用版本 */
  version: string;
  /** 应用状态 */
  status: string;
  /** 应用类型 */
  siteType?: string;
  /** 应用来源类型 */
  appType?: string;
  /** 是否手工添加 */
  manual?: boolean;
  /** 是否在平台内打开 */
  openInPlatform?: boolean;
  /** 描述 */
  description?: string;
  /** 图标资产 ID */
  iconAssetId?: number;
  /** 封面资产 ID */
  coverAssetId?: number;
  /** 应用部署状态 */
  deployStatus?: string;
  /** 应用部署错误信息 */
  deployError?: string;
  /** 应用 schema 名（动态应用部署时预占/写入 project_app.schema_name） */
  schemaName?: string;
  networkAlias?: string;
  internalUrl?: string;
  /** 应用访问 URL */
  url?: string;
  /** 应用静态 URL */
  staticUrl: string;
  /** 应用图标地址 */
  iconUrl?: string;
  /** 应用封面地址 */
  coverUrl?: string;
  /** 流程 ID */
  flowId: string;
  /** 流程名称 */
  flowName: string;
  /** 角色列表 */
  accessibleRoles: string[];
  /** 创建人 */
  createdBy: string;
  /** 创建时间 */
  createdAt: string;
  /** 更新时间 */
  updatedAt: string;
}

/**
 * 项目对象（前端统一使用）
 */
export interface ProjectItem {
  /** 项目 ID */
  id: number;
  /** 项目名称（系统名） */
  name: string;
  /** 项目展示名 */
  displayName?: string;
  /** 项目描述 */
  description: string;
  /** 状态 */
  status: string;
  /** app数量 */
  appNums: number;
  /** 创建人 */
  createdBy: string;
  /** 创建时间 */
  createdAt: string;
  /** 更新时间 */
  updatedAt: string;
}

/**
 * Project 相关类型定义
 */

/**
 * 项目角色对象（前端统一使用）
 */
export interface ProjectRole {
  /** 角色 ID */
  roleId: number;
  /** 角色编码 */
  roleKey: string;
  /** 角色名称 */
  roleName: string;
  /** 角色描述 */
  description: string;
  /** 成员数量 */
  memberCount: number;
  /** 可访问应用列表 */
  apps: { appId: number; appName: string }[];
}

/**
 * 项目成员对象（前端统一使用）
 */
export interface ProjectMember {
  /** 成员 ID */
  memberId: number | string;
  /** 用户 ID */
  userId: string;
  /** 用户名称 */
  userName?: string;
  /** 邮箱 */
  email: string;
  /** 角色列表 */
  roles?: ProjectRole[];
  /** 更新时间 */
  updatedAt: string | number;
}
