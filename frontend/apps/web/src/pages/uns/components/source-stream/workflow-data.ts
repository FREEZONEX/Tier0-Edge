import type { ProgramWorkflow } from './program-workflow';
import { workflowIssues, workflowThrough } from './program-workflow';

export function workflowStages(workflow: ProgramWorkflow, selected: string) {
  const action = workflow.actions.find((step) => step.id === selected);
  const issues = workflowIssues(workflowThrough(workflow, selected), false);
  return {
    configure: !!action,
    setupIssues: issues.filter(
      (issue) => issue.step === selected && ['source', 'resource', 'qos', 'target'].includes(issue.code)
    ),
    issues,
  };
}

export function outputAt(value: unknown, path: string): unknown {
  let current = value;
  for (const key of path ? path.split('.') : []) {
    if (
      !key ||
      ['__proto__', 'constructor', 'prototype'].includes(key) ||
      current === null ||
      typeof current !== 'object' ||
      !Object.prototype.hasOwnProperty.call(current, key)
    )
      return undefined;
    current = (current as Record<string, unknown>)[key];
  }
  return current;
}

export function displayValue(value: unknown): string {
  if (value === undefined) return '—';
  const text = typeof value === 'string' && value ? value : JSON.stringify(value);
  return text.length > 180 ? `${text.slice(0, 180)}…` : text;
}
