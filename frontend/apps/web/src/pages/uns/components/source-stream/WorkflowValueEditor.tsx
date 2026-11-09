import { useState } from 'react';
import { AutoComplete, Collapse, Input, Popover, Select, Tabs } from 'antd';
import { Add, Close, DataReference } from '@carbon/icons-react';
import { useTranslate } from '@/hooks';
import ComButton from '@/components/com-button';
import { outputPaths, type WorkflowTrace, type WorkflowValue } from './program-workflow';
import { displayValue, outputAt } from './workflow-data';
import styles from './workflow.module.scss';

export default function WorkflowValueEditor({
  value,
  onChange,
  steps,
  results = [],
  label,
  onTestStep,
}: {
  value: WorkflowValue;
  onChange: (value: WorkflowValue) => void;
  steps: { value: string; label: string }[];
  results?: WorkflowTrace[];
  label: string;
  onTestStep?: (id: string) => void;
}) {
  const t = useTranslate();
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState('');
  const source = value.kind === 'field' ? steps.find((step) => step.value === value.step) : undefined;
  const trace = value.kind === 'field' ? results.find((step) => step.id === value.step) : undefined;
  const preview = value.kind === 'field' && trace ? outputAt(trace.output, value.path) : undefined;
  const select = (step: string, path: string) => {
    onChange({ kind: 'field', step, path });
    setOpen(false);
  };
  const groups = steps
    .map((step) => {
      const result = results.find((item) => item.id === step.value);
      const fields = result
        ? ['', ...outputPaths(result.output)].filter((path) =>
            `${step.label} ${path || t('Workflow.entireOutput')} ${displayValue(outputAt(result.output, path))}`
              .toLowerCase()
              .includes(search.toLowerCase())
          )
        : [];
      return { step, result, fields };
    })
    .filter(({ result, fields }) => !result || fields.length);
  const picker = (
    <div className={styles['data-picker']}>
      <div className={styles['picker-heading']}>
        <strong>{t('Workflow.insertData')}</strong>
        <ComButton type="text" aria-label={t('Workflow.closePicker')} icon={<Close />} onClick={() => setOpen(false)} />
      </div>
      <Tabs
        items={[
          {
            key: 'data',
            label: t('Workflow.previousStep'),
            children: (
              <>
                <Input.Search
                  aria-label={t('Workflow.searchFields')}
                  placeholder={t('Workflow.searchFields')}
                  value={search}
                  onChange={(event) => setSearch(event.target.value)}
                  allowClear
                />
                <div className={styles['picker-results']}>
                  {groups.length ? (
                    <Collapse
                      ghost
                      defaultActiveKey={steps.map((step) => step.value)}
                      items={groups.map(({ step, result, fields }) => ({
                        key: step.value,
                        label: step.label,
                        children: result ? (
                          fields.map((path) => (
                            <button
                              type="button"
                              key={path}
                              className={styles['data-option']}
                              onClick={() => select(step.value, path)}
                            >
                              <span>{path || t('Workflow.entireOutput')}</span>
                              <span className={styles['muted-text']}>
                                {displayValue(outputAt(result.output, path))}
                              </span>
                            </button>
                          ))
                        ) : (
                          <div className={styles['empty-data']}>
                            <span>{t('Workflow.testToSeeFields')}</span>
                            {onTestStep && (
                              <ComButton
                                size="small"
                                onClick={() => {
                                  setOpen(false);
                                  onTestStep(step.value);
                                }}
                              >
                                {t('Workflow.testStep')}
                              </ComButton>
                            )}
                          </div>
                        ),
                      }))}
                    />
                  ) : (
                    <p className={styles['muted-text']}>{t('Workflow.noFields')}</p>
                  )}
                </div>
              </>
            ),
          },
          {
            key: 'advanced',
            label: t('Workflow.advancedValue'),
            children: (
              <div className={styles['form-stack']}>
                <Select
                  aria-label={`${label} · ${t('Workflow.valueMode')}`}
                  value={value.kind}
                  options={['text', 'json', 'field'].map((kind) => ({
                    value: kind,
                    label: t(`Workflow.value.${kind}`),
                  }))}
                  onChange={(kind) =>
                    onChange(
                      kind === 'field'
                        ? { kind, step: steps[steps.length - 1]?.value ?? 'trigger', path: '' }
                        : { kind, value: '' }
                    )
                  }
                />
                {value.kind === 'field' && (
                  <>
                    <Select
                      aria-label={`${label} · ${t('Workflow.previousStep')}`}
                      value={value.step}
                      options={steps}
                      status={source ? undefined : 'error'}
                      onChange={(step) => onChange({ ...value, step, path: '' })}
                    />
                    <AutoComplete
                      aria-label={`${label} · ${t('Workflow.fieldPath')}`}
                      value={value.path}
                      placeholder={t('Workflow.wholeOutput')}
                      onChange={(path) => onChange({ ...value, path })}
                      options={outputPaths(trace?.output).map((path) => ({ value: path }))}
                      filterOption={(input, option) =>
                        String(option?.value).toLowerCase().includes(input.toLowerCase())
                      }
                    />
                  </>
                )}
              </div>
            ),
          },
        ]}
      />
    </div>
  );
  return (
    <div className={styles['value-editor']}>
      <div className={styles['value-input']}>
        {value.kind === 'field' ? (
          <div className={styles['reference-value']}>
            <button type="button" className={styles['reference-chip']} onClick={() => setOpen(true)}>
              <DataReference size={16} />
              <span>
                {source?.label ?? value.step} / {value.path || t('Workflow.entireOutput')}
              </span>
            </button>
            <ComButton
              type="text"
              size="small"
              aria-label={`${label} · ${t('Workflow.clearValue')}`}
              icon={<Close />}
              onClick={() => onChange({ kind: 'text', value: '' })}
            />
          </div>
        ) : value.kind === 'json' ? (
          <Input.TextArea
            aria-label={label}
            autoSize={{ minRows: 2, maxRows: 7 }}
            value={value.value}
            placeholder={t('Workflow.jsonValue')}
            onChange={(event) => onChange({ ...value, value: event.target.value })}
          />
        ) : (
          <Input
            aria-label={label}
            value={value.value}
            placeholder={t('Workflow.valuePlaceholder')}
            onChange={(event) => onChange({ ...value, value: event.target.value })}
          />
        )}
        <Popover open={open} onOpenChange={setOpen} trigger="click" placement="leftTop" content={picker}>
          <ComButton aria-label={`${label} · ${t('Workflow.insertData')}`} icon={<Add />} />
        </Popover>
      </div>
      {value.kind === 'field' && (
        <span className={styles['muted-text']}>
          {!source
            ? t('Workflow.issue.reference')
            : trace
              ? preview === undefined
                ? t('Workflow.missingField')
                : `${t('Workflow.exampleValue')}: ${displayValue(preview)}`
              : t('Workflow.noTestData')}
        </span>
      )}
    </div>
  );
}
