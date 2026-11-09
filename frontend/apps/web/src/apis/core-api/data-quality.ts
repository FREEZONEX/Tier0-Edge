import { CustomAxiosConfigEnum } from '@/utils/request';
import { coreApi } from './core-adapter';

export type DataQualityRuleCode =
  | 'duplicate'
  | 'lateArrival'
  | 'timestampGap'
  | 'outOfOrder'
  | 'missingTelemetry'
  | 'ingestionDrop'
  | 'flatSignal'
  | 'frequencyDeviation'
  | 'mandatoryFieldMissing'
  | 'invalidValue'
  | 'valueRange'
  | 'collectionInterrupted'
  | 'connectorFailure';

export type PersistedDataQualityRuleCode = DataQualityRuleCode | 'staleSignal';

export type DataQualityIssueState = '' | 'open' | 'ack' | 'resolved';

export interface DataQualityConnectorSignal {
  source: 'mqtt' | 'flow';
  state: 'connected' | 'lost' | 'deployed' | 'disabled' | 'error';
  observedAt: string;
  errorCode?: string;
  flowId?: number;
}

export interface DataQualityPolicy {
  unsId: number;
  enabled: boolean;
  notifyOnIssue: boolean;
  expectedIntervalMs: number;
  sequenceField: string;
  missingGraceMs: number;
  lateThresholdMs: number;
  clockSkewToleranceMs: number;
  gapToleranceMs: number;
  outOfOrderToleranceMs: number;
  frequencyTolerancePercent: number;
  minSamples: number;
  flatMinSamples: number;
  flatMinDurationMs: number;
  mandatoryFields: string[];
  flatFields: string[];
  staleFields: string[];
  rules: PersistedDataQualityRuleCode[];
  severityOverrides: Record<string, string>;
  samplingPriority: number;
  cooldownMs: number;
}

export type SaveDataQualityPolicyInput = Omit<DataQualityPolicy, 'unsId'>;

export interface DataQualityNodeStatus {
  unsId: number;
  nodeName?: string;
  displayName?: string;
  namespace?: string;
  phase: 'warming' | 'healthy' | 'degraded';
  lastIngestedAt?: string;
  maxSourceTime?: string;
  receivedCount: number;
  persistedCount: number;
  dropCount: number;
  openRules: PersistedDataQualityRuleCode[];
  deepSampleSize: number;
  deepSampleTarget: number;
  deepState: 'inactive' | 'waiting' | 'running' | 'completed' | 'insufficientEvidence';
  deepLastResult?: 'completed' | 'insufficientEvidence';
  deepReason?: string;
  deepStartedAt?: string;
  deepCompletedAt?: string;
  deepMaxCollectWindowMs: number;
  insufficientEvidence?: Partial<Record<PersistedDataQualityRuleCode, string>>;
  collectionState: 'unknown' | 'suspected' | 'observed' | 'interrupted';
  connectorSignals: DataQualityConnectorSignal[];
}

export interface DataQualitySummary {
  monitoredNodes: number;
  healthyNodes: number;
  abnormalNodes: number;
  warmingNodes: number;
  receivedCount: number;
  persistedCount: number;
  dropCount: number;
  ingestionSuccessPercent: number;
  openIssues: number;
  ackIssues: number;
  criticalIssues: number;
}

export interface DataQualityStatus {
  enabled: boolean;
  items: DataQualityNodeStatus[];
  evaluationDrops: number;
  connectorEventDrops: number;
  summary?: DataQualitySummary;
  activeRuleCounts?: Record<string, number>;
}

export interface DataQualityIssue {
  id: number;
  unsId: number;
  nodeName?: string;
  displayName?: string;
  namespace?: string;
  ruleCode: PersistedDataQualityRuleCode;
  severity: 'info' | 'warning' | 'critical';
  state: Exclude<DataQualityIssueState, ''>;
  firstObservedAt: string;
  lastObservedAt: string;
  occurrences: number;
  summary: string;
  evidence?: Record<string, unknown>;
  acknowledgedBy: number;
  acknowledgedAt?: string;
  resolvedAt?: string;
}

export interface DataQualityIssuePage {
  list: DataQualityIssue[];
  total: number;
  page: number;
  size: number;
}

export interface DataQualityActiveSummaryItem {
  unsId: number;
  namespace: string;
  severity: DataQualityIssue['severity'];
}

export interface DataQualityActiveSummary {
  list: DataQualityActiveSummaryItem[];
}

const silentConfig = { [CustomAxiosConfigEnum.NoMessage]: true };

export const getDataQualityStatus = (unsId: string | number) =>
  coreApi.get('/uns/data-quality/status', { params: { unsId }, ...silentConfig }) as Promise<DataQualityStatus>;

export const getDataQualityPolicy = (unsId: string | number) =>
  coreApi.get(`/uns/data-quality/policies/${unsId}`, silentConfig) as Promise<DataQualityPolicy>;

export const saveDataQualityPolicy = (unsId: string | number, data: SaveDataQualityPolicyInput) =>
  coreApi.put(`/uns/data-quality/policies/${unsId}`, data) as Promise<DataQualityPolicy>;

export const listDataQualityIssues = (params: {
  unsId?: string | number;
  state?: DataQualityIssueState;
  ruleCode?: PersistedDataQualityRuleCode | '';
  severity?: DataQualityIssue['severity'] | '';
  page: number;
  size: number;
}) => coreApi.get('/uns/data-quality/issues', { params, ...silentConfig }) as Promise<DataQualityIssuePage>;

export const getDataQualityActiveSummary = () =>
  coreApi.get('/uns/data-quality/active-summary', silentConfig) as Promise<DataQualityActiveSummary>;

export const acknowledgeDataQualityIssue = (id: number) =>
  coreApi.post(`/uns/data-quality/issues/${id}/ack`, undefined, silentConfig) as Promise<DataQualityIssue>;

export const requestDataQualityDeepRun = (unsId: string | number) =>
  coreApi.post(`/uns/data-quality/runs/${unsId}`, undefined, silentConfig) as Promise<
    Pick<DataQualityNodeStatus, 'unsId' | 'deepState'>
  >;
