// Public Record/Sample query grammar. Runtime compiles this bounded DSL to SQL.
const ref = name => ({$ref:`#/components/schemas/${name}`});
const object = (properties, required = []) => ({type:'object',additionalProperties:false,properties,required});
const array = (items, extra = {}) => ({type:'array',items,...extra});
const text = {type:'string'};
export function pipelineContract(schemas, operations) {
  schemas.UnsV2DataFilter = {type:'object',additionalProperties:true,description:'统一谓词：业务路径直接使用 field 或 customer.name；系统路径使用 _id、_meta.revision/createdAt/updatedAt/sourceId/sourceTime/locked。Sample 使用 _meta.recordId/time/receivedAt/quality/streamId/bindingId。直接值等同 $eq；支持标量比较、数组 $elemMatch/$all/$size、逻辑组合与受限 $expr。路径以点分段；字面点写 \\.、反斜杠写 \\\\，JSON 中反斜杠需再次转义；最多 512 字节/10 段，数组使用显式下标，不隐式穿透。字段类型按 Schema 校验，不接受 SQL 或脚本。Record 写入默认必须精确匹配一条。',example:{$and:[{_id:'example-record'},{'available':{$gte:20}}]}};
  schemas.UnsV2FilterScalar={oneOf:[{type:'string',nullable:true},{type:'number'},{type:'boolean'}]};
  const comparisons=Object.fromEntries(['$eq','$ne','$gt','$gte','$lt','$lte'].map(op=>[op,ref('UnsV2FilterScalar')]));
  schemas.UnsV2ArrayElementFilter={type:'object',minProperties:1,additionalProperties:true,description:'元素为标量时使用比较操作符；元素为对象时使用相对元素的字段条件，可嵌套逻辑组合。pull 不接受 $expr。'};
  schemas.UnsV2FieldComparisons={...object({...comparisons,$in:array(ref('UnsV2FilterScalar'),{minItems:1,maxItems:200}),$nin:array(ref('UnsV2FilterScalar'),{minItems:1,maxItems:200}),$exists:{type:'boolean'},$all:array({},{minItems:1,maxItems:200,description:'每一项按完整 JSON 元素相等匹配。'}),$size:{type:'integer',minimum:0},$elemMatch:ref('UnsV2ArrayElementFilter')}),minProperties:1};
  schemas.UnsV2FieldPredicate={...object({...structuredClone(schemas.UnsV2FieldComparisons.properties),$not:ref('UnsV2FieldComparisons')}),minProperties:1};
  schemas.UnsV2DataFilter.properties={$and:array(ref('UnsV2DataFilter'),{minItems:1}),$or:array(ref('UnsV2DataFilter'),{minItems:1}),$nor:array(ref('UnsV2DataFilter'),{minItems:1}),$not:ref('UnsV2DataFilter'),$expr:ref('UnsV2PipelineExpression')};
  schemas.UnsV2DataFilter.description+=' $eq:null 匹配 null 或缺失；$ne:null 匹配存在且非 null；$ne:非空值包含缺失。$exists 区分存在与缺失，显式 null 算存在。';
  schemas.UnsV2DataFilter.additionalProperties={oneOf:[ref('UnsV2FilterScalar'),ref('UnsV2FieldPredicate')]};
  schemas.UnsV2PipelineExpression={oneOf:[ref('UnsV2FilterScalar'),...['$ifNull','$add','$subtract','$multiply','$divide'].map(op=>object({[op]:array(ref('UnsV2PipelineExpression'),{minItems:2,maxItems:2})},[op])),object({$literal:ref('UnsV2FilterScalar')},['$literal'])],description:'$field 路径引用或标量常量；$literal 强制常量。算术仅数值，$divide 分母为 0 返回 null；$ifNull 显式提供缺失/null 默认值。'};
  const expr=ref('UnsV2PipelineExpression');
  const exprArray=(min,max)=>array(expr,{minItems:min,maxItems:max});
  const expressionOp=(name,value)=>object({[name]:value},[name]);
  schemas.UnsV2PipelineExpression.oneOf.push(
    ...['$eq','$ne','$gt','$gte','$lt','$lte'].map(op=>expressionOp(op,exprArray(2,2))),
    ...['$and','$or'].map(op=>expressionOp(op,exprArray(1,64))),expressionOp('$not',exprArray(1,1)),
    expressionOp('$cond',{oneOf:[exprArray(3,3),object({if:expr,then:expr,else:expr},['if','then','else'])]}),
    expressionOp('$concat',exprArray(1,64)),
    ...['$toLower','$toUpper','$strLenCP','$toString','$toDecimal','$toLong','$toBool'].map(op=>expressionOp(op,expr)),
    expressionOp('$trim',object({input:expr,chars:expr},['input'])),
    expressionOp('$convert',object({input:expr,to:{type:'string',enum:['string','decimal','long','bool']}},['input','to'])),
    expressionOp('$dateTrunc',object({date:expr,unit:{type:'string',enum:['millisecond','second','minute','hour','day','week','month','quarter','year']},timezone:{type:'string',default:'UTC'},binSize:{type:'integer',enum:[1],default:1},startOfWeek:{type:'string',enum:['sunday','monday','tuesday','wednesday','thursday','friday','saturday'],default:'sunday'}},['date','unit']))
  );
  schemas.UnsV2PipelineExpression.description+=' $expr 必须产生布尔值；$cond 条件必须为布尔值且两分支类型兼容。转换失败报错，缺失/null 传播。$trim input/chars 均为字符串表达式，省略 chars 裁剪空白。$dateTrunc 输入与返回均为 Unix 毫秒，timezone 为 IANA 时区，startOfWeek 仅用于 week。表达式深度最多 8，节点最多 512；每个 $concat 中间结果的 UTF-8 字节上限为 1 MiB，超出返回 EXPRESSION_BUDGET_EXCEEDED；不支持任意函数。';
  schemas.UnsV2PipelineAccumulator={oneOf:[...['$sum','$avg','$min','$max'].map(op=>object({[op]:ref('UnsV2PipelineExpression')},[op])),object({$count:{type:'object',maxProperties:0}},['$count'])]};
  schemas.UnsV2PipelineSort = array(array({oneOf:[{type:'string',minLength:1},{type:'integer',enum:[1,-1]}]},{minItems:2,maxItems:2}),{minItems:1,maxItems:4,description:'Nova ordered sort pairs: [[field, 1|-1], ...]. Server appends _id as a stable tie breaker.'});
  schemas.UnsV2DistinctBy={type:'object',minProperties:1,maxProperties:16,additionalProperties:{type:'string',minLength:2,maxLength:513,pattern:'^\\$[^$].*$',description:'单个 $ 前缀的已声明字段路径，例如 $status；不接受表达式或 $$ 变量。'},description:'输出别名到输入字段引用的映射。别名必须是合法顶层字段路径（转义规则相同），禁止空名、$ 前缀、控制字符及保留原型名。任一引用字段缺失则丢弃该输入行；显式 null 保留。数组按整体值比较，需元素去重时先 $unwind。'};
  schemas.UnsV2Distinct=object({by:ref('UnsV2DistinctBy')},['by']);
  schemas.UnsV2Distinct.description='按 by 指定的字段组合去重，输出各别名字段。结果使用普通 offset 分页；唯一值数量用 $distinct 后接 $count，不新增 countDistinct 阶段。';
  schemas.UnsV2Frequencies=object({by:{allOf:[ref('UnsV2DistinctBy')],not:{required:['count']},description:'沿用 distinct 的字段组合与缺失/null 规则；别名不能为 count。'}},['by']);
  schemas.UnsV2Frequencies.description='按 by 指定的字段组合统计频次，输出各别名字段和固定 count 字段。计数基于当前阶段输入；结果可继续排序或过滤。';
  const scalarOutput={type:'string',minLength:1,maxLength:512,description:'合法顶层输出字段名，遵循统一路径转义及保留名规则。'};
  const stages = {
    $match:ref('UnsV2DataFilter'),
    $project:{type:'object',minProperties:1,maxProperties:64,additionalProperties:ref('UnsV2PipelineExpression'),description:'字段包含/排除（1/0）或表达式；支持 field 嵌套输出路径，引用为 $field。不能混合包含与排除（_id 除外）。'},
    $set:{type:'object',minProperties:1,maxProperties:64,additionalProperties:ref('UnsV2PipelineExpression'),description:'添加或替换计算字段，支持嵌套路径，仅接受白名单表达式。'},
    $sort:ref('UnsV2PipelineSort'),
    $group:{type:'object',required:['_id'],properties:{_id:{anyOf:[ref('UnsV2PipelineExpression'),{type:'object',additionalProperties:ref('UnsV2PipelineExpression')}]}},additionalProperties:ref('UnsV2PipelineAccumulator'),description:'_id 指定单值、null 或对象分组键；其余字段支持 $sum/$avg/$min/$max。计数用 {$sum:1}。'},
    $distinct:ref('UnsV2Distinct'),
    $frequencies:ref('UnsV2Frequencies'),
    $lookup:{...object({collectionId:text,recordId:{type:'string',minLength:1,maxLength:128},kind:{type:'string',enum:['records','samples'],default:'records'},localField:text,foreignField:text,as:text,pipeline:array(ref('UnsV2PipelineStage'))},['collectionId','localField','foreignField','as']),oneOf:[{properties:{kind:{enum:['samples']}},required:['kind','recordId']},{properties:{kind:{enum:['records']}},not:{required:['recordId']}}],description:'kind=samples 必须提供固定 recordId，关联范围限定在该 Record；records 禁止 recordId。foreignField 需要 ready 的首字段 B-tree 索引或适用的内置身份索引。主输入最多 1000 行，每条输入关联原始行最多 10000，阶段中间结果最多 8 MiB，最终每条关联数组最多 200 项。超限明确失败，不静默截断；子 pipeline 可过滤或聚合。'},
    $unwind:{oneOf:[text,object({path:text,preserveNullAndEmptyArrays:{type:'boolean'}},['path'])]},
    $skip:{type:'integer',minimum:0,maximum:1000000},
    $limit:{type:'integer',minimum:1,maximum:1000000},
    $count:{...scalarOutput,description:scalarOutput.description+' 输出一行计数；空输入返回 0。作为末阶段时禁止显式 page，后面还有阶段时按最终结果正常分页。'},
    $exists:{...scalarOutput,description:scalarOutput.description+' 输出一行布尔值，判断当前阶段是否有输入行；空输入返回 false。作为末阶段时禁止显式 page，后面还有阶段时按最终结果正常分页。'},
  };
  schemas.UnsV2PipelineStage={oneOf:Object.entries(stages).map(([name,schema])=>object({[name]:schema},[name])),description:'每个阶段恰好一个操作符；顺序执行。$lookup 先等值关联，再运行其 pipeline，结果为数组；各目标集合单独鉴权。'};
  schemas.UnsV2PipelinePage=object({size:{type:'integer',minimum:1,maximum:200,default:20},mode:{type:'string',enum:['offset','cursor'],description:'仅由 $match/$sort 组成的流水线默认 cursor（keyset）；其他流水线默认 offset 且不支持 cursor。'},page:{type:'integer',minimum:1,default:1},cursor:{type:'string',description:'签名 keyset 令牌，保存完整排序值和唯一身份，绑定查询、集合、Schema/修订及调用身份，7 天有效；更换身份、Schema 或签名密钥后重新开始。不冻结跨请求快照。'} });
  schemas.UnsV2PipelinePage.allOf=[{not:{required:['cursor','page']}},{oneOf:[{properties:{mode:{enum:['cursor']}},required:['mode'],not:{required:['page']}},{properties:{mode:{enum:['offset']}}}]}];
  schemas.UnsV2PipelinePage.allOf.push({not:{required:['mode','cursor'],properties:{mode:{enum:['offset']}}}});
  schemas.UnsV2PipelinePage.properties.cursor.maxLength=32768;
  schemas.UnsV2DataQuery=object({queryId:{type:'string',minLength:1,maxLength:128},collectionId:{type:'string',minLength:1},pipeline:array(ref('UnsV2PipelineStage'),{maxItems:32}),resultMode:{type:'string',enum:['all','first'],default:'all'},page:ref('UnsV2PipelinePage')},['queryId','collectionId','pipeline']);
  schemas.UnsV2DataQuery.oneOf=[{properties:{resultMode:{enum:['first']}},required:['resultMode'],not:{required:['page']}},{properties:{resultMode:{enum:['all']}}}];
  schemas.UnsV2DataQuery.description='first 只返回完整流水线处理后的首项，无匹配为 null，不接受 page。all 默认返回分页列表，不是无限量取回全集。需要确定首项时显式排序并提供唯一同值排序依据。末阶段是 $count/$exists 时，first/all 均禁止显式 page（包括空对象），空输入仍返回一行 0/false；后面还有其他阶段时恢复普通分页规则。';
  schemas.UnsV2DataQueryReq=object({queries:array(ref('UnsV2DataQuery'),{minItems:1,maxItems:20})},['queries']);
  schemas.UnsV2DataQueryReq.description='同一请求最多 20 个 queryId 唯一的独立查询，使用同一只读数据库快照；任一失败整批失败。最多 2000 返回行、8 MiB 响应、15 秒执行预算。';
  schemas.UnsV2SampleQuery=structuredClone(schemas.UnsV2DataQuery);
  schemas.UnsV2SampleQuery.properties.recordId={type:'string',minLength:1,maxLength:128,description:'必填父 Record 身份，与 collectionId 共同确定采样范围；pipeline 只能进一步收窄，不能扩大范围。'};
  schemas.UnsV2SampleQuery.required.push('recordId');
  schemas.UnsV2SampleQuery.description+=' 每个 Sample query 必须指定外层 recordId；跨 Record 查询使用多条 queries。';
  schemas.UnsV2SampleQueryReq=object({queries:array(ref('UnsV2SampleQuery'),{minItems:1,maxItems:20})},['queries']);
  schemas.UnsV2SampleQueryReq.description=schemas.UnsV2DataQueryReq.description;
  schemas.UnsV2PipelinePageInfo=object({mode:{type:'string',enum:['cursor','offset']},hasMore:{type:'boolean'},nextCursor:{type:'string',nullable:true},nextPage:{type:'integer',nullable:true},snapshotScope:{type:'string',enum:['thisRequestOnly']}},['mode','hasMore','nextCursor','nextPage','snapshotScope']);
  const resultBase={queryId:text,resultMode:{type:'string'}};
  schemas.UnsV2DataQueryFirstResult=object({...resultBase,resultMode:{type:'string',enum:['first']},item:{type:'object',additionalProperties:true,nullable:true}},['queryId','resultMode','item']);
  schemas.UnsV2DataQueryAllResult=object({...resultBase,resultMode:{type:'string',enum:['all']},items:array({type:'object',additionalProperties:true}),pageInfo:ref('UnsV2PipelinePageInfo')},['queryId','resultMode','items','pageInfo']);
  schemas.UnsV2DataQueryResult={oneOf:[ref('UnsV2DataQueryFirstResult'),ref('UnsV2DataQueryAllResult')],discriminator:{propertyName:'resultMode',mapping:{first:'#/components/schemas/UnsV2DataQueryFirstResult',all:'#/components/schemas/UnsV2DataQueryAllResult'}}};
  schemas.UnsV2DataQueriesResult=object({results:array(ref('UnsV2DataQueryResult'))},['results']);
  for (const op of operations.filter(op=>['records','samples'].includes(op.name))) {
    op.response=`${op.handler}Response`;
    schemas[op.response]={...structuredClone(schemas.UnsV2Envelope),properties:{...structuredClone(schemas.UnsV2Envelope.properties),data:ref('UnsV2DataQueriesResult')}};
  }
  // Legacy fields remain executable for internal callers, but are not advertised.
  schemas.UnsV2Change.properties.filter=ref('UnsV2DataFilter');
  schemas.UnsV2TransactionQuery.properties.filter=ref('UnsV2DataFilter');
  schemas.UnsV2TransactionQuery.properties.sort=ref('UnsV2PipelineSort');
  delete schemas.UnsV2TransactionQuery.properties.filters;
  delete schemas.UnsV2TransactionQuery.properties.orderBy;
  schemas.UnsV2ResultBinding.properties.path.description='data.field DTO 路径、operations.N.value，或 filter/ 开头的 JSON Pointer（如 filter/available/$gte、filter/_id/$eq）。filter 指针只能替换已存在的标量/null 占位值，不能替换谓词结构。id 可绑定同 Collection 查询结果中的记录身份。';
  for(const name of ['UnsV2Change','UnsV2RecordChange']) {
    const shape=schemas[name];
    if(!shape)continue;
    shape.properties.filter=ref('UnsV2DataFilter');
    shape.properties.selection={type:'string',enum:['exactOne','first'],default:'exactOne',description:'仅 record.update/modify/replace。first 按 sort 选择第一条并在事务锁内重检；默认匹配多条报错。'};
    shape.properties.sort={allOf:[ref('UnsV2PipelineSort')],description:'仅 selection=first。服务端追加 _id 升序确保同值顺序稳定。'};
    shape.properties.returnDocument={type:'string',enum:['before','after'],description:'仅 record.update/modify/replace；返回 results[].document。省略则保持原回执，不返回文档。'};
    shape.allOf=[...(shape.allOf||[]),{anyOf:[{properties:{action:{enum:['record.update','record.modify','record.replace']}},required:['action']},{not:{anyOf:[{required:['selection']},{required:['sort']},{required:['returnDocument']}]}}]}];
    delete shape.properties.conditions;
  }
  for(const shape of [schemas.UnsV2Change,schemas.UnsV2RecordChange,...['UnsV2RecordUpdateReq','UnsV2RecordReplaceReq'].map(name=>schemas[name]?.properties.items?.items)].filter(Boolean)) {
    shape.allOf=[...(shape.allOf||[]),{anyOf:[{properties:{selection:{enum:['first']},filter:{allOf:[ref('UnsV2DataFilter')],minProperties:1}},required:['selection','filter']},{properties:{selection:{enum:['exactOne']}},not:{required:['sort']}}]}];
  }
}
