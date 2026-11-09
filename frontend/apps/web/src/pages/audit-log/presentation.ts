import type { AuditLogItem } from '@/apis/core-api/audit-log';

type Translate = (id: string, opt?: any, defaultMessage?: string, description?: string | object) => string;

const moduleLabelMap: Record<string, { id: string; fallback: string }> = {
  Fleet: { id: 'auditLog.module.Fleet', fallback: 'Fleet' },
  Platform: { id: 'auditLog.module.Platform', fallback: 'Platform' },
  AuditLog: { id: 'auditLog.resource.AuditLog', fallback: 'Audit Log' },
  Auth: { id: 'auditLog.module.Auth', fallback: 'Authentication' },
  UserManagement: { id: 'auditLog.module.UserManagement', fallback: 'User Management' },
  Role: { id: 'auditLog.module.Role', fallback: 'Role' },
  Project: { id: 'auditLog.module.Project', fallback: 'System' },
  ProjectMember: { id: 'auditLog.module.ProjectMember', fallback: 'Project Member' },
  ProjectApp: { id: 'auditLog.module.ProjectApp', fallback: 'Project App' },
  UNS: { id: 'auditLog.module.UNS', fallback: 'UNS' },
  Resource: { id: 'auditLog.module.Resource', fallback: 'Resource' },
  License: { id: 'auditLog.module.License', fallback: 'License' },
  App: { id: 'auditLog.module.App', fallback: 'App' },
  Template: { id: 'auditLog.module.Template', fallback: 'Template' },
  Label: { id: 'auditLog.module.Label', fallback: 'Label' },
  Group: { id: 'auditLog.module.Group', fallback: 'Group' },
  Dashboard: { id: 'auditLog.module.Dashboard', fallback: 'Dashboard' },
  AppKey: { id: 'auditLog.module.AppKey', fallback: 'App Key' },
  SourceFlow: { id: 'auditLog.module.SourceFlow', fallback: 'Collection Flow' },
  EventFlow: { id: 'auditLog.module.EventFlow', fallback: 'Event Flow' },
  Notebook: { id: 'auditLog.module.Notebook', fallback: 'Notebook' },
  AlarmRule: { id: 'auditLog.module.AlarmRule', fallback: 'Alarm Rule' },
  Alarm: { id: 'auditLog.module.Alarm', fallback: 'Alarm' },
  Gateway: { id: 'auditLog.module.Gateway', fallback: 'Gateway' },
  OAuthClient: { id: 'auditLog.module.OAuthClient', fallback: 'OAuth Client' },
  MQTTAuth: { id: 'auditLog.module.MQTTAuth', fallback: 'Edge Connection' },
  Cluster: { id: 'auditLog.module.Cluster', fallback: 'Cluster' },
  Asset: { id: 'auditLog.module.Asset', fallback: 'Asset' },
};

const actionLabelMap: Record<string, { id: string; fallback: string }> = {
  SSHTerminalCreate: { id: 'auditLog.action.SSHTerminalCreate', fallback: 'Open terminal' },
  SSHTerminalClose: { id: 'auditLog.action.SSHTerminalClose', fallback: 'Close terminal' },
  SSHTerminalFinish: { id: 'auditLog.action.SSHTerminalFinish', fallback: 'Terminal session ended' },
  View: { id: 'auditLog.action.View', fallback: 'View' },
  Rollback: { id: 'auditLog.action.Rollback', fallback: 'Rollback' },
  SaveAs: { id: 'auditLog.action.SaveAs', fallback: 'Save As' },
  MoveToRecycleBin: { id: 'auditLog.action.MoveToRecycleBin', fallback: 'Move to Recycle Bin' },
  Invite: { id: 'auditLog.action.Invite', fallback: 'Invite' },
  RemoveMember: { id: 'auditLog.action.RemoveMember', fallback: 'Remove Member' },
  ChangeRole: { id: 'auditLog.action.ChangeRole', fallback: 'Change Role' },
  Create: { id: 'auditLog.action.Create', fallback: 'Create' },
  Update: { id: 'auditLog.action.Update', fallback: 'Update' },
  Delete: { id: 'auditLog.action.Delete', fallback: 'Delete' },
  Enable: { id: 'auditLog.action.Enable', fallback: 'Enable' },
  Disable: { id: 'auditLog.action.Disable', fallback: 'Disable' },
  Import: { id: 'auditLog.action.Import', fallback: 'Import' },
  Export: { id: 'auditLog.action.Export', fallback: 'Export' },
  Start: { id: 'auditLog.action.Start', fallback: 'Start' },
  Stop: { id: 'auditLog.action.Stop', fallback: 'Stop' },
  Login: { id: 'auditLog.action.Login', fallback: 'Login' },
  Logout: { id: 'auditLog.action.Logout', fallback: 'Logout' },
  ResetPassword: { id: 'auditLog.action.ResetPassword', fallback: 'Reset Password' },
  AssignRole: { id: 'auditLog.action.AssignRole', fallback: 'Assign Role' },
  Replace: { id: 'auditLog.action.Replace', fallback: 'Replace' },
  Activate: { id: 'auditLog.action.Activate', fallback: 'Activate' },
  Install: { id: 'auditLog.action.Install', fallback: 'Install' },
  Uninstall: { id: 'auditLog.action.Uninstall', fallback: 'Uninstall' },
  Save: { id: 'auditLog.action.Save', fallback: 'Save' },
  Copy: { id: 'auditLog.action.Copy', fallback: 'Copy' },
  Deploy: { id: 'auditLog.action.Deploy', fallback: 'Deploy' },
  Mark: { id: 'auditLog.action.Mark', fallback: 'Mark' },
  Unmark: { id: 'auditLog.action.Unmark', fallback: 'Unmark' },
  BindUNS: { id: 'auditLog.action.BindUNS', fallback: 'Bind UNS' },
  Confirm: { id: 'auditLog.action.Confirm', fallback: 'Confirm' },
  Restore: { id: 'auditLog.action.Restore', fallback: 'Restore' },
};

const resourceLabelMap: Record<string, { id: string; fallback: string }> = {
  AuditLog: { id: 'auditLog.resource.AuditLog', fallback: 'Audit Log' },
  Auth: { id: 'auditLog.resource.Auth', fallback: 'Authentication' },
  UserManagement: { id: 'auditLog.resource.UserManagement', fallback: 'User Management' },
  Role: { id: 'auditLog.resource.Role', fallback: 'Role' },
  Project: { id: 'auditLog.resource.Project', fallback: 'System' },
  ProjectMember: { id: 'auditLog.resource.ProjectMember', fallback: 'Project Member' },
  ProjectApp: { id: 'auditLog.resource.ProjectApp', fallback: 'Project App' },
  UNS: { id: 'auditLog.resource.UNS', fallback: 'UNS' },
  Resource: { id: 'auditLog.resource.Resource', fallback: 'Resource' },
  License: { id: 'auditLog.resource.License', fallback: 'License' },
  App: { id: 'auditLog.resource.App', fallback: 'App' },
  Template: { id: 'auditLog.resource.Template', fallback: 'Template' },
  Label: { id: 'auditLog.resource.Label', fallback: 'Label' },
  Group: { id: 'auditLog.resource.Group', fallback: 'Group' },
  Dashboard: { id: 'auditLog.resource.Dashboard', fallback: 'Dashboard' },
  AppKey: { id: 'auditLog.resource.AppKey', fallback: 'App Key' },
  SourceFlow: { id: 'auditLog.resource.SourceFlow', fallback: 'Collection Flow' },
  EventFlow: { id: 'auditLog.resource.EventFlow', fallback: 'Event Flow' },
  Notebook: { id: 'auditLog.resource.Notebook', fallback: 'Notebook' },
  AlarmRule: { id: 'auditLog.resource.AlarmRule', fallback: 'Alarm Rule' },
  Alarm: { id: 'auditLog.resource.Alarm', fallback: 'Alarm' },
  Gateway: { id: 'auditLog.resource.Gateway', fallback: 'Gateway' },
  OAuthClient: { id: 'auditLog.resource.OAuthClient', fallback: 'OAuth Client' },
  MQTTAuth: { id: 'auditLog.resource.MQTTAuth', fallback: 'Edge Connection' },
  Cluster: { id: 'auditLog.resource.Cluster', fallback: 'Cluster' },
  Asset: { id: 'auditLog.resource.Asset', fallback: 'Asset' },
  Platform: { id: 'auditLog.resource.Platform', fallback: 'System' },
};

const getMappedLabel = (
  raw: string | undefined,
  t: Translate,
  mapping: Record<string, { id: string; fallback: string }>
) => {
  const value = raw?.trim() || '';
  if (!value) {
    return '';
  }
  const mapped = mapping[value];
  if (!mapped) {
    return value;
  }
  return t(mapped.id, {}, mapped.fallback);
};

export const getAuditScopeLabel = (record: Pick<AuditLogItem, 'scopeType' | 'scopeName'>, t: Translate) => {
  const scopeName = record.scopeName?.trim() || '';
  switch (record.scopeType) {
    case 2:
      if (scopeName) {
        return t('auditLog.scope.projectWithName', { name: scopeName }, `System: ${scopeName}`);
      }
      return t('auditLog.scope.project', {}, 'System');
    default:
      return t('auditLog.notRecorded');
    case 1:
      if (scopeName) {
        return scopeName;
      }
      return t('auditLog.scope.platform', {}, 'System');
  }
};

export const getAuditModuleLabel = (
  _scopeType: number | undefined,
  _scopeName: string | undefined,
  resType: string | undefined,
  t: Translate
) => {
  return getMappedLabel(resType, t, moduleLabelMap) || t('auditLog.unknown', {}, 'Unknown');
};

export const getAuditActionLabel = (businessType: string | undefined, t: Translate) =>
  getMappedLabel(businessType, t, actionLabelMap) || t('auditLog.unknown', {}, 'Unknown');

const getUnsPathFromDetail = (detailJson?: string) => {
  if (!detailJson?.trim()) {
    return '';
  }
  try {
    const detail = JSON.parse(detailJson) as {
      after?: { path?: string };
      before?: { path?: string };
    };
    return detail.after?.path?.trim() || detail.before?.path?.trim() || '';
  } catch {
    return '';
  }
};

export const getAuditResourceLabel = (
  record: Pick<AuditLogItem, 'resType' | 'resName' | 'detailJson' | 'resourcePath'>,
  t: Translate
) => {
  if (record.resourcePath) return record.resourcePath;
  const unsPath = record.resType === 'UNS' ? getUnsPathFromDetail(record.detailJson) : '';
  if (unsPath) {
    return unsPath;
  }
  const resName = record.resName?.trim() || '';
  if (!resName) {
    return '-';
  }
  return getMappedLabel(resName, t, resourceLabelMap) || resName;
};

export const getAuditOperatorLabel = (record: Pick<AuditLogItem, 'operatorName' | 'operatorEmail' | 'operatorId'>) =>
  record.operatorName?.trim() || record.operatorEmail?.trim() || record.operatorId?.trim() || '-';

export const getAuditResult = (record: Pick<AuditLogItem, 'code' | 'result'>) =>
  record.result || (record.code === 202 ? 'accepted' : record.code >= 200 && record.code < 400 ? 'success' : 'failed');
export const getAuditResultLabel = (record: Pick<AuditLogItem, 'code' | 'result'>, t: Translate) =>
  t('auditLog.result.' + getAuditResult(record));
