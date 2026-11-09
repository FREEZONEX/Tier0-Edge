import type { Source } from '@/apis/core-api/source-stream';
import { sourceInsertionModes } from './program-document';
import { workflowSource, type ProgramWorkflow, type WorkflowConnection } from './program-workflow';

export const isHTTPConnection = (source: Source) =>
  !source.document.sourceType && ['http-api', 'oauth2'].includes(source.protocol) && source.document.role !== 'server';

export function connectionFromSource(source: Source, alias: string, resource = ''): WorkflowConnection | undefined {
  const mode = sourceInsertionModes(source, resource)[0];
  if (!mode || mode === 'http-server') return;
  return { ...workflowSource(source, resource), alias, mode };
}

export function workflowUsedSources(workflow: ProgramWorkflow) {
  const sources = [
    ...(workflow.trigger.kind === 'source-event' && workflow.trigger.source ? [workflow.trigger.source] : []),
    ...workflow.actions.flatMap((action) => (action.kind === 'source' && action.source ? [action.source] : [])),
  ];
  return [...new Map(sources.map((source) => [source.id + '/' + source.resource, source])).values()];
}

export function sourceConnectionDeclaration(connection: WorkflowConnection) {
  return `const ${connection.alias} = api.sources.use(${JSON.stringify({
    id: connection.id,
    resources: connection.mode === 'request' ? [] : [connection.resource],
  })});`;
}

export function insertConnectionCall(code: string, connection: WorkflowConnection): string {
  if (connection.mode === 'source-event') return code;
  const handle = `connections.${connection.alias}`;
  const call =
    connection.mode === 'request'
      ? `${handle}.request({ method: 'GET', path: '/' })`
      : `${handle}.${connection.mode}(${JSON.stringify(connection.resource)}${connection.mode === 'read' ? '' : ', input'})`;
  let result = `${connection.alias}Result`;
  for (let suffix = 2; code.includes(result); suffix++) result = `${connection.alias}Result${suffix}`;
  const snippet = `const ${result} = await ${call};`;
  return `${snippet}\n${code.trim() === 'return input;' ? `return ${result};` : code}`;
}

// 脚本的实际调用只能由运行时确定；提供响应的入口不代表会自动请求该 Source。
export function workflowTestCalls(workflow: ProgramWorkflow) {
  const calls = workflow.actions.flatMap((step) =>
    step.kind === 'source' && step.source
      ? [
          {
            key: `${step.source.id}/${step.operation === 'request' ? 'request' : step.source.resource}`,
            source: step.source,
            operation: step.operation,
            optional: false,
            safeFetch: step.operation === 'request' && ['GET', 'HEAD'].includes(step.method),
          },
        ]
      : []
  );
  if (workflow.actions.some((step) => step.kind === 'script')) {
    for (const connection of workflow.connections ?? []) {
      if (connection.mode === 'source-event') continue;
      const key = `${connection.id}/${connection.mode === 'request' ? 'request' : connection.resource}`;
      if (!calls.some((call) => call.key === key))
        calls.push({
          key,
          source: connection,
          operation: connection.mode === 'request' ? 'request' : 'read',
          optional: true,
          safeFetch: false,
        });
    }
  }
  return calls;
}
