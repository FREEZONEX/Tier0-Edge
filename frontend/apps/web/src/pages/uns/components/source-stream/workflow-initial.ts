import type { Source } from '@/apis/core-api/source-stream';
import { initialProgram, sourceInsertionModes } from './program-document';
import { newWorkflow, previousValue, withWorkflow, workflowSource } from './program-workflow';

export function initialWorkflowProgram(source?: Source, resource?: string) {
  const workflow = newWorkflow();
  if (source) {
    const ref = resource && source.resources.some((item) => item.id === resource) ? resource : '';
    const http = source.protocol === 'http-api' && !source.document.sourceType && source.document.role !== 'server';
    const connection = workflowSource(source, ref);
    if (source.resources.some((item) => sourceInsertionModes(source, item.id).includes('source-event'))) {
      workflow.trigger = { ...workflow.trigger, kind: 'source-event', source: connection };
    } else {
      workflow.actions.push({
        id: 'read-source',
        kind: 'source',
        source: connection,
        operation: http ? 'request' : 'read',
        method: 'GET',
        path: '/',
      });
    }
  }
  if (resource && /^(topic|collection):/.test(resource)) {
    const input = workflow.actions.at(-1)?.id ?? 'trigger';
    workflow.actions.push({
      id: 'write-output',
      kind: 'write',
      target: resource,
      recordId: { kind: 'field', step: input, path: 'id' },
      record: previousValue(input),
    });
  }
  return withWorkflow(initialProgram(), workflow);
}
