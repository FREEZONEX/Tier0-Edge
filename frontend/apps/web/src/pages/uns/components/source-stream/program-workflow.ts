import type { ProgramDocument, Source } from '@/apis/core-api/source-stream';

// 与 backend/internal/streamexec/program.go 的执行上限一致；表单按整秒配置。
export const WORKFLOW_LIMITS = {
  sourceCalls: 64,
  records: 1000,
  minIntervalMs: 1000,
  maxIntervalMs: 86400000,
} as const;
export const validWorkflowInterval = (value: number) =>
  Number.isSafeInteger(value) && value >= WORKFLOW_LIMITS.minIntervalMs && value <= WORKFLOW_LIMITS.maxIntervalMs;
export const requestAllowsBody = (method: string) => !['GET', 'HEAD'].includes(method);

export type WorkflowValue = { kind: 'text' | 'json'; value: string } | { kind: 'field'; step: string; path: string };
export type WorkflowSource = { id: string; name: string; resource: string; qos?: number };
export type WorkflowConnection = WorkflowSource & {
  alias: string;
  mode: 'request' | 'read' | 'write' | 'invoke' | 'publish' | 'source-event';
};
export const validConnectionAlias = (alias: string) =>
  /^[a-zA-Z][a-zA-Z0-9_]*$/.test(alias) && !['__proto__', 'prototype', 'constructor'].includes(alias);
export const workflowSource = (source: Source, resource = ''): WorkflowSource => ({
  id: String(source.id),
  name: source.name,
  resource,
  ...(source.protocol === 'mqtt' ? { qos: source.document.mqtt?.qos ?? 0 } : {}),
});
export type WorkflowTrigger = {
  id: 'trigger';
  kind: 'request' | 'interval' | 'source-event';
  intervalMs: number;
  source?: WorkflowSource;
};
type ActionBase = { id: string };
export type WorkflowAction =
  | (ActionBase & {
      kind: 'source';
      source?: WorkflowSource;
      operation: 'request' | 'read';
      method: string;
      path: string;
      query?: { key: string; value: string }[];
      headers?: { key: string; value: string }[];
      input?: WorkflowValue;
      pagination?: {
        mode: 'cursor' | 'page';
        itemsPath: string;
        nextPath: string;
        parameter: string;
        sizeParameter: string;
        maxPages: number;
        pageSize: number;
        startPage: number;
      };
    })
  | (ActionBase & { kind: 'map'; fields: { key: string; value: WorkflowValue }[] })
  | (ActionBase & { kind: 'script'; input: WorkflowValue; code: string })
  | (ActionBase & {
      kind: 'filter';
      left: WorkflowValue;
      operator: 'eq' | 'ne' | 'exists' | 'gt' | 'lt';
      right: WorkflowValue;
    })
  | (ActionBase & {
      kind: 'write';
      mode?: 'create' | 'upsert' | 'update' | 'replace';
      unset?: string[];
      target: string;
      recordId: WorkflowValue;
      record: WorkflowValue;
      fields?: { key: string; value: WorkflowValue }[];
      each?: { records: WorkflowValue; identityPath: string; fields?: string[] };
      sample?: WorkflowValue;
      time?: WorkflowValue;
      quality?: WorkflowValue;
    });
export interface ProgramWorkflow {
  version: 1;
  trigger: WorkflowTrigger;
  actions: WorkflowAction[];
  connections?: WorkflowConnection[];
}
export type WorkflowIssue = { step: string; code: string };
export type WorkflowTrace = { id: string; input: unknown; output: unknown; stopped?: boolean; failed?: boolean };
const extensionKey = 'tier0Workflow';
const unsafe = new Set(['__proto__', 'prototype', 'constructor']);
const targetPattern = /^(topic|collection):[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
export const validWorkflowTarget = (target: string) =>
  targetPattern.test(target) && !target.endsWith(':00000000-0000-0000-0000-000000000000');
const object = (value: unknown): value is Record<string, unknown> =>
  typeof value === 'object' && value !== null && !Array.isArray(value);
const bindingShape = (value: unknown): value is WorkflowValue =>
  object(value) &&
  (value.kind === 'field'
    ? typeof value.step === 'string' && typeof value.path === 'string'
    : ['text', 'json'].includes(String(value.kind)) && typeof value.value === 'string');
const sourceShape = (value: unknown) =>
  value === undefined ||
  (object(value) &&
    ['id', 'name', 'resource'].every((key) => typeof value[key] === 'string') &&
    (value.qos === undefined || typeof value.qos === 'number'));

// JSONB may reorder object keys. The executable artifact must survive persistence unchanged.
export function workflowJSON(value: unknown): string {
  if (Array.isArray(value)) return `[${value.map(workflowJSON).join(',')}]`;
  if (object(value))
    return `{${Object.keys(value)
      .sort()
      .filter((key) => value[key] !== undefined)
      .map((key) => `${JSON.stringify(key)}:${workflowJSON(value[key])}`)
      .join(',')}}`;
  return JSON.stringify(value) ?? 'null';
}

function workflowShape(value: unknown): value is ProgramWorkflow {
  if (!object(value) || value.version !== 1 || !object(value.trigger) || !Array.isArray(value.actions)) return false;
  if (
    value.connections !== undefined &&
    (!Array.isArray(value.connections) ||
      !value.connections.every(
        (connection) =>
          object(connection) &&
          sourceShape(connection) &&
          typeof connection.alias === 'string' &&
          ['request', 'read', 'write', 'invoke', 'publish', 'source-event'].includes(String(connection.mode))
      ))
  )
    return false;
  const trigger = value.trigger;
  if (
    trigger.id !== 'trigger' ||
    !['request', 'interval', 'source-event'].includes(String(trigger.kind)) ||
    typeof trigger.intervalMs !== 'number' ||
    !sourceShape(trigger.source)
  )
    return false;
  return value.actions.every((step) => {
    if (!object(step) || typeof step.id !== 'string') return false;
    switch (step.kind) {
      case 'source':
        return (
          sourceShape(step.source) &&
          ['read', 'request'].includes(String(step.operation)) &&
          typeof step.method === 'string' &&
          typeof step.path === 'string' &&
          [step.query, step.headers].every(
            (pairs) =>
              pairs === undefined ||
              (Array.isArray(pairs) &&
                pairs.every((pair) => object(pair) && typeof pair.key === 'string' && typeof pair.value === 'string'))
          ) &&
          (step.pagination === undefined ||
            (object(step.pagination) &&
              ['cursor', 'page'].includes(String(step.pagination.mode)) &&
              ['itemsPath', 'nextPath', 'parameter', 'sizeParameter'].every(
                (key) => typeof (step.pagination as Record<string, unknown>)[key] === 'string'
              ) &&
              ['maxPages', 'pageSize', 'startPage'].every(
                (key) => typeof (step.pagination as Record<string, unknown>)[key] === 'number'
              ))) &&
          (step.input === undefined || bindingShape(step.input))
        );
      case 'map':
        return (
          Array.isArray(step.fields) &&
          step.fields.every((field) => object(field) && typeof field.key === 'string' && bindingShape(field.value))
        );
      case 'filter':
        return (
          bindingShape(step.left) &&
          bindingShape(step.right) &&
          ['eq', 'ne', 'exists', 'gt', 'lt'].includes(String(step.operator))
        );
      case 'script':
        return bindingShape(step.input) && typeof step.code === 'string';
      case 'write':
        return (
          (step.mode === undefined || ['create', 'upsert', 'update', 'replace'].includes(String(step.mode))) &&
          (step.unset === undefined ||
            (Array.isArray(step.unset) && step.unset.every((key) => typeof key === 'string'))) &&
          typeof step.target === 'string' &&
          bindingShape(step.recordId) &&
          bindingShape(step.record) &&
          (step.each === undefined ||
            (object(step.each) &&
              bindingShape(step.each.records) &&
              typeof step.each.identityPath === 'string' &&
              (step.each.fields === undefined ||
                (Array.isArray(step.each.fields) && step.each.fields.every((key) => typeof key === 'string'))))) &&
          (step.fields === undefined ||
            (Array.isArray(step.fields) &&
              step.fields.every(
                (field) => object(field) && typeof field.key === 'string' && bindingShape(field.value)
              ))) &&
          ['sample', 'time', 'quality'].every((key) => step[key] === undefined || bindingShape(step[key]))
        );
      default:
        return false;
    }
  });
}

export function workflowIssues(workflow: ProgramWorkflow, requireAction = true): WorkflowIssue[] {
  const issues: WorkflowIssue[] = [];
  const issue = (step: string, code: string) => issues.push({ step, code });
  const known = new Set(['trigger']);
  const checkSource = (id: string, source?: WorkflowSource, resource = true) => {
    if (!source || !/^\d+$/.test(source.id)) issue(id, 'source');
    else if (resource && !source.resource.trim()) issue(id, 'resource');
  };
  const checkValue = (id: string, value: WorkflowValue) => {
    if (value.kind === 'field') {
      if (!known.has(value.step)) issue(id, 'reference');
      if (value.path && value.path.split('.').some((key) => !key || unsafe.has(key))) issue(id, 'path');
    } else if (value.kind === 'json') {
      try {
        JSON.parse(value.value);
      } catch {
        issue(id, 'json');
      }
    }
  };
  const aliases = new Set<string>();
  for (const connection of workflow.connections ?? []) {
    if (!validConnectionAlias(connection.alias) || aliases.has(connection.alias))
      issue('connections', 'connectionAlias');
    aliases.add(connection.alias);
    checkSource('connections', connection, connection.mode !== 'request');
  }
  const trigger = workflow.trigger;
  if (trigger.kind === 'interval' && !validWorkflowInterval(trigger.intervalMs)) issue('trigger', 'interval');
  if (trigger.kind === 'source-event') {
    checkSource('trigger', trigger.source);
    if (trigger.source?.qos !== undefined && ![0, 1, 2].includes(trigger.source.qos)) issue('trigger', 'qos');
  }
  if (requireAction && !workflow.actions.length) issue('trigger', 'action');
  let sourceCalls = 0;
  for (const step of workflow.actions) {
    if (!/^[a-zA-Z][\w-]*$/.test(step.id) || known.has(step.id) || unsafe.has(step.id)) issue(step.id, 'id');
    if (step.kind === 'source') {
      sourceCalls += step.pagination?.maxPages ?? 1;
      if (sourceCalls > WORKFLOW_LIMITS.sourceCalls) issue(step.id, 'callLimit');
      checkSource(step.id, step.source, step.operation === 'read');
      if (
        step.operation === 'request' &&
        (!/^\/(?!\/)/.test(step.path) ||
          /[?#]/.test(step.path) ||
          !['GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'HEAD', 'OPTIONS'].includes(step.method))
      )
        issue(step.id, 'request');
      for (const [kind, pairs] of [
        ['query', step.query],
        ['headers', step.headers],
      ] as const) {
        const keys = pairs?.map((pair) => (kind === 'headers' ? pair.key.toLowerCase() : pair.key)) ?? [];
        if (
          keys.length > 64 ||
          new Set(keys).size !== keys.length ||
          pairs?.some((pair) => !pair.key.trim() || unsafe.has(pair.key) || /[\r\n\0]/.test(pair.key + pair.value))
        )
          issue(step.id, 'requestOptions');
      }
      if (step.operation === 'request' && step.input && !requestAllowsBody(step.method)) issue(step.id, 'body');
      if (step.input) checkValue(step.id, step.input);
      if (step.pagination) {
        const p = step.pagination;
        const pathOK = (path: string) => !path || path.split('.').every((key) => key && !unsafe.has(key));
        if (
          step.operation !== 'request' ||
          step.method !== 'GET' ||
          !pathOK(p.itemsPath) ||
          !p.parameter.trim() ||
          !Number.isSafeInteger(p.maxPages) ||
          p.maxPages < 1 ||
          p.maxPages > WORKFLOW_LIMITS.sourceCalls ||
          !Number.isSafeInteger(p.pageSize) ||
          p.pageSize < 1 ||
          p.pageSize > WORKFLOW_LIMITS.records ||
          p.parameter === p.sizeParameter ||
          (p.mode === 'cursor'
            ? !p.nextPath || !pathOK(p.nextPath)
            : !p.sizeParameter.trim() || !Number.isSafeInteger(p.startPage) || p.startPage < 0)
        )
          issue(step.id, 'pagination');
      }
    } else if (step.kind === 'map') {
      const keys = new Set<string>();
      if (!step.fields.length) issue(step.id, 'fields');
      for (const field of step.fields) {
        if (!field.key.trim() || unsafe.has(field.key) || keys.has(field.key)) issue(step.id, 'fields');
        keys.add(field.key);
        checkValue(step.id, field.value);
      }
    } else if (step.kind === 'filter') {
      checkValue(step.id, step.left);
      if (step.operator !== 'exists') checkValue(step.id, step.right);
    } else if (step.kind === 'script') {
      checkValue(step.id, step.input);
      if (!step.code.trim()) issue(step.id, 'script');
    } else if (step.kind === 'write') {
      if (step.mode !== undefined && !['create', 'upsert', 'update', 'replace'].includes(step.mode))
        issue(step.id, 'writeMode');
      if (
        step.unset?.length &&
        (!['upsert', 'update'].includes(step.mode || '') ||
          step.unset.length > 128 ||
          new Set(step.unset).size !== step.unset.length ||
          step.unset.some((key) => !key.trim() || unsafe.has(key)))
      )
        issue(step.id, 'fields');
      if (!validWorkflowTarget(step.target)) issue(step.id, 'target');
      if (step.each) {
        checkValue(step.id, step.each.records);
        if (!step.each.identityPath || step.each.identityPath.split('.').some((key) => !key || unsafe.has(key)))
          issue(step.id, 'recordId');
        if (
          step.each.fields &&
          (!step.each.fields.length ||
            new Set(step.each.fields).size !== step.each.fields.length ||
            step.each.fields.some((key) => !key || unsafe.has(key)))
        )
          issue(step.id, 'fields');
      } else {
        checkValue(step.id, step.recordId);
        if (step.recordId.kind === 'text' && !step.recordId.value.trim()) issue(step.id, 'recordId');
      }
      if (!step.each && step.fields !== undefined) {
        const keys = new Set<string>();
        if (!step.fields.length) issue(step.id, 'fields');
        for (const field of step.fields) {
          if (!field.key.trim() || unsafe.has(field.key) || keys.has(field.key)) issue(step.id, 'fields');
          keys.add(field.key);
          checkValue(step.id, field.value);
        }
      } else if (!step.each) checkValue(step.id, step.record);
      for (const value of [step.sample, step.time, step.quality]) if (value) checkValue(step.id, value);
    }
    known.add(step.id);
  }
  return issues;
}

export function newWorkflow(): ProgramWorkflow {
  return { version: 1, trigger: { id: 'trigger', kind: 'request', intervalMs: 60000 }, actions: [] };
}
export const previousValue = (step: string): WorkflowValue => ({ kind: 'field', step, path: '' });
export function newWorkflowAction(kind: WorkflowAction['kind'], id: string, previous: string): WorkflowAction {
  const input = previousValue(previous);
  switch (kind) {
    case 'source':
      return { id, kind, operation: 'request', method: 'GET', path: '/' };
    case 'map':
      return { id, kind, fields: [{ key: '', value: input }] };
    case 'script':
      return { id, kind, input, code: 'return input;' };
    case 'filter':
      return { id, kind, left: input, operator: 'exists', right: { kind: 'text', value: '' } };
    case 'write':
      return { id, kind, mode: 'upsert', target: '', recordId: { kind: 'text', value: '' }, record: input };
  }
}

// 表单值保持序列化数据；只有显式添加的脚本步骤会编译为函数，并在后端执行。
const handlerRuntime = (hasScripts: boolean) => `async function(context) {
    const outputs = Object.create(null);
    const trace = [];
    outputs.trigger = context.input;
    if (context.dryRun) trace.push({id: 'trigger', input: context.input, output: context.input});
    const equal = function(left, right) {
      if (left === right) return true;
      if (left === null || right === null || typeof left !== 'object' || typeof right !== 'object') return false;
      if (Array.isArray(left) !== Array.isArray(right)) return false;
      const keys = Object.keys(left);
      return keys.length === Object.keys(right).length && keys.every(function(key) {
        return Object.prototype.hasOwnProperty.call(right, key) && equal(left[key], right[key]);
      });
    };
    const get = function(value, optional) {
      if (value.kind === 'text') return value.value;
      if (value.kind === 'json') return JSON.parse(value.value);
      let data = outputs[value.step];
      for (const key of value.path ? value.path.split('.') : []) {
        if (['__proto__', 'prototype', 'constructor'].indexOf(key) !== -1) throw new Error('Unsafe field path');
        if (data === null || data === undefined || !Object.prototype.hasOwnProperty.call(Object(data), key)) {
          if (optional) return undefined;
          throw new Error('Missing field: ' + value.step + '.' + value.path);
        }
        data = data[key];
      }
      if (data === undefined && !optional) throw new Error('Missing step output: ' + value.step);
      return data;
    };
    let last = context.input;
    const at = function(value, path) {
      for (const key of path ? path.split('.') : []) {
        if (['__proto__', 'prototype', 'constructor'].indexOf(key) !== -1 || value === null || typeof value !== 'object' || !Object.prototype.hasOwnProperty.call(value, key)) return undefined;
        value = value[key];
      }
      return value;
    };
    for (const step of workflow.actions) {
      let input = last;
      let output;
      let stopped = false;
      try {
        if (step.kind === 'source') {
          const source = sources[step.source.id];
          input = step.input ? get(step.input) : undefined;
          const query = Object.create(null);
          for (const pair of step.query || []) query[pair.key] = pair.value;
          const headers = Object.create(null);
          for (const pair of step.headers || []) headers[pair.key] = pair.value;
          if (step.pagination) {
            const p = step.pagination;
            if (p.sizeParameter) query[p.sizeParameter] = String(p.pageSize);
            if (p.mode === 'page') query[p.parameter] = String(p.startPage);
          }
          output = step.operation === 'request'
            ? await source.request(Object.assign({method: step.method, path: step.path, query: query, headers: headers}, step.input ? {json: input} : {}))
            : await source.read(step.source.resource, input);
          if (step.operation === 'request' && (!output || !Number.isInteger(output.status) || output.status < 200 || output.status >= 300)) throw new Error('HTTP request failed: ' + (output && output.status));
          if (step.pagination) {
            const p = step.pagination;
            const first = output;
            const records = [];
            const cursors = new Set();
            let completed = false;
            for (let page = 0; page < p.maxPages; page++) {
              const items = at(output.data, p.itemsPath);
              if (!Array.isArray(items)) throw new Error('Pagination path must select a list');
              records.push(...items);
              if (records.length > ${WORKFLOW_LIMITS.records}) throw new Error('Record limit exceeded');
              const next = p.mode === 'cursor' ? at(output.data, p.nextPath) : p.startPage + page + 1;
              if (p.mode === 'cursor' ? next === null || next === '' : items.length < p.pageSize) { completed = true; break; }
              if (p.mode === 'cursor' && (next === undefined || (typeof next !== 'string' && typeof next !== 'number'))) throw new Error('Missing or invalid next-page cursor');
              if (cursors.has(String(next))) throw new Error('Repeated pagination cursor');
              cursors.add(String(next));
              if (page + 1 === p.maxPages) break;
              output = await source.request({method: 'GET', path: step.path, headers: headers, query: Object.assign({}, query, {[p.parameter]: String(next)})});
              if (!output || !Number.isInteger(output.status) || output.status < 200 || output.status >= 300) throw new Error('HTTP request failed: ' + (output && output.status));
            }
            if (!completed) throw new Error('Pagination incomplete: page limit reached');
            output = JSON.parse(JSON.stringify(first));
            if (!p.itemsPath) output.data = records;
            else {
              const keys = p.itemsPath.split('.');
              let parent = output.data;
              for (let i = 0; i < keys.length - 1; i++) parent = parent[keys[i]];
              parent[keys[keys.length - 1]] = records;
            }
          }
        } ${
          hasScripts
            ? `else if (step.kind === 'script') {
          input = JSON.parse(JSON.stringify(get(step.input)));
          output = await scripts[step.id](JSON.parse(JSON.stringify(input)));
          if (output === undefined) throw new Error('Script must return a JSON value');
          output = JSON.parse(JSON.stringify(output));
        } `
            : ''
        }else if (step.kind === 'map') {
          output = Object.create(null);
          for (const field of step.fields) output[field.key] = get(field.value);
          input = output;
        } else if (step.kind === 'filter') {
          const left = get(step.left, step.operator === 'exists');
          const right = step.operator === 'exists' ? undefined : get(step.right);
          input = {left: left === undefined ? null : left, right: right === undefined ? null : right};
          if ((step.operator === 'gt' || step.operator === 'lt') && (typeof left !== 'number' || typeof right !== 'number'))
            throw new Error('Numeric comparison requires numbers');
          const pass = step.operator === 'exists' ? left !== undefined && left !== null
            : step.operator === 'eq' ? equal(left, right)
            : step.operator === 'ne' ? !equal(left, right)
            : step.operator === 'gt' ? left > right : left < right;
          stopped = !pass;
          output = last;
        } else if (step.kind === 'write') {
          if (step.each) {
            const rows = get(step.each.records);
            if (!Array.isArray(rows) || rows.length > ${WORKFLOW_LIMITS.records}) throw new Error('Expected a record list within the execution limit');
            const ids = new Set();
            const writes = rows.map(function(row) {
              if (!row || typeof row !== 'object' || Array.isArray(row)) throw new Error('Each record must be an object');
              const id = at(row, step.each.identityPath);
              if ((typeof id !== 'string' && (typeof id !== 'number' || !Number.isSafeInteger(id))) || !String(id).trim()) throw new Error('Missing or invalid record identity');
              if (ids.has(String(id))) throw new Error('Duplicate record identity: ' + String(id));
              ids.add(String(id));
              const record = step.each.fields ? Object.create(null) : row;
              if (step.each.fields) for (const key of step.each.fields) {
                if (Object.prototype.hasOwnProperty.call(row, key)) record[key] = row[key];
              }
              for (const key of step.unset || []) if (Object.prototype.hasOwnProperty.call(record, key)) throw new Error('Cannot set and remove the same field: ' + key);
              return {recordId: String(id), record: record, ...(step.mode ? {mode: step.mode} : {}), ...(step.unset && step.unset.length ? {unset: step.unset} : {})};
            });
            for (const write of writes) await targets[step.target].write(write);
            input = rows;
            output = {count: writes.length, records: writes};
          } else {
          const record = step.fields ? Object.create(null) : get(step.record);
          if (step.fields) for (const field of step.fields) record[field.key] = get(field.value);
          const identity = get(step.recordId);
          if ((typeof identity !== 'string' && (typeof identity !== 'number' || !Number.isSafeInteger(identity))) || !String(identity).trim()) throw new Error('Missing or invalid record identity');
          input = {recordId: String(identity), record: record, ...(step.mode ? {mode: step.mode} : {}), ...(step.unset && step.unset.length ? {unset: step.unset} : {})};
          if (!input.record || typeof input.record !== 'object' || Array.isArray(input.record)) throw new Error('record must be an object');
          for (const key of step.unset || []) if (Object.prototype.hasOwnProperty.call(record, key)) throw new Error('Cannot set and remove the same field: ' + key);
          for (const key of ['sample', 'time', 'quality']) if (step[key]) input[key] = get(step[key]);
          await targets[step.target].write(input);
          output = input;
          }
        }
      } catch (error) { throw new Error('Step ' + step.id + ': ' + error.message); }
      outputs[step.id] = output;
      if (context.dryRun) trace.push({id: step.id, input: input === undefined ? null : input, output: output, stopped: stopped});
      if (stopped) return context.dryRun ? {steps: trace, stoppedAt: step.id, result: null} : null;
      last = output;
    }
    return context.dryRun ? {steps: trace, result: last} : last;
  }`;

export function compileWorkflow(workflow: ProgramWorkflow): string {
  if (workflowIssues(workflow, false).length)
    return 'module.exports = function(api) { api.stream.onRequest({id:"trigger"}, function() { throw new Error("Incomplete workflow"); }); };';
  const sourceRefs = new Map<string, Set<string>>();
  const registerSource = (source: WorkflowSource, resource: boolean) => {
    if (!sourceRefs.has(source.id)) sourceRefs.set(source.id, new Set());
    if (resource) sourceRefs.get(source.id)!.add(source.resource);
  };
  if (workflow.trigger.kind === 'source-event' && workflow.trigger.source)
    registerSource(workflow.trigger.source, true);
  workflow.actions.forEach((step) => {
    if (step.kind === 'source' && step.source) registerSource(step.source, step.operation === 'read');
  });
  const lines = [
    '// Tier0 workflow v1. Edit in Steps; modifying this script switches to custom code.',
    'module.exports = function(api) {',
    `  const workflow = ${workflowJSON(workflow)};`,
    '  const sources = Object.create(null);',
    '  const targets = Object.create(null);',
  ];
  const scriptSteps = workflow.actions.filter((step) => step.kind === 'script');
  if (scriptSteps.length) {
    lines.push('  const scripts = Object.create(null);');
    for (const step of scriptSteps) {
      lines.push(`  scripts[${JSON.stringify(step.id)}] = async function(input) {\n${step.code}\n  };`);
    }
  }
  sourceRefs.forEach((resources, id) =>
    lines.push(
      `  sources[${JSON.stringify(id)}] = api.sources.use(${JSON.stringify({ id, resources: [...resources] })});`
    )
  );
  if (workflow.connections?.length) {
    lines.push('  const connections = Object.create(null);');
    for (const connection of workflow.connections) {
      // 每个引用单独声明资源，不能复用其他步骤合并后的资源范围。
      lines.push(
        `  connections[${JSON.stringify(connection.alias)}] = api.sources.use(${JSON.stringify({
          id: connection.id,
          resources: connection.mode === 'request' ? [] : [connection.resource],
        })});`
      );
    }
    lines.push('  Object.freeze(connections);');
  }
  const targets = new Set(workflow.actions.flatMap((step) => (step.kind === 'write' ? [step.target] : [])));
  targets.forEach((ref) => {
    const [kind, id] = ref.split(':');
    lines.push(
      `  targets[${JSON.stringify(ref)}] = api.uns.use(${JSON.stringify({ [`${kind}Id`]: id, access: 'write' })});`
    );
  });
  const trigger = workflow.trigger;
  const registration =
    trigger.kind === 'interval'
      ? `api.stream.onTimer(${JSON.stringify({ id: 'trigger', intervalMs: trigger.intervalMs })}, `
      : trigger.kind === 'source-event'
        ? `api.stream.onMessage({id: "trigger", source: sources[${JSON.stringify(trigger.source!.id)}], resourceRefs: ${JSON.stringify([trigger.source!.resource])}, qos: ${JSON.stringify(trigger.source!.qos ?? 0)}}, `
        : 'api.stream.onRequest({id: "trigger"}, ';
  lines.push(`  ${registration}${handlerRuntime(scriptSteps.length > 0)});`, '};');
  return lines.join('\n');
}

export function withWorkflow(document: ProgramDocument, workflow: ProgramWorkflow): ProgramDocument {
  return {
    ...document,
    script: compileWorkflow(workflow),
    extensions: { ...(object(document.extensions) ? document.extensions : {}), [extensionKey]: workflow },
  };
}
export function readWorkflow(document?: ProgramDocument): ProgramWorkflow | undefined {
  if (!document || !object(document.extensions)) return;
  const value = document.extensions[extensionKey];
  if (!workflowShape(value)) return;
  return value;
}

// 步骤定义是可视化流程的源文件；生成脚本只是执行产物，不用于推测编辑模式。
export function executableWorkflowDocument(document: ProgramDocument): ProgramDocument {
  const workflow = readWorkflow(document);
  return workflow ? withWorkflow(document, workflow) : document;
}

export function withoutWorkflow(document: ProgramDocument): ProgramDocument {
  const extensions = { ...(object(document.extensions) ? document.extensions : {}) };
  delete extensions[extensionKey];
  return { ...document, extensions };
}

// 只在用户编辑完整脚本时移除步骤定义，避免旧步骤覆盖用户代码。
export function editedProgramDocument(previous: ProgramDocument, next: ProgramDocument): ProgramDocument {
  if (previous.script === next.script) return next;
  const workflow = readWorkflow(next);
  return workflow && next.script !== compileWorkflow(workflow) ? withoutWorkflow(next) : next;
}
export function workflowThrough(workflow: ProgramWorkflow, stepId: string): ProgramWorkflow {
  return {
    ...workflow,
    actions: workflow.actions.slice(
      0,
      stepId === 'trigger' ? 0 : workflow.actions.findIndex((s) => s.id === stepId) + 1
    ),
  };
}

export function outputPaths(value: unknown, prefix = '', depth = 0): string[] {
  if (depth > 5 || value === null || typeof value !== 'object') return [];
  return Object.keys(value)
    .filter((key) => !unsafe.has(key) && !key.includes('.'))
    .slice(0, 50)
    .flatMap((key) => {
      const path = prefix ? `${prefix}.${key}` : key;
      return [path, ...outputPaths((value as Record<string, unknown>)[key], path, depth + 1)];
    })
    .slice(0, 200);
}
