import type { ProgramDocument, ProgramManifest, Source } from '@/apis/core-api/source-stream';

export const emptyProgram = `module.exports = function (api) {
  api.stream.onRequest({ id: 'request' }, async function (context) {
    const input = context.input;
    api.log.info('request received');
    return input;
  });
};`;

export type SourceInsertionMode = 'read' | 'source-event' | 'write' | 'invoke' | 'publish' | 'request' | 'http-server';

/** 与宿主的协议操作限制一致；不要为 MQTT 生成不存在的 read 方法。 */
export function sourceInsertionModes(source: Source, ref: string): SourceInsertionMode[] {
  if (source.document.sourceType) {
    const resource = source.resources.find((item) => item.id === ref);
    if (!resource || resource.metadata?.sourceType !== source.document.sourceType) return [];
    return resource.metadata.readOnly === true ? ['read'] : resource.metadata.readOnly === false ? ['invoke'] : [];
  }
  if (source.protocol === 'http-api' || source.protocol === 'oauth2')
    return source.document.role === 'server' ? ['http-server'] : ['request'];
  const resource = source.resources.find((item) => item.id === ref);
  if (!resource) return [];
  const modes: SourceInsertionMode[] = [];
  const supports = (model: string) => resource.models.includes(model as 'publish' | 'subscribe' | 'request-reply');
  const nodeClass = resource.metadata?.nodeClass;
  if (supports('publish')) {
    if (source.protocol === 'modbus-tcp' || (source.protocol === 'opcua' && nodeClass === 2)) modes.push('read');
    modes.push('source-event');
  }
  if (supports('subscribe')) {
    if (source.protocol === 'mqtt' && ref.startsWith('mqtt:') && !/[+#]/.test(ref)) modes.push('publish');
    if (source.protocol === 'opcua' && nodeClass === 2) modes.push('write');
    if (source.protocol === 'modbus-tcp') {
      const range = source.document.modbus?.ranges?.find((item) => ref === `modbus:${item.id}`);
      if (
        range &&
        range.writable !== false &&
        ((range.table === 'holding-register' && range.quantity <= 123) ||
          (range.table === 'coil' && range.quantity <= 1968))
      )
        modes.push('write');
    }
  }
  if (supports('request-reply')) {
    if (
      source.protocol === 'mqtt' &&
      ref === 'mqtt-request-reply' &&
      source.document.mqtt?.requestTopic &&
      source.document.mqtt.replyTopic &&
      source.document.mqtt.correlationField
    )
      modes.push('invoke');
    if (source.protocol === 'opcua' && nodeClass === 4 && source.document.role !== 'server') modes.push('invoke');
  }
  return modes;
}

export const registrationSnippet = (body: string) => `(function (previous) {
  module.exports = function (api) {
    previous(api);
${body}
  };
})(module.exports);`;

export function internalResourceConfig(reference: string, access: 'read' | 'write'): string {
  const match = /^(topic|collection):([A-Za-z0-9][A-Za-z0-9_.:-]{0,127})$/.exec(reference);
  if (match) return `${match[1]}Id: ${JSON.stringify(match[2])}, space: "metadata", access: ${JSON.stringify(access)}`;
  return `topicId: ${JSON.stringify(reference)}`;
}

/** Mirror the compiled SDK reference key, not an untyped raw storage ID. */
export function programResourceKey(reference: ProgramManifest['topics'][number]): string {
  if (reference.collectionId) return `collection:${reference.collectionId}`;
  const id = reference.topicId || '';
  const canonicalUUID =
    /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(id) &&
    id !== '00000000-0000-0000-0000-000000000000';
  const metadata = reference.space === 'metadata' || (!reference.space && !/^\d+$/.test(id));
  return metadata && !canonicalUUID ? `metadata-topic:${id}` : id;
}

function sourceRegistration(
  source: Source,
  ref: string,
  mode: SourceInsertionMode,
  entryId: string
): string | undefined {
  if (!sourceInsertionModes(source, ref).includes(mode)) return;
  if (source.document.sourceType) {
    const reference = JSON.stringify(ref);
    const result =
      mode === 'read'
        ? `await source.read(${reference}, context.input || {})`
        : `await source.invoke(${reference}, context.input || {})`;
    return `    const source = api.sources.use({ id: ${JSON.stringify(String(source.id))}, resources: [${reference}] });
    // Manual entry. Source owns authentication, cookies, signing and connection reuse.
    // Input is { params: {}, query: {}, data: {} }; use the discovered native contract.
    api.stream.onRequest({ id: ${JSON.stringify(entryId)} }, async function (context) {
      return ${result};
    });`;
  }
  if (mode === 'request' || mode === 'http-server') {
    const declaration =
      mode === 'http-server'
        ? `onRequest({ id: ${JSON.stringify(entryId)}, source: source, method: 'POST', path: '/ingress' }`
        : `onRequest({ id: ${JSON.stringify(entryId)} }`;
    const body =
      mode === 'http-server'
        ? `      return { status: 200, headers: { 'Content-Type': 'application/json' }, body: { received: context.request.body } };`
        : `      const parameters = await source.parameters();
      // Relative path only; authentication and connections are managed by Source.
      const response = await source.request({ method: 'GET', path: String(parameters.path || '/') });
      return { status: response.status, data: response.data };`;
    return `    const source = api.sources.use({ id: ${JSON.stringify(String(source.id))}, resources: [] });
    api.stream.${declaration}, async function (context) {
${body}
    });`;
  }
  const reference = JSON.stringify(ref);
  const qos = source.protocol === 'mqtt' ? (source.document.mqtt?.qos ?? 0) : 0;
  const declaration =
    mode === 'source-event'
      ? `onMessage({ id: ${JSON.stringify(entryId)}, source: source, resourceRefs: [${reference}], qos: ${qos} }`
      : `onRequest({ id: ${JSON.stringify(entryId)} }`;
  const result =
    mode === 'source-event' ? 'input' : `await source.${mode}(${reference}${mode === 'read' ? '' : ', input'})`;
  const inputHint =
    mode === 'write'
      ? source.protocol === 'opcua'
        ? '      // Request input: { value: ... }\n'
        : '      // Request input: { values: [...] } matching the configured range quantity.\n'
      : mode === 'invoke' && source.protocol === 'opcua'
        ? '      // Request input: { arguments: [...] } matching the native Method signature.\n'
        : '';
  return `    const source = api.sources.use({ id: ${JSON.stringify(String(source.id))}, resources: [${reference}] });
    api.stream.${declaration}, async function (context) {
      const input = context.input;
${inputHint}      return ${result};
    });`;
}

export function sourceInsertionSnippet(
  source: Source,
  ref: string,
  mode: SourceInsertionMode,
  entryId: string
): string | undefined {
  const body = sourceRegistration(source, ref, mode, entryId);
  return body && registrationSnippet(body);
}

export function entryInsertionSnippet(
  kind: 'request' | 'interval' | 'uns-event',
  topics: string[],
  entryId: string
): string {
  const handles = topics
    .map((id, i) => `    const topic${i} = api.uns.use({ ${internalResourceConfig(id, 'read')} });`)
    .join('\n');
  const declaration =
    kind === 'request'
      ? `onRequest({ id: ${JSON.stringify(entryId)} }`
      : kind === 'interval'
        ? `onTimer({ id: ${JSON.stringify(entryId)}, intervalMs: 5000 }`
        : `onEvent({ id: ${JSON.stringify(entryId)}, topics: [${topics.map((_, i) => `topic${i}`).join(', ')}] }`;
  return registrationSnippet(`${kind === 'uns-event' ? `${handles}\n` : ''}    api.stream.${declaration}, async function (context) {
      return context.input;
    });`);
}

export function topicInsertionSnippet(topics: string[], entryId: string): string {
  const handles = topics
    .map((id, i) => `    const topic${i} = api.uns.use({ ${internalResourceConfig(id, 'write')} });`)
    .join('\n');
  const writes = topics
    .map((id, i) =>
      /^\w+:/.test(id)
        ? `      // Input: { recordId: 'stable-business-id', record: {...}, sample?: {...}, time?: milliseconds, quality?: 'Good' }.\n      await topic${i}.write(input);`
        : `      await topic${i}.write(input);`
    )
    .join('\n');
  return registrationSnippet(`${handles}
    api.stream.onRequest({ id: ${JSON.stringify(entryId)} }, async function (context) {
      const input = context.input;
${writes}
      return input;
    });`);
}

export function sourceProgram(source: Source): string {
  if (['http-api', 'oauth2'].includes(source.protocol) && !source.document.sourceType)
    return `module.exports = function (api) {
${sourceRegistration(source, '', source.document.role === 'server' ? 'http-server' : 'request', source.document.role === 'server' ? 'ingress' : 'request')}
};`;
  // Prefer a bounded read, never an alphabetically first write or unfiltered jobs list.
  const vendorReads: Record<string, string> = {
    'pi-web-api': 'servers',
    'plex-mes': 'workcenters',
    'wms-rest': 'inventory',
    'sap-odata': 'service',
    'yonyou-yonbip': 'materials',
    'kingdee-jdy': 'materials',
    ngp: 'materials',
    'kingdee-k3cloud': 'query',
    salesforce: 'versions',
    teamcenter: 'queries',
    'hikvision-isapi': 'device',
  };
  const preferredRef = source.document.sourceType
    ? `vendor:${source.document.sourceType}:${vendorReads[source.document.sourceType]}`
    : undefined;
  const resource =
    source.resources.find(
      (item) => item.id === preferredRef && sourceInsertionModes(source, item.id).includes('read')
    ) ||
    (source.document.sourceType
      ? source.resources.find((item) => sourceInsertionModes(source, item.id).includes('read'))
      : source.resources.find((item) => sourceInsertionModes(source, item.id).length > 0));
  if (!resource)
    return `// Discover a supported resource in Source ${source.id}, then declare its stable ID:
// const source = api.sources.use({ id: ${JSON.stringify(String(source.id))}, resources: ['RESOURCE_ID'] });
${emptyProgram}`;
  const mode = sourceInsertionModes(source, resource.id)[0];
  return `module.exports = function (api) {
${sourceRegistration(source, resource.id, mode, mode === 'source-event' ? 'message' : mode)}
};`;
}

export function initialProgram(source?: Source, topicId?: number | string): ProgramDocument {
  return {
    name: '',
    description: '',
    timeoutMs: 5000,
    script: source
      ? sourceProgram(source)
      : topicId
        ? `module.exports = function (api) {
  const topic = api.uns.use({ ${internalResourceConfig(String(topicId), 'write')} });
  api.stream.onRequest({ id: 'write' }, async function (context) {
    const input = context.input;
    // Canonical resources require { recordId, record, sample?, time?, quality? }.
    await topic.write(input);
    return input;
  });
};`
        : emptyProgram,
  };
}

/** Only current fields are writable; opaque business metadata belongs in extensions. */
export function readProgramDocument(raw: string): ProgramDocument | undefined {
  try {
    const value: unknown = JSON.parse(raw);
    if (!value || typeof value !== 'object' || Array.isArray(value)) return;
    const document = value as Record<string, unknown>;
    if (
      Object.keys(document).some(
        (key) => !['name', 'description', 'script', 'timeoutMs', 'extensions'].includes(key)
      ) ||
      typeof document.name !== 'string' ||
      (document.description !== undefined && typeof document.description !== 'string') ||
      typeof document.script !== 'string' ||
      !Number.isInteger(document.timeoutMs) ||
      Number(document.timeoutMs) < 1 ||
      Number(document.timeoutMs) > 5000
    )
      return;
    return { ...document, description: document.description ?? '' } as ProgramDocument;
  } catch {
    return;
  }
}

export function sampleInput(manifest: ProgramManifest, entryId?: string): string {
  const entry =
    manifest.entries.find((item) => item.id === entryId) ??
    (manifest.entries.length === 1 ? manifest.entries[0] : undefined);
  const request = entry?.kind === 'http-request' ? entry : undefined;
  const resourceRef = entry?.resourceRefs?.length === 1 ? entry.resourceRefs[0] : '';
  return JSON.stringify(
    {
      data: request
        ? {
            method: request.method,
            path: request.path,
            params: {},
            query: {},
            headers: {},
            body: {},
            requestId: 'mock-request',
          }
        : {},
      ...(entry?.kind === 'source-event'
        ? { event: { resourceRef: resourceRef.startsWith('mqtt:') && /[+#]/.test(resourceRef) ? '' : resourceRef } }
        : {}),
      sourceSamples: Object.fromEntries(
        manifest.sources.map((source) => [
          source.id,
          source.resources.length
            ? Object.fromEntries(
                source.resources.map((ref) => [
                  ref,
                  ref.startsWith('vendor:') ? { status: 200, headers: {}, data: [], pagination: null } : {},
                ])
              )
            : { parameters: {}, secrets: {}, request: { status: 200, headers: {}, data: {} } },
        ])
      ),
      topicSamples: Object.fromEntries(manifest.topics.map((topic) => [programResourceKey(topic), {}])),
      checkpointSample: null,
    },
    null,
    2
  );
}

export function appendProgramSnippet(document: ProgramDocument, snippet: string): ProgramDocument {
  // A registration block preserves existing source without scanning or rewriting arbitrary JavaScript.
  return { ...document, script: `${document.script}\n\n${snippet}` };
}

export function manifestIDs(manifest?: ProgramManifest): { sources: number[]; topics: string[] } {
  return {
    sources: (manifest?.sources ?? []).map((s) => Number(s.id)).filter((id) => Number.isSafeInteger(id) && id > 0),
    // Meta resource IDs and historic UNS IDs have distinct contracts; keep references opaque.
    topics: (manifest?.topics ?? []).map((t) => t.topicId || t.collectionId).filter((id): id is string => Boolean(id)),
  };
}
