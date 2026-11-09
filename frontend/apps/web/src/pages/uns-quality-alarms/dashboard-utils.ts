import type {
  DataQualityIssue,
  DataQualityNodeStatus,
  DataQualityStatus,
  DataQualitySummary,
} from '@/apis/core-api/data-quality';
import { formatTimestamp } from '@/utils/format';

export type Translate = (id: string, values?: Record<string, string | number>, defaultMessage?: string) => string;

export type NodeHealthState = 'healthy' | 'warning' | 'critical' | 'warming';

export const nodeLabel = (node: Pick<DataQualityNodeStatus, 'unsId' | 'displayName' | 'nodeName' | 'namespace'>) =>
  node.displayName || node.nodeName || node.namespace || `#${node.unsId}`;

export const issueNodeLabel = (issue: DataQualityIssue) =>
  issue.displayName || issue.nodeName || issue.namespace || `#${issue.unsId}`;

export const nodeHealthState = (node: DataQualityNodeStatus): NodeHealthState => {
  if (node.phase === 'warming') return 'warming';
  if (node.phase === 'healthy') return 'healthy';
  if (
    node.collectionState === 'interrupted' ||
    node.connectorSignals?.some((signal) => signal.state === 'error' || signal.state === 'lost')
  ) {
    return 'critical';
  }
  return 'warning';
};

export const nodeIngestionSuccess = (node?: DataQualityNodeStatus) => {
  if (!node || node.receivedCount <= 0) return undefined;
  return Math.max(0, Math.min(100, (node.persistedCount / node.receivedCount) * 100));
};

export const fallbackSummary = (status?: DataQualityStatus): DataQualitySummary => {
  const items = status?.items || [];
  const receivedCount = items.reduce((sum, item) => sum + item.receivedCount, 0);
  const persistedCount = items.reduce((sum, item) => sum + item.persistedCount, 0);
  const dropCount = items.reduce((sum, item) => sum + item.dropCount, 0);
  return {
    monitoredNodes: items.length,
    healthyNodes: items.filter((item) => item.phase === 'healthy').length,
    abnormalNodes: items.filter((item) => item.phase === 'degraded').length,
    warmingNodes: items.filter((item) => item.phase === 'warming').length,
    receivedCount,
    persistedCount,
    dropCount,
    ingestionSuccessPercent: receivedCount > 0 ? (persistedCount / receivedCount) * 100 : 0,
    openIssues: 0,
    ackIssues: 0,
    criticalIssues: 0,
  };
};

export const formatCount = (value?: number) => Number(value || 0).toLocaleString();

export const formatPercent = (value?: number) => `${Number(value || 0).toFixed(1)}%`;

export const formatDuration = (start: string, end: string | undefined, t: Translate) => {
  const startAt = new Date(start).getTime();
  const endAt = end ? new Date(end).getTime() : Date.now();
  if (!Number.isFinite(startAt) || !Number.isFinite(endAt)) return '-';
  const totalMinutes = Math.max(0, Math.floor((endAt - startAt) / 60_000));
  const days = Math.floor(totalMinutes / 1440);
  const hours = Math.floor((totalMinutes % 1440) / 60);
  const minutes = totalMinutes % 60;
  if (days > 0) return t('dataQuality.duration.days', { days, hours });
  if (hours > 0) return t('dataQuality.duration.hours', { hours, minutes });
  return t('dataQuality.duration.minutes', { minutes });
};

const timestampFields = new Set([
  'sourceTimestamp',
  'ingestedAt',
  'previousSourceTimestamp',
  'maxSourceTimestamp',
  'lastSeenAt',
  'deadlineAt',
  'observedAt',
  'firstSeenAt',
  'duplicateSeenAt',
  'windowStartSourceTimestamp',
  'windowEndSourceTimestamp',
]);

const millisecondFields = new Set([
  'delayMs',
  'skewToleranceMs',
  'deltaMs',
  'gapMs',
  'expectedMs',
  'durationMs',
  'observedMs',
  'lateThresholdMs',
  'allowedDelayMs',
  'expectedIntervalMs',
  'meanObservedIntervalMs',
  'gapToleranceMs',
  'allowedGapMs',
  'behindMs',
  'outOfOrderToleranceMs',
  'missingGraceMs',
  'allowedSilenceMs',
  'minimumDurationMs',
  'meanObservedMs',
  'minimumObservedMs',
  'maximumObservedMs',
  'allowedMinimumMs',
  'allowedMaximumMs',
]);

const percentFields = new Set(['deviationPercent', 'tolerancePercent']);

const formatEvidenceValue = (key: string, value: unknown, t: Translate) => {
  if (value == null || value === '') return '-';
  if (timestampFields.has(key)) {
    const timestamp = typeof value === 'number' ? value : Number(value);
    return Number.isFinite(timestamp) ? formatTimestamp(timestamp) || '-' : String(value);
  }
  if (millisecondFields.has(key)) return t('dataQuality.evidenceValue.ms', { value: String(value) });
  if (percentFields.has(key)) return t('dataQuality.evidenceValue.percent', { value: String(value) });
  if (key === 'source') return t(`dataQuality.connectorSource.${String(value)}`, undefined, String(value));
  if (key === 'state') return t(`dataQuality.connectorState.${String(value)}`, undefined, String(value));
  if (key === 'stage' || key === 'reason' || key === 'direction') {
    return t(`dataQuality.evidenceValue.${String(value)}`, undefined, String(value));
  }
  if (key === 'truncated' && value === true) return t('dataQuality.evidenceTruncated');
  if (typeof value === 'boolean') return t(value ? 'common.yes' : 'common.no');
  if (Array.isArray(value)) return value.map(String).join(', ') || '-';
  if (typeof value === 'object') return JSON.stringify(value);
  return String(value);
};

export const evidenceEntries = (issue: DataQualityIssue, t: Translate) =>
  Object.entries(issue.evidence || {})
    .filter(([key]) => !/(payloadPreview|examples|fieldDetails|countsByField|violations)/i.test(key))
    .map(([key, value]) => ({
      key,
      label: t(`dataQuality.evidenceField.${key}`, undefined, key),
      value: formatEvidenceValue(key, value, t),
    }));

export interface IssueDiagnosisMetric {
  key: string;
  label: string;
  value: string;
  tone?: 'normal' | 'danger' | 'warning';
}

export interface IssuePayloadPreview {
  key: string;
  title: string;
  values: Record<string, unknown>;
  fieldCount?: number;
  truncated?: boolean;
}

export interface IssueDiagnosis {
  summary: string;
  formula?: string;
  metrics: IssueDiagnosisMetric[];
  payloads: IssuePayloadPreview[];
  comparison?: IssueDecisionComparison;
  isLegacyEvidence: boolean;
}

export interface IssueDecisionComparison {
  observedLabel: string;
  observedValue: number;
  observedText: string;
  thresholdLabel: string;
  thresholdValue: number;
  thresholdText: string;
}

export interface IssueCausalStep {
  key: string;
  title: string;
  description: string;
  payload?: IssuePayloadPreview;
}

export interface IssueTimelinePoint {
  key: string;
  title: string;
  time: string;
  description: string;
  tone?: 'danger' | 'warning' | 'success';
}

const evidenceNumber = (evidence: Record<string, unknown>, key: string) => {
  const value = Number(evidence[key]);
  return Number.isFinite(value) ? value : undefined;
};

const evidenceStrings = (evidence: Record<string, unknown>, key: string) => {
  const value = evidence[key];
  return Array.isArray(value) ? value.map(String) : [];
};

const asPayload = (value: unknown): Record<string, unknown> | undefined => {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return undefined;
  return value as Record<string, unknown>;
};

export const formatEvidenceDuration = (value?: number) => {
  if (value == null || !Number.isFinite(value)) return '-';
  const absolute = Math.abs(value);
  if (absolute < 1000) return `${Math.round(value)} ms`;
  if (absolute < 60_000) return `${(value / 1000).toFixed(absolute < 10_000 ? 1 : 0)} s`;
  if (absolute < 3_600_000) return `${(value / 60_000).toFixed(1)} min`;
  return `${(value / 3_600_000).toFixed(1)} h`;
};

const metric = (
  key: string,
  label: string,
  value: string,
  tone: IssueDiagnosisMetric['tone'] = 'normal'
): IssueDiagnosisMetric => ({ key, label, value, tone });

const payloadFromKey = (
  evidence: Record<string, unknown>,
  key: string,
  title: string
): IssuePayloadPreview | undefined => {
  const values = asPayload(evidence[key]);
  if (!values || Object.keys(values).length === 0) return undefined;
  return {
    key,
    title,
    values,
    fieldCount: evidenceNumber(evidence, `${key}FieldCount`),
    truncated: evidence[`${key}Truncated`] === true,
  };
};

const collectPayloads = (evidence: Record<string, unknown>, t: Translate) => {
  const payloads = [
    payloadFromKey(evidence, 'referencePayloadPreview', t('dataQuality.diagnosis.referencePayload')),
    payloadFromKey(evidence, 'matchedPayloadPreview', t('dataQuality.diagnosis.matchedPayload')),
    payloadFromKey(evidence, 'lastPayloadPreview', t('dataQuality.diagnosis.lastPayload')),
    payloadFromKey(evidence, 'currentPayloadPreview', t('dataQuality.diagnosis.currentPayload')),
    payloadFromKey(evidence, 'payloadPreview', t('dataQuality.diagnosis.samplePayload')),
  ].filter(Boolean) as IssuePayloadPreview[];

  const examples = Array.isArray(evidence.missingExamples) ? evidence.missingExamples : [];
  examples.slice(0, 3).forEach((raw, index) => {
    const example = asPayload(raw);
    const values = asPayload(example?.payloadPreview);
    if (!values) return;
    const missingFields = Array.isArray(example?.missingFields) ? example.missingFields.map(String).join(', ') : '-';
    payloads.push({
      key: `missingExample${index}`,
      title: t('dataQuality.diagnosis.missingPayload', { index: index + 1, fields: missingFields }),
      values,
      fieldCount: evidenceNumber(example || {}, 'payloadPreviewFieldCount'),
      truncated: example?.payloadPreviewTruncated === true,
    });
  });

  const invalidExamples = asPayload(evidence.invalidExamples);
  if (invalidExamples && Object.keys(invalidExamples).length > 0) {
    payloads.push({
      key: 'invalidExamples',
      title: t('dataQuality.diagnosis.invalidPayload'),
      values: invalidExamples,
      fieldCount: Object.keys(invalidExamples).length,
    });
  }

  const flatDetails = Array.isArray(evidence.flatFieldDetails) ? evidence.flatFieldDetails : [];
  flatDetails.slice(0, 8).forEach((raw, index) => {
    const detail = asPayload(raw);
    if (!detail) return;
    const field = String(detail.field || `#${index + 1}`);
    payloads.push({
      key: `flatField${index}`,
      title: t('dataQuality.diagnosis.flatPayload', { field }),
      values: {
        [field]: detail.valuePreview ?? '-',
        [t('dataQuality.evidenceField.sampleCount')]: detail.sampleCount ?? '-',
        [t('dataQuality.evidenceField.durationMs')]: formatEvidenceDuration(Number(detail.durationMs) || undefined),
      },
      fieldCount: 3,
    });
  });
  return payloads;
};

export const buildIssueDiagnosis = (issue: DataQualityIssue, t: Translate): IssueDiagnosis => {
  const evidence = issue.evidence || {};
  const payloads = collectPayloads(evidence, t);
  const fallback = t(`dataQuality.ruleHelp.${issue.ruleCode}`, undefined, issue.summary || issue.ruleCode);
  let summary = fallback;
  let formula: string | undefined;
  let metrics: IssueDiagnosisMetric[] = [];
  let comparison: IssueDecisionComparison | undefined;
  let hasDecisionEvidence = false;
  const ms = (key: string) => evidenceNumber(evidence, key);

  switch (issue.ruleCode) {
    case 'lateArrival': {
      const delay = ms('delayMs');
      const allowed = ms('allowedDelayMs');
      const expected = ms('expectedIntervalMs');
      const meanObserved = ms('meanObservedIntervalMs');
      if (delay != null && allowed != null && expected != null && meanObserved != null) {
        const excess = Math.max(0, delay - allowed);
        summary = t('dataQuality.diagnosis.lateArrival', {
          delay: formatEvidenceDuration(delay),
          allowed: formatEvidenceDuration(allowed),
          excess: formatEvidenceDuration(excess),
          mean: formatEvidenceDuration(meanObserved),
        });
        formula = t('dataQuality.diagnosis.lateFormula');
        metrics = [
          metric('delay', t('dataQuality.diagnosis.observedDelay'), formatEvidenceDuration(delay), 'danger'),
          metric('threshold', t('dataQuality.diagnosis.allowedDelay'), formatEvidenceDuration(allowed)),
          metric('excess', t('dataQuality.diagnosis.excess'), formatEvidenceDuration(excess), 'warning'),
          metric('mean', t('dataQuality.diagnosis.meanInterval'), formatEvidenceDuration(meanObserved)),
          metric('expected', t('dataQuality.diagnosis.expectedInterval'), formatEvidenceDuration(expected)),
        ];
        comparison = {
          observedLabel: t('dataQuality.diagnosis.observedDelay'),
          observedValue: delay,
          observedText: formatEvidenceDuration(delay),
          thresholdLabel: t('dataQuality.diagnosis.allowedDelay'),
          thresholdValue: allowed,
          thresholdText: formatEvidenceDuration(allowed),
        };
        hasDecisionEvidence = true;
      }
      break;
    }
    case 'timestampGap': {
      const gap = ms('gapMs');
      const allowed = ms('allowedGapMs');
      const gapTolerance = ms('gapToleranceMs');
      if (gap != null && allowed != null && allowed > 0 && gapTolerance != null) {
        const excess = Math.max(0, gap - allowed);
        summary = t('dataQuality.diagnosis.timestampGap', {
          gap: formatEvidenceDuration(gap),
          allowed: formatEvidenceDuration(allowed),
          excess: formatEvidenceDuration(excess),
        });
        formula = t('dataQuality.diagnosis.gapFormula');
        metrics = [
          metric('gap', t('dataQuality.diagnosis.observedGap'), formatEvidenceDuration(gap), 'danger'),
          metric('expected', t('dataQuality.diagnosis.expectedInterval'), formatEvidenceDuration(ms('expectedMs'))),
          metric('tolerance', t('dataQuality.diagnosis.tolerance'), formatEvidenceDuration(gapTolerance)),
          metric('excess', t('dataQuality.diagnosis.excess'), formatEvidenceDuration(excess), 'warning'),
        ];
        comparison = {
          observedLabel: t('dataQuality.diagnosis.observedGap'),
          observedValue: gap,
          observedText: formatEvidenceDuration(gap),
          thresholdLabel: t('dataQuality.diagnosis.allowedGap'),
          thresholdValue: allowed,
          thresholdText: formatEvidenceDuration(allowed),
        };
        hasDecisionEvidence = true;
      }
      break;
    }
    case 'outOfOrder': {
      const behind = ms('behindMs') ?? Math.abs(Math.min(0, ms('deltaMs') || 0));
      const tolerance = ms('outOfOrderToleranceMs');
      if (behind > 0 && tolerance != null) {
        summary = t('dataQuality.diagnosis.outOfOrder', {
          behind: formatEvidenceDuration(behind),
          tolerance: formatEvidenceDuration(tolerance),
        });
        formula = t('dataQuality.diagnosis.orderFormula');
        metrics = [
          metric('behind', t('dataQuality.diagnosis.behindMaximum'), formatEvidenceDuration(behind), 'danger'),
          metric('tolerance', t('dataQuality.diagnosis.allowedOutOfOrder'), formatEvidenceDuration(tolerance)),
        ];
        comparison = {
          observedLabel: t('dataQuality.diagnosis.behindMaximum'),
          observedValue: behind,
          observedText: formatEvidenceDuration(behind),
          thresholdLabel: t('dataQuality.diagnosis.allowedOutOfOrder'),
          thresholdValue: tolerance,
          thresholdText: formatEvidenceDuration(tolerance),
        };
        hasDecisionEvidence = true;
      }
      break;
    }
    case 'missingTelemetry':
    case 'staleSignal': {
      const lastSeenAt = ms('lastSeenAt');
      const capturedSilence = ms('delayMs');
      const liveSilence =
        issue.state !== 'resolved' && lastSeenAt != null ? Math.max(0, Date.now() - lastSeenAt) : capturedSilence;
      const silence = Math.max(capturedSilence || 0, liveSilence || 0);
      const allowed = ms('allowedSilenceMs');
      const expected = ms('expectedIntervalMs');
      const grace = ms('missingGraceMs');
      if (capturedSilence != null && allowed != null && expected != null && grace != null) {
        summary = t('dataQuality.diagnosis.missingTelemetry', {
          silence: formatEvidenceDuration(silence),
          expected: formatEvidenceDuration(expected),
          grace: formatEvidenceDuration(grace),
        });
        formula = t('dataQuality.diagnosis.missingFormula');
        metrics = [
          metric('silence', t('dataQuality.diagnosis.silenceDuration'), formatEvidenceDuration(silence), 'danger'),
          metric('allowed', t('dataQuality.diagnosis.allowedSilence'), formatEvidenceDuration(allowed)),
          metric(
            'lastSeen',
            t('dataQuality.evidenceField.lastSeenAt'),
            formatEvidenceValue('lastSeenAt', evidence.lastSeenAt, t)
          ),
        ];
        comparison = {
          observedLabel: t('dataQuality.diagnosis.silenceDuration'),
          observedValue: silence,
          observedText: formatEvidenceDuration(silence),
          thresholdLabel: t('dataQuality.diagnosis.allowedSilence'),
          thresholdValue: allowed,
          thresholdText: formatEvidenceDuration(allowed),
        };
        hasDecisionEvidence = true;
      }
      break;
    }
    case 'frequencyDeviation': {
      const median = ms('observedMs');
      const mean = ms('meanObservedMs');
      const expected = ms('expectedMs');
      const deviation = ms('deviationPercent');
      const tolerance = ms('tolerancePercent');
      const allowedMinimum = ms('allowedMinimumMs');
      const allowedMaximum = ms('allowedMaximumMs');
      if (
        median != null &&
        mean != null &&
        expected != null &&
        deviation != null &&
        tolerance != null &&
        allowedMinimum != null &&
        allowedMaximum != null
      ) {
        summary = t('dataQuality.diagnosis.frequencyDeviation', {
          median: formatEvidenceDuration(median),
          mean: formatEvidenceDuration(mean),
          expected: formatEvidenceDuration(expected),
          deviation,
          tolerance,
        });
        formula = t('dataQuality.diagnosis.frequencyFormula');
        metrics = [
          metric('median', t('dataQuality.diagnosis.medianInterval'), formatEvidenceDuration(median), 'danger'),
          metric('mean', t('dataQuality.diagnosis.meanInterval'), formatEvidenceDuration(mean)),
          metric('expected', t('dataQuality.diagnosis.expectedInterval'), formatEvidenceDuration(expected)),
          metric(
            'range',
            t('dataQuality.diagnosis.allowedRange'),
            `${formatEvidenceDuration(allowedMinimum)} – ${formatEvidenceDuration(allowedMaximum)}`
          ),
        ];
        comparison = {
          observedLabel: t('dataQuality.evidenceField.deviationPercent'),
          observedValue: deviation,
          observedText: `${deviation}%`,
          thresholdLabel: t('dataQuality.evidenceField.tolerancePercent'),
          thresholdValue: tolerance,
          thresholdText: `${tolerance}%`,
        };
        hasDecisionEvidence = true;
      }
      break;
    }
    case 'mandatoryFieldMissing': {
      const fields = evidenceStrings(evidence, 'fields');
      const count = ms('count');
      const samples = ms('sampleCount');
      if (fields.length > 0) {
        summary = t('dataQuality.diagnosis.mandatoryMissing', {
          fields: fields.join(', '),
          count: count || 0,
          samples: samples || 0,
        });
        formula = t('dataQuality.diagnosis.mandatoryFormula');
        metrics = [
          metric('fields', t('dataQuality.diagnosis.missingFields'), fields.join(', '), 'danger'),
          metric('count', t('dataQuality.diagnosis.missingOccurrences'), String(count || 0)),
          metric('samples', t('dataQuality.evidenceField.sampleCount'), String(samples || 0)),
        ];
        hasDecisionEvidence = true;
      }
      break;
    }
    case 'flatSignal': {
      const fields = evidenceStrings(evidence, 'fields');
      const duration = ms('durationMs');
      const minimum = ms('minimumDurationMs');
      if (fields.length > 0 && duration != null && minimum != null) {
        summary = t('dataQuality.diagnosis.flatSignal', {
          fields: fields.join(', '),
          duration: formatEvidenceDuration(duration),
          minimum: formatEvidenceDuration(minimum),
        });
        formula = t('dataQuality.diagnosis.flatFormula');
        metrics = [
          metric('fields', t('dataQuality.evidenceField.fields'), fields.join(', '), 'danger'),
          metric('duration', t('dataQuality.evidenceField.durationMs'), formatEvidenceDuration(duration)),
          metric('minimum', t('dataQuality.diagnosis.minimumDuration'), formatEvidenceDuration(minimum)),
          metric('samples', t('dataQuality.evidenceField.sampleCount'), String(ms('sampleCount') || 0)),
        ];
        comparison = {
          observedLabel: t('dataQuality.evidenceField.durationMs'),
          observedValue: duration,
          observedText: formatEvidenceDuration(duration),
          thresholdLabel: t('dataQuality.diagnosis.minimumDuration'),
          thresholdValue: minimum,
          thresholdText: formatEvidenceDuration(minimum),
        };
        hasDecisionEvidence = true;
      }
      break;
    }
    case 'valueRange': {
      const field = String(evidence.field || evidenceStrings(evidence, 'fields')[0] || '-');
      const observed = ms('observedValue');
      const lower = ms('lowerLimit');
      const upper = ms('upperLimit');
      const direction = String(evidence.direction || '');
      const directionLabel = formatEvidenceValue('direction', direction, t);
      const threshold = direction === 'belowLower' ? lower : direction === 'aboveUpper' ? upper : undefined;
      if (observed != null && threshold != null && direction) {
        summary = t('dataQuality.diagnosis.valueRange', {
          field,
          value: String(observed),
          direction: directionLabel,
          threshold: String(threshold),
        });
        formula = t('dataQuality.diagnosis.rangeFormula');
        metrics = [
          metric('field', t('dataQuality.diagnosis.rangeField'), field),
          metric('observed', t('dataQuality.diagnosis.observedValue'), String(observed), 'danger'),
          metric('lower', t('dataQuality.diagnosis.rangeLower'), lower == null ? '-' : String(lower)),
          metric('upper', t('dataQuality.diagnosis.rangeUpper'), upper == null ? '-' : String(upper)),
          metric('direction', t('dataQuality.evidenceField.direction'), directionLabel, 'warning'),
        ];
        hasDecisionEvidence = true;
      }
      break;
    }
    case 'invalidValue': {
      const fields = evidenceStrings(evidence, 'fields');
      const codes = evidenceStrings(evidence, 'errorCodes');
      if (fields.length > 0 || codes.length > 0) {
        summary = t('dataQuality.diagnosis.invalidValue', {
          fields: fields.join(', ') || '-',
          codes: codes.join(', ') || '-',
          count: ms('count') || 0,
        });
        metrics = [
          metric('fields', t('dataQuality.evidenceField.fields'), fields.join(', ') || '-', 'danger'),
          metric('codes', t('dataQuality.evidenceField.errorCodes'), codes.join(', ') || '-'),
          metric('count', t('dataQuality.evidenceField.count'), String(ms('count') || 0)),
        ];
        hasDecisionEvidence = true;
      }
      break;
    }
    case 'duplicate': {
      if (evidence.firstSeenAt != null) {
        summary = t('dataQuality.diagnosis.duplicate', {
          first: formatEvidenceValue('firstSeenAt', evidence.firstSeenAt, t),
          current: formatEvidenceValue('duplicateSeenAt', evidence.duplicateSeenAt, t),
        });
        formula = t('dataQuality.diagnosis.duplicateFormula');
        metrics = [
          metric('identity', t('dataQuality.evidenceField.sourceIdentity'), String(evidence.sourceIdentity || '-')),
          metric('hash', t('dataQuality.evidenceField.canonicalHash'), String(evidence.canonicalHash || '-')),
        ];
        hasDecisionEvidence = true;
      }
      break;
    }
    case 'ingestionDrop': {
      if (evidence.stage != null || evidence.reason != null) {
        summary = t('dataQuality.diagnosis.ingestionDrop', {
          stage: formatEvidenceValue('stage', evidence.stage, t),
          reason: formatEvidenceValue('reason', evidence.reason, t),
          count: ms('count') || 0,
        });
        hasDecisionEvidence = true;
      }
      break;
    }
    case 'collectionInterrupted':
    case 'connectorFailure': {
      if (evidence.source != null && evidence.state != null) {
        summary = t('dataQuality.diagnosis.connector', {
          source: formatEvidenceValue('source', evidence.source, t),
          state: formatEvidenceValue('state', evidence.state, t),
          error: String(evidence.errorCode || '-'),
        });
        hasDecisionEvidence = true;
      }
      break;
    }
  }

  return { summary, formula, metrics, payloads, comparison, isLegacyEvidence: !hasDecisionEvidence };
};

const currentPayloadKeys = new Set([
  'currentPayloadPreview',
  'payloadPreview',
  'missingExample0',
  'invalidExamples',
  'flatField0',
]);

export const buildIssueCausalSteps = (
  issue: DataQualityIssue,
  diagnosis: IssueDiagnosis,
  t: Translate
): IssueCausalStep[] => {
  const currentPayload = diagnosis.payloads.find((payload) => currentPayloadKeys.has(payload.key));
  const referencePayload = diagnosis.payloads.find((payload) => payload.key !== currentPayload?.key);
  return [
    {
      key: 'reference',
      title: t('dataQuality.diagnosis.causalReference'),
      description: referencePayload?.title || t('dataQuality.diagnosis.causalReferenceDesc'),
      payload: referencePayload,
    },
    {
      key: 'current',
      title: t('dataQuality.diagnosis.causalCurrent'),
      description: currentPayload?.title || t('dataQuality.diagnosis.causalCurrentDesc'),
      payload: currentPayload,
    },
    {
      key: 'decision',
      title: t('dataQuality.diagnosis.causalDecision'),
      description: diagnosis.formula || diagnosis.summary,
    },
    {
      key: 'alarm',
      title: t('dataQuality.diagnosis.causalAlarm'),
      description: t('dataQuality.diagnosis.causalAlarmDesc', {
        id: issue.id,
        state: t(`dataQuality.state.${issue.state}`),
      }),
    },
  ];
};

const evidenceTime = (evidence: Record<string, unknown>, keys: string[]) => {
  for (const key of keys) {
    const value = evidence[key];
    const timestamp = typeof value === 'number' ? value : Number(value);
    if (Number.isFinite(timestamp) && timestamp > 0) return formatTimestamp(timestamp) || '-';
  }
  return undefined;
};

export const buildIssueTimeline = (
  issue: DataQualityIssue,
  diagnosis: IssueDiagnosis,
  t: Translate
): IssueTimelinePoint[] => {
  const evidence = issue.evidence || {};
  const referenceTime = evidenceTime(evidence, [
    'previousSourceTimestamp',
    'maxSourceTimestamp',
    'firstSeenAt',
    'lastSeenAt',
    'windowStartSourceTimestamp',
    'sourceTimestamp',
  ]);
  const currentTime = evidenceTime(evidence, [
    'duplicateSeenAt',
    'sourceTimestamp',
    'ingestedAt',
    'windowEndSourceTimestamp',
    'observedAt',
    'deadlineAt',
  ]);
  const points: IssueTimelinePoint[] = [];

  if (referenceTime) {
    points.push({
      key: 'reference',
      title: t('dataQuality.diagnosis.timelineReference'),
      time: referenceTime,
      description: t('dataQuality.diagnosis.timelineReferenceDesc'),
    });
  }
  if (currentTime && currentTime !== referenceTime) {
    points.push({
      key: 'current',
      title: t('dataQuality.diagnosis.timelineCurrent'),
      time: currentTime,
      description: diagnosis.summary,
      tone: 'danger',
    });
  }

  points.push({
    key: 'alarm',
    title: t('dataQuality.diagnosis.timelineAlarm'),
    time: formatTimestamp(issue.firstObservedAt) || '-',
    description: t('dataQuality.diagnosis.timelineAlarmDesc', { occurrences: issue.occurrences }),
    tone: issue.severity === 'critical' ? 'danger' : 'warning',
  });
  if (issue.acknowledgedAt) {
    points.push({
      key: 'ack',
      title: t('dataQuality.diagnosis.timelineAck'),
      time: formatTimestamp(issue.acknowledgedAt) || '-',
      description: t('dataQuality.diagnosis.timelineAckDesc'),
    });
  }
  if (issue.resolvedAt) {
    points.push({
      key: 'resolved',
      title: t('dataQuality.diagnosis.timelineResolved'),
      time: formatTimestamp(issue.resolvedAt) || '-',
      description: t('dataQuality.diagnosis.timelineResolvedDesc'),
      tone: 'success',
    });
  }
  return points;
};

export const serializeIssueEvidence = (issue: DataQualityIssue, diagnosis: IssueDiagnosis, t: Translate) =>
  JSON.stringify(
    {
      alarm: {
        id: issue.id,
        unsId: issue.unsId,
        node: issueNodeLabel(issue),
        namespace: issue.namespace,
        ruleCode: issue.ruleCode,
        severity: issue.severity,
        state: issue.state,
        firstObservedAt: issue.firstObservedAt,
        lastObservedAt: issue.lastObservedAt,
        acknowledgedAt: issue.acknowledgedAt,
        resolvedAt: issue.resolvedAt,
        occurrences: issue.occurrences,
      },
      decision: {
        summary: diagnosis.summary,
        formula: diagnosis.formula,
        metrics: diagnosis.metrics,
      },
      parameters: evidenceEntries(issue, t).map(({ label, value }) => ({ label, value })),
      sanitizedPayloadPreviews: diagnosis.payloads.map(({ title, values, fieldCount, truncated }) => ({
        title,
        values,
        fieldCount,
        truncated,
      })),
    },
    null,
    2
  );
