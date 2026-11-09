// 已退役入口只用于生成时核对替代能力，不进入任何执行注册表或公共契约。
export const retiredOperations = {
  recordFindOneAndUpdate: ['recordUpdateOne', 'returnDocument'],
  recordFindOneAndReplace: ['recordReplaceOne', 'returnDocument'],
  recordFindOneAndDelete: ['recordDeleteOne', 'returnDocument'],
  recordInsertMany: ['recordBulkWrite', 'bulkInsert'],
  recordDistinct: ['recordAggregatePipeline', 'aggregateDistinct'],
  metadataFindOneAndUpdate: ['metadataUpdateOne', 'returnDocument'],
  metadataFindOneAndReplace: ['metadataReplaceOne', 'returnDocument'],
  metadataFindOneAndDelete: ['metadataDeleteOne', 'returnDocument'],
  metadataFindOne: ['metadataFind', 'findOne'],
  metadataInsertMany: ['metadataBulkWrite', 'bulkInsert'],
  metadataDistinct: ['metadataAggregate', 'aggregateDistinct'],
  sourceList: ['sourceDocumentList', 'sourceDocument'],
  sourceGet: ['sourceDocumentGet', 'sourceDocument'],
  sourceCreate: ['sourceDocumentCreate', 'sourceDocument'],
  sourceUpdate: ['sourceDocumentUpdate', 'sourceDocument'],
  sourceConnectionUpdate: ['sourceDocumentUpdate', 'sourceDocument'],
  streamProgramValidate: ['streamDocumentValidate', 'streamDocument'],
  streamProgramCreate: ['streamDocumentCreate', 'streamDocument'],
  streamProgramUpdate: ['streamDocumentUpdate', 'streamDocument'],
  streamProgramGet: ['streamDocumentGet', 'streamDocument'],
  streamProgramDisable: ['streamDocumentDisable', 'streamDocument'],
};

export const starterOperations = new Set([
  'catalog', 'dataAppContext', 'appCollectionsEnsure', 'recordGet', 'recordFind',
  'recordCount', 'recordInsertOne', 'recordUpdateOne', 'recordDeleteOne',
  'recordBulkWrite', 'recordTransactionExecute', 'recordSubscribe',
  'sampleAppend', 'sampleFind', 'sampleLatestValues',
]);

export function operationAudience(op) {
  if (['controlGrantGet', 'controlGrantSet', 'appStreamAccessList', 'appCollectionsList'].includes(op.name) || op.resource === 'app') return 'governance';
  if (/Observation|RunsList|History|Runtime|Probe|SampleTest/.test(op.name) || /^control(Discover|Describe|Invoke)$/.test(op.name)) return 'operations';
  if (['appCollectionsSchemaReplace', 'appCollectionsArchive', 'appCollectionsRestore'].includes(op.name)) return 'platform';
  if (op.resource === 'record' || op.resource === 'sample' || starterOperations.has(op.name) || op.name === 'jobs') return 'app';
  return 'builder';
}

export function applyApiSurface(source, openapi) {
  const byName = new Map(source.operations.map(op => [op.name, op]));
  for (const [name, [replacement]] of Object.entries(retiredOperations)) {
    const op = byName.get(name), target = byName.get(replacement);
    if (!op || !target || op.coreOnly || target.coreOnly) throw new Error(`Invalid retired operation replacement: ${name}`);
  }
  source.operations = source.operations.filter(op => !Object.hasOwn(retiredOperations, op.name));
  for (const op of source.operations) {
    op.audience = operationAudience(op);
    op.starter = starterOperations.has(op.name);
  }
  for (const schemas of [source.schemas, openapi.components.schemas]) {
    const catalog = schemas.UnsV2CatalogReq;
    if (!catalog?.properties) throw new Error('Missing catalog request');
    delete catalog.properties.includeCompatibility;
  }
  source.cloudContractRevision += '+api-surface/2';
}

export function decorateApiSurface(spec, operations) {
  for (const op of operations.filter(op => !op.coreOnly)) {
    const path = op.suffix.replace(/\/:([A-Za-z][A-Za-z0-9]*)(?=\/|$)/g, '/{$1}');
    const endpoint = spec.paths[path]?.[(op.method || 'POST').toLowerCase()];
    if (!endpoint) throw new Error(`Missing public surface: ${op.name}`);
    endpoint['x-tier0-audience'] = op.audience;
    endpoint['x-tier0-starter'] = op.starter;
  }
}
