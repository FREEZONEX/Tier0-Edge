import { ArrowRight, Checkmark, ChevronLeft, ChevronRight, Copy, Launch, WarningAltFilled } from '@carbon/icons-react';
import { Descriptions, Grid, Progress, Tabs, Tag, Timeline } from 'antd';
import type { FC } from 'react';

import type { DataQualityIssue } from '@/apis/core-api/data-quality';
import ComButton from '@/components/com-button';
import ComDrawer from '@/components/com-drawer';
import HelpTooltip from '@/components/help-tooltip';
import { formatTimestamp } from '@/utils/format';

import {
  buildIssueCausalSteps,
  buildIssueDiagnosis,
  buildIssueTimeline,
  evidenceEntries,
  formatDuration,
  issueNodeLabel,
  serializeIssueEvidence,
  type Translate,
} from '../dashboard-utils';
import styles from '../index.module.scss';

interface IssueDetailDrawerProps {
  issue?: DataQualityIssue;
  canManage: boolean;
  onClose: () => void;
  onAcknowledge: (issue: DataQualityIssue) => Promise<void>;
  onCopyEvidence: (content: string) => void;
  onOpenUns: (issue: DataQualityIssue) => void;
  onPrevious?: () => void;
  onNext?: () => void;
  t: Translate;
}

const severityColor = (severity: DataQualityIssue['severity']) => {
  if (severity === 'critical') return 'error';
  if (severity === 'warning') return 'warning';
  return 'processing';
};

const stateColor = (state: DataQualityIssue['state']) => {
  if (state === 'resolved') return 'success';
  if (state === 'ack') return 'warning';
  return 'error';
};

const IssueDetailDrawer: FC<IssueDetailDrawerProps> = ({
  issue,
  canManage,
  onClose,
  onAcknowledge,
  onCopyEvidence,
  onOpenUns,
  onPrevious,
  onNext,
  t,
}) => {
  const screens = Grid.useBreakpoint();
  const evidence = issue ? evidenceEntries(issue, t) : [];
  const diagnosis = issue ? buildIssueDiagnosis(issue, t) : undefined;
  const causalSteps = issue && diagnosis ? buildIssueCausalSteps(issue, diagnosis, t) : [];
  const timeline = issue && diagnosis ? buildIssueTimeline(issue, diagnosis, t) : [];
  const comparisonMax = diagnosis?.comparison
    ? Math.max(diagnosis.comparison.observedValue, diagnosis.comparison.thresholdValue, 1)
    : 1;

  return (
    <ComDrawer
      open={Boolean(issue)}
      width={screens.md ? '82vw' : '96vw'}
      title={t('dataQuality.dashboard.issueDetail')}
      mask={false}
      destroyOnHidden
      rootClassName={styles['quality-detail-drawer']}
      onClose={onClose}
      footer={
        issue && diagnosis ? (
          <div className={styles['drawer-footer']}>
            <div className={styles['drawer-footer-help']}>
              {issue.state === 'open' && canManage && (
                <>
                  <span>{t('dataQuality.diagnosis.ackHint')}</span>
                  <HelpTooltip title={t('dataQuality.diagnosis.ackHelp')} />
                </>
              )}
            </div>
            <div className={styles['drawer-footer-actions']}>
              <ComButton onClick={() => onOpenUns(issue)}>
                <Launch size={16} />
                {t('dataQuality.dashboard.openUnsNode')}
              </ComButton>
              <ComButton onClick={() => onCopyEvidence(serializeIssueEvidence(issue, diagnosis, t))}>
                <Copy size={16} />
                {t('dataQuality.diagnosis.copyEvidence')}
              </ComButton>
              {canManage && issue.state === 'open' && (
                <ComButton type="primary" onClick={() => onAcknowledge(issue)}>
                  <Checkmark size={16} />
                  {t('dataQuality.diagnosis.ackAlarm')}
                </ComButton>
              )}
            </div>
          </div>
        ) : null
      }
    >
      {issue && diagnosis && (
        <div className={styles['drawer-content']}>
          <div className={styles['drawer-nav']}>
            <span>{t('dataQuality.diagnosis.issueNumber', { id: issue.id })}</span>
            <div>
              <ComButton size="small" disabled={!onPrevious} onClick={onPrevious}>
                <ChevronLeft size={16} />
                {t('dataQuality.diagnosis.previousAlarm')}
              </ComButton>
              <ComButton size="small" disabled={!onNext} onClick={onNext}>
                {t('dataQuality.diagnosis.nextAlarm')}
                <ChevronRight size={16} />
              </ComButton>
            </div>
          </div>

          <div className={styles['issue-hero']}>
            <span className={styles[`issue-hero-${issue.severity}`]}>
              <WarningAltFilled size={22} />
            </span>
            <div className={styles['issue-hero-main']}>
              <div className={styles['issue-hero-title']}>
                <Tag color={severityColor(issue.severity)}>{t(`dataQuality.severity.${issue.severity}`)}</Tag>
                <strong>{issueNodeLabel(issue)}</strong>
                <span>{t(`dataQuality.rule.${issue.ruleCode}`, undefined, issue.ruleCode)}</span>
                <Tag color={stateColor(issue.state)}>{t(`dataQuality.state.${issue.state}`)}</Tag>
              </div>
              <p>{issue.namespace || '-'}</p>
            </div>
            <div className={styles['issue-hero-time']}>
              <span>{t('dataQuality.lastObserved')}</span>
              <strong>{formatTimestamp(issue.lastObservedAt) || '-'}</strong>
            </div>
          </div>

          <section className={`${styles['drawer-section']} ${styles['decision-section']}`}>
            <div className={styles['section-title-row']}>
              <div>
                <h3>{t('dataQuality.diagnosis.whyTriggered')}</h3>
                <p>{t('dataQuality.diagnosis.whyTriggeredDesc')}</p>
              </div>
              {diagnosis.isLegacyEvidence && <Tag>{t('dataQuality.diagnosis.legacyEvidence')}</Tag>}
            </div>
            <div className={styles['decision-conclusion']}>{diagnosis.summary}</div>
          </section>

          <section className={styles['drawer-section']}>
            <div className={styles['section-title-row']}>
              <div>
                <h3>{t('dataQuality.diagnosis.causalTitle')}</h3>
                <p>{t('dataQuality.diagnosis.causalDesc')}</p>
              </div>
            </div>
            <ol className={styles['causal-chain']}>
              {causalSteps.map((step, index) => (
                <li key={step.key}>
                  <div className={styles['causal-card']}>
                    <header>
                      <span>{index + 1}</span>
                      <strong>{step.title}</strong>
                    </header>
                    <p>{step.description}</p>
                    {step.payload && (
                      <dl>
                        {Object.entries(step.payload.values)
                          .slice(0, 3)
                          .map(([field, value]) => (
                            <div key={field}>
                              <dt>{field}</dt>
                              <dd>{String(value ?? '-')}</dd>
                            </div>
                          ))}
                      </dl>
                    )}
                  </div>
                  {index < causalSteps.length - 1 && <ArrowRight className={styles['causal-arrow']} size={20} />}
                </li>
              ))}
            </ol>
          </section>

          <div className={styles['drawer-analysis-grid']}>
            <section className={styles['drawer-section']}>
              <h3>{t('dataQuality.diagnosis.timelineTitle')}</h3>
              <p>{t('dataQuality.diagnosis.timelineDesc')}</p>
              <Timeline
                className={styles['issue-timeline']}
                items={timeline.map((point) => ({
                  color:
                    point.tone === 'danger'
                      ? 'var(--ui-error-color)'
                      : point.tone === 'warning'
                        ? 'var(--ui-status-warning-color)'
                        : point.tone === 'success'
                          ? 'var(--ui-status-active-text)'
                          : 'var(--ui-theme-color)',
                  children: (
                    <div className={styles['timeline-item']}>
                      <div>
                        <strong>{point.title}</strong>
                        <time>{point.time}</time>
                      </div>
                      <p>{point.description}</p>
                    </div>
                  ),
                }))}
              />
            </section>

            <section className={styles['drawer-section']}>
              <h3>{t('dataQuality.diagnosis.thresholdTitle')}</h3>
              <p>{t('dataQuality.diagnosis.thresholdDesc')}</p>
              {diagnosis.comparison ? (
                <div className={styles['threshold-comparison']}>
                  <div>
                    <span>{diagnosis.comparison.observedLabel}</span>
                    <strong data-tone="danger">{diagnosis.comparison.observedText}</strong>
                    <Progress
                      percent={(diagnosis.comparison.observedValue / comparisonMax) * 100}
                      showInfo={false}
                      strokeColor="var(--ui-error-color)"
                      trailColor="var(--ui-fill-secondary)"
                      strokeLinecap="square"
                    />
                  </div>
                  <div>
                    <span>{diagnosis.comparison.thresholdLabel}</span>
                    <strong>{diagnosis.comparison.thresholdText}</strong>
                    <Progress
                      percent={(diagnosis.comparison.thresholdValue / comparisonMax) * 100}
                      showInfo={false}
                      strokeColor="var(--ui-theme-color)"
                      trailColor="var(--ui-fill-secondary)"
                      strokeLinecap="square"
                    />
                  </div>
                </div>
              ) : (
                <div className={styles['decision-metrics']}>
                  {diagnosis.metrics.map((item) => (
                    <div key={item.key} data-tone={item.tone}>
                      <span>{item.label}</span>
                      <strong>{item.value}</strong>
                    </div>
                  ))}
                </div>
              )}
              {diagnosis.formula && (
                <div className={styles['decision-formula']}>
                  <span>{t('dataQuality.diagnosis.decisionFormula')}</span>
                  <code>{diagnosis.formula}</code>
                </div>
              )}
            </section>
          </div>

          <div className={styles['drawer-evidence-grid']}>
            <section className={styles['drawer-section']}>
              <h3>{t('dataQuality.diagnosis.parameterTitle')}</h3>
              <p>{t('dataQuality.diagnosis.parameterDesc')}</p>
              {evidence.length > 0 ? (
                <dl className={styles['evidence-list']}>
                  {evidence.map((entry) => (
                    <div key={entry.key}>
                      <dt>{entry.label}</dt>
                      <dd>{entry.value}</dd>
                    </div>
                  ))}
                </dl>
              ) : (
                <div className={styles['drawer-empty']}>{t('dataQuality.dashboard.noEvidence')}</div>
              )}
            </section>

            <section className={styles['drawer-section']}>
              <h3>{t('dataQuality.diagnosis.payloadTitle')}</h3>
              <p>{t('dataQuality.diagnosis.payloadDesc')}</p>
              {diagnosis.payloads.length > 0 ? (
                <Tabs
                  className={styles['payload-tabs']}
                  items={diagnosis.payloads.map((payload) => ({
                    key: payload.key,
                    label: payload.title,
                    children: (
                      <div className={styles['payload-code-panel']}>
                        <span>
                          {t('dataQuality.diagnosis.payloadFieldCount', {
                            shown: Object.keys(payload.values).length,
                            total: payload.fieldCount || Object.keys(payload.values).length,
                          })}
                          {(payload.truncated || (payload.fieldCount || 0) > Object.keys(payload.values).length) &&
                            ` · ${t('dataQuality.diagnosis.payloadTruncated')}`}
                        </span>
                        <pre>{JSON.stringify(payload.values, null, 2)}</pre>
                      </div>
                    ),
                  }))}
                />
              ) : (
                <div className={styles['drawer-empty']}>{t('dataQuality.diagnosis.noPayload')}</div>
              )}
            </section>
          </div>

          <section className={styles['drawer-section']}>
            <h3>{t('dataQuality.diagnosis.eventContext')}</h3>
            <p>{t('dataQuality.diagnosis.eventContextDesc')}</p>
            <Descriptions bordered size="small" column={screens.lg ? 4 : 2} className={styles['issue-descriptions']}>
              <Descriptions.Item label={t('dataQuality.firstObserved')}>
                {formatTimestamp(issue.firstObservedAt) || '-'}
              </Descriptions.Item>
              <Descriptions.Item label={t('dataQuality.lastObserved')}>
                {formatTimestamp(issue.lastObservedAt) || '-'}
              </Descriptions.Item>
              <Descriptions.Item label={t('dataQuality.dashboard.duration')}>
                {formatDuration(issue.firstObservedAt, issue.resolvedAt, t)}
              </Descriptions.Item>
              <Descriptions.Item label={t('dataQuality.occurrences')}>{issue.occurrences}</Descriptions.Item>
              <Descriptions.Item label={t('dataQuality.nodeId')}>{issue.unsId}</Descriptions.Item>
              <Descriptions.Item label={t('dataQuality.dashboard.namespace')} span={screens.lg ? 2 : 1}>
                {issue.namespace || '-'}
              </Descriptions.Item>
            </Descriptions>
          </section>
        </div>
      )}
    </ComDrawer>
  );
};

export default IssueDetailDrawer;
