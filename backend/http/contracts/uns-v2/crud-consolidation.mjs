// 单条写入的返回策略属于同一事务，不另造一次查询或另一套写入语义。
export function applyCrudConsolidation(source, openapi) {
  const schemas = source.schemas;
  const operation = name => {
    const value = source.operations.find(item => item.name === name);
    if (!value) throw new Error(`Missing CRUD operation: ${name}`);
    return value;
  };
  const resolve = value => value?.$ref ? schemas[value.$ref.split('/').at(-1)] : value;
  for (const resource of ['record', 'metadata']) {
    for (const action of ['Update', 'Replace', 'Delete']) {
      const canonical = operation(`${resource}${action}One`);
      const legacy = operation(`${resource}FindOneAnd${action}`);
      const request = schemas[canonical.request];
      const legacyRequest = schemas[legacy.request];
      const modes = action === 'Delete' ? ['none', 'before'] : ['none', 'before', 'after'];
      request.properties.returnDocument = {
        type: 'string', enum: modes, default: 'none',
        description: 'none 仅返回原有计数；before/after 在同一写事务内附带 value，需要对应读取权限。未匹配返回 value:null；新增时 before 为 null。',
      };
      request.properties.projection = structuredClone(legacyRequest.properties.projection);
      request.allOf = [...(request.allOf || []), {anyOf: [
        {not: {required: ['projection']}},
        {required: ['returnDocument'], properties: {returnDocument: {enum: modes.filter(mode => mode !== 'none')}}},
      ]}];
      const result = resolve(schemas[canonical.response].properties.data);
      const legacyResult = resolve(schemas[legacy.response].properties.data);
      result.properties.value = structuredClone(legacyResult.properties.value);
      result.properties.value.description = '仅在 returnDocument=before/after 时出现；原有计数与状态字段保持不变。';
      Object.assign(openapi.components.schemas, {[canonical.request]: request, [canonical.response]: schemas[canonical.response]});
    }
  }
  const insert = operation('recordInsertOne');
  const insertRequest = schemas[insert.request];
  insertRequest.properties.returnDocument = {
    type: 'string', enum: ['none', 'after'], default: 'none',
    description: 'none 保留原有 insertedId 响应；after 在同一写事务内附带创建后的 value，需要对应读取权限。幂等重放返回原始创建快照，dryRun 返回未提交的预览。',
  };
  insertRequest.properties.projection = structuredClone(schemas[operation('recordUpdateOne').request].properties.projection);
  insertRequest.allOf = [...(insertRequest.allOf || []), {anyOf: [
    {not: {required: ['projection']}},
    {required: ['returnDocument'], properties: {returnDocument: {enum: ['after']}}},
  ]}];
  const insertResult = resolve(schemas[insert.response].properties.data);
  insertResult.properties.value = {$ref: '#/components/schemas/RecordDocument',
    description: '仅在 returnDocument=after 时出现；同一写事务的创建快照，包含服务端 ID、revision 和时间戳。重放不重新读取最新记录。'};
  Object.assign(openapi.components.schemas, {[insert.request]: insertRequest, [insert.response]: schemas[insert.response]});
  const get = operation('recordGet');
  schemas[get.request].description += ' 投影后的响应最多 8 MiB；超限返回 413 RESPONSE_TOO_LARGE，请减少 ids 或 projection 字段。';
  Object.assign(openapi.components.schemas, {[get.request]: schemas[get.request]});
  const aggregate = operation('metadataAggregate');
  const metadataPipeline = schemas[aggregate.request].properties.pipeline;
  const distinctStage = {type: 'object', additionalProperties: false,
    properties: {$distinct: {type: 'string', minLength: 1, maxLength: 128,
      description: '字段路径；排除缺失值，保留显式 null，按 JSONB 值排序。输出 items 每项为 {value:...}。'}}, required: ['$distinct']};
  metadataPipeline.items.oneOf = [...metadataPipeline.items.oneOf.filter(stage => !stage.properties.$distinct), distinctStage];
  metadataPipeline.description = '支持 $match/$project/$group/$sort/$skip/$limit/$distinct；$distinct 按 JSONB 值排序并使用外层分页。';
  Object.assign(openapi.components.schemas, {[aggregate.request]: schemas[aggregate.request]});
  source.cloudContractRevision += '+crud-consolidation/1+record-insert-return/1';
}
