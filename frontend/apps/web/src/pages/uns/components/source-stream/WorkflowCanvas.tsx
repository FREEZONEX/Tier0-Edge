import { Fragment, useEffect, useRef, useState, type FC, type ReactNode } from 'react';
import { Add, Checkmark, Code, Flow, Play, Plug, DataBase, WarningAlt } from '@carbon/icons-react';
import classNames from 'classnames';
import { useTranslate } from '@/hooks';
import ComButton from '@/components/com-button';
import WorkflowActionPicker from './WorkflowActionPicker';
import { workflowIssues, type ProgramWorkflow, type WorkflowAction, type WorkflowTrace } from './program-workflow';
import styles from './workflow.module.scss';
import type { UnsMetadata } from '@/apis/core-api/uns-metadata';
import { outputCollection } from './use-workflow-metadata';

const WorkflowCanvas: FC<{
  workflow?: ProgramWorkflow;
  selected?: string;
  onSelect: (id: string) => void;
  onAdd?: (kind: WorkflowAction['kind'], index: number) => void;
  results?: WorkflowTrace[];
  metadata?: UnsMetadata[];
  children?: ReactNode;
}> = ({ workflow, selected, onSelect, onAdd, results = [], metadata = [], children }) => {
  const t = useTranslate();
  const [insertAt, setInsertAt] = useState<number>();
  const listRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const viewport = listRef.current?.parentElement;
    const card = listRef.current?.querySelector('[aria-pressed="true"]');
    if (!viewport || !card) return;
    const bounds = viewport.getBoundingClientRect();
    const item = card.getBoundingClientRect();
    if (item.left < bounds.left) viewport.scrollLeft += item.left - bounds.left;
    else if (item.right > bounds.right) viewport.scrollLeft += item.right - bounds.right;
  }, [selected]);
  const issues = workflow ? workflowIssues(workflow) : [];
  const steps = workflow
    ? [...(workflow.trigger.kind === 'source-event' ? [workflow.trigger] : []), ...workflow.actions]
    : [];
  const insert = (index: number) => (
    <div className={styles['step-connector']}>
      {onAdd && (
        <ComButton
          type="text"
          shape="circle"
          icon={<Add size={20} />}
          aria-label={t('Workflow.addAction')}
          onClick={() => setInsertAt(index)}
        />
      )}
    </div>
  );
  return (
    <div className={classNames(styles['editor-body'], { [styles['panel-open']]: !!children })}>
      <section className={styles['outline-panel']} aria-label={t('Workflow.workflow')}>
        <div className={styles['step-list']} ref={listRef}>
          {steps.map((step, index) => {
            const start = step.id === 'trigger';
            const problem = issues.some((issue) => issue.step === step.id && issue.code !== 'action');
            const result = results.find((item) => item.id === step.id);
            const collection = step.kind === 'write' ? outputCollection(step.target, metadata) : undefined;
            const outputFields =
              step.kind === 'write'
                ? step.fields?.map((field) => field.key).filter(Boolean) ||
                  Object.keys(collection?.data.recordSchema?.properties || {})
                : [];
            return (
              <Fragment key={step.id}>
                <button
                  type="button"
                  aria-pressed={selected === step.id}
                  className={classNames(styles['step-card'], { [styles['step-selected']]: selected === step.id })}
                  onClick={() => onSelect(step.id)}
                >
                  <span className={styles['step-icon']}>
                    {start ? (
                      <Play size={22} />
                    ) : step.kind === 'source' ? (
                      <Plug size={22} />
                    ) : step.kind === 'write' ? (
                      <DataBase size={22} />
                    ) : step.kind === 'script' ? (
                      <Code size={22} />
                    ) : (
                      <Flow size={22} />
                    )}
                  </span>
                  <span className={styles['step-copy']}>
                    <span className={styles['step-kicker']}>
                      {index + 1}.{' '}
                      {t(start ? 'Workflow.runMode' : step.kind === 'write' ? 'Output.dataOutput' : 'Workflow.action')}
                    </span>
                    <strong>
                      {collection?.data.name ||
                        t(start ? `Workflow.runMode.${workflow!.trigger.kind}` : `Workflow.kind.${step.kind}`)}
                    </strong>
                    {'source' in step && step.source && (
                      <span className={styles['muted-text']}>{step.source.name}</span>
                    )}
                    {problem && <span className={styles['muted-text']}>{t('Workflow.needsSetup')}</span>}
                    {!!outputFields.length && (
                      <span className={styles['output-chips']}>
                        {outputFields.slice(0, 4).map((name) => (
                          <span key={name}>{name}</span>
                        ))}
                        {outputFields.length > 4 && <span>+{outputFields.length - 4}</span>}
                      </span>
                    )}
                    {step.kind === 'write' && collection && (
                      <span className={styles['muted-text']}>{t(`Output.mode.${step.mode || 'replace'}`)}</span>
                    )}
                  </span>
                  {result?.failed ? (
                    <WarningAlt aria-label={t('SourceStream.testFailed')} />
                  ) : result && !result.stopped ? (
                    <Checkmark aria-label={t('Workflow.testPassed')} />
                  ) : null}
                </button>
                {insert(start ? 0 : workflow!.actions.findIndex((action) => action.id === step.id) + 1)}
              </Fragment>
            );
          })}
          {workflow && !workflow.actions.length && onAdd && (
            <button
              type="button"
              className={classNames(styles['step-card'], styles['step-placeholder'])}
              onClick={() => setInsertAt(0)}
            >
              <span className={styles['step-icon']}>
                <Add size={22} />
              </span>
              <span className={styles['step-copy']}>
                <span className={styles['step-kicker']}>{t('Workflow.action')}</span>
                <strong>{t('Workflow.chooseAction')}</strong>
              </span>
            </button>
          )}
          {!workflow && (
            <button
              type="button"
              aria-pressed={selected === 'script'}
              className={classNames(styles['step-card'], { [styles['step-selected']]: selected === 'script' })}
              onClick={() => onSelect('script')}
            >
              <span className={styles['step-icon']}>
                <Code size={22} />
              </span>
              <span className={styles['step-copy']}>
                <span className={styles['step-kicker']}>{t('SourceStream.stream')}</span>
                <strong>{t('Workflow.scriptStep')}</strong>
              </span>
            </button>
          )}
        </div>
      </section>
      {children}
      {insertAt !== undefined && onAdd && (
        <WorkflowActionPicker
          onClose={() => setInsertAt(undefined)}
          onSelect={(kind) => {
            onAdd(kind, insertAt);
            setInsertAt(undefined);
          }}
        />
      )}
    </div>
  );
};
export default WorkflowCanvas;
