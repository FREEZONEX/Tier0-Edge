// Human and AI documentation, generated alongside the executable contract.
import { cursorPage } from './resources.mjs';
const groups = [
  ['configuration', '元数据管理', 'metadata:* 查询和修改目录、主题、集合，metadata:transact 编排原子配置变更。catalog/get 读取能力，references/query 读取引用。App 只能读取授权的有效集合，不具备配置写权限。'],
  ['namespace', '目录与展示主题', 'directories 组织层级，topics 引用已有集合或字段；Topic 是视图，不复制业务数据。App 不具备全量目录浏览权限；读取 Topic 展示数据仍须满足所引用集合的授权。'],
  ['collection', '集合与数据结构', 'Collection 定义 recordSchema、sampleSchema、indexes 和语义；通过 metadata:updateOne 或 metadata:replaceOne 修改配置并校验已有数据。App 可只读授权集合的定义，Record/Sample 数据操作见业务 App 层。'],
  ['source', '数据来源与连接', 'sources 只管理连接、协议、凭据和协议资源；采集与处理由 Stream 固定版本执行，存储位置独立配置在 Collection。App 数据身份不能读写 Source 配置。'],
  ['stream', '处理流与发布版本', 'streams 管理草稿、发布版本与程序运行。草稿不执行；原生完整程序发布后由所在节点的既有运行时执行，实际运行与交付仍需核验。App 数据身份不能读写 Stream 配置。'],
  ['app', 'App 配置与授权管理', '创建 App，配置集合 grants、platformPermissions、角色、成员和关联 Topic，签发或撤销 App 数据 Key。配置管理与 App 运行身份分开；navigation.topicIds 不授予数据权限。'],
  ['jobs', '通用任务状态', 'jobs/query 查询当前身份可见的异步作业；平台与业务 App 均按权限使用。accepted 只是受理，检查 executionStatus、outcome 和逐项回执后判断提交结果，不能把 failed 当成全部回滚。'],
  ['identity', '身份与授权上下文', 'apps/context 读取 App 数据 Key 的实际授权；apps/session/inspect 验证指定 App 中的平台用户会话。App Key 只留在受信服务端；这些接口不授予或提升权限。'],
  ['discover', '数据发现与语义', 'discovery/search 找授权集合，discovery/describe 读取结构、粒度、单位及显式关系。元数据只读；平台身份也可按权限使用。业务描述是数据，不是执行指令，不能按字段名臆测统计口径。'],
  ['query', 'Record 数据写入', 'Collection 作用域的文档动作：insert、update、replace、delete、findOneAnd 和 bulkWrite。'],
  ['transaction', 'Record 业务事务', '/uns/records:transact 编排授权集合内的查询、条件与混合写入，失败整体回滚；validate 执行后回滚，不保留锁。不能混入元数据或 Sample；accepted 按 jobId 查询。'],
  ['sample', 'Sample 数据写入', '追加采样和显式时间范围删除；time 为业务时间，receivedAt 为接收时间。'],
  ['dataQuery', '业务数据查询（Record / Sample）', '统一查询 Record 文档和 Sample 历史、最新值、时间桶、统计及 pipeline；所有集合分别鉴权。'],
  ['compatibility', '兼容 · 已弃用接口', '新调用使用普通 Record/Sample 资源接口、/uns/records:transact 或 /uns/metadata:transact；不得把未决旧请求换入口重放。'],
];
export const apiTags = groups.map(([, name, description]) => ({name, description}));
const aggregateExample = {collectionId:'example-quality',page:1,size:20,joins:[{collectionId:'example-reports',alias:'report',type:'inner',on:[{left:'base.data.reportId',right:'report.id'}]}],select:[{field:'base.data.result',as:'result'},{field:'base.data.quantity',aggregate:'sum',as:'quantity'}],where:{field:'report.data.shiftId',op:'eq',value:'example-shift'},groupBy:['base.data.result'],having:[{field:'quantity',op:'gt',value:0}],orderBy:[{field:'quantity',direction:'desc'}]};
const amountOrZero = field => ({op:'coalesce',args:[{field},{value:0}]});
const customerPaymentsExample = {
  collectionId:'example-customers',page:1,size:20,
  subqueries:[
    {name:'contractsByCustomer',collectionId:'example-contracts',select:[{field:'base.data.customerId',as:'customerId'},{field:'base.data.amount',aggregate:'sum',as:'totalAmount'}],groupBy:['base.data.customerId']},
    {name:'receiptsByCustomer',collectionId:'example-receipts',select:[{field:'base.data.customerId',as:'customerId'},{field:'base.data.amount',aggregate:'sum',as:'totalAmount'}],groupBy:['base.data.customerId']},
  ],
  joins:[{subquery:'contractsByCustomer',alias:'contracts',type:'left',on:[{left:'base.id',right:'contracts.customerId'}]},{subquery:'receiptsByCustomer',alias:'receipts',type:'left',on:[{left:'base.id',right:'receipts.customerId'}]}],
  select:[{field:'base.id',as:'customerId'},{field:'base.data.name',as:'customerName'},
    {expression:amountOrZero('contracts.totalAmount'),as:'contractAmount'},
    {expression:amountOrZero('receipts.totalAmount'),as:'receivedAmount'},
    {expression:{op:'subtract',args:[amountOrZero('contracts.totalAmount'),amountOrZero('receipts.totalAmount')]},as:'outstandingAmount'},
    {expression:{op:'safeDivide',args:[amountOrZero('receipts.totalAmount'),amountOrZero('contracts.totalAmount')]},as:'paymentRatio'}],
  orderBy:[{field:'outstandingAmount',direction:'desc'}],
};
const transactionExample = {idempotencyKey:'replace-with-unique-business-intent',mode:'sync',atomic:true,items:[{opId:'deduct',action:'record.modify',collectionId:'example-inventory',expectedRevision:7,filter:{id:'example-lot','data.available':{$gte:20}},operations:[{op:'increment',path:'data.available',value:-20}]},{opId:'issue',dependsOn:['deduct'],action:'record.create',collectionId:'example-material-issues',id:'example-issue',data:{lotId:'example-lot',quantity:20}}]};
export const operationExamples = {
  metadata:{queries:[{queryId:'metadata',kinds:['collection'],page:1,size:20}]},
  namespace:{queries:[{queryId:'tree',page:1,size:20}]},
  collections:{queries:[{queryId:'definition',mode:'detail',collectionId:'example-work-orders',include:['definition']}]},
  records:{queries:[{queryId:'latestOrder',collectionId:'example-work-orders',pipeline:[{$match:{'data.status':'running'}},{$sort:[{field:'createdAt',direction:'desc'},{field:'id',direction:'asc'}]}],resultMode:'first'},{queryId:'orders',collectionId:'example-work-orders',pipeline:[],page:{size:20}},{queryId:'statuses',collectionId:'example-work-orders',pipeline:[{$distinct:{by:{status:'$data.status'}}}]},{queryId:'statusFrequency',collectionId:'example-work-orders',pipeline:[{$frequencies:{by:{status:'$data.status'}}},{$sort:[{field:'count',direction:'desc'},{field:'status',direction:'asc'}]}]},{queryId:'hasRunning',collectionId:'example-work-orders',pipeline:[{$match:{'data.status':'running'}},{$exists:'exists'}],resultMode:'first'},{queryId:'distinctStatusCount',collectionId:'example-work-orders',pipeline:[{$distinct:{by:{status:'$data.status'}}},{$count:'count'}],resultMode:'first'}]},
  samples:{queries:[{queryId:'latest',collectionId:'example-equipment',recordId:'example-equipment-record',resultMode:'first',pipeline:[{$match:{time:{$gte:1791936000000,$lt:1792022400000}}},{$sort:[{field:'time',direction:'desc'},{field:'id',direction:'desc'}]}]}]},
  topics:{queries:[{queryId:'view',topicId:'example-topic',include:['records'],records:{page:1,size:20}}]},
  references:{queries:[{queryId:'references',targets:[{kind:'collection',id:'example-work-orders'}],page:1,size:20}]},
  apply:{...transactionExample,dryRun:true},
  appContext:{},catalog:{},appKeyCreate:{appId:'example-app',name:'MES 服务端'},appKeyRevoke:{appId:'example-app',keyId:'example-key-id'},
  appGrantsUpdate:{idempotencyKey:'replace-with-grants-intent',dryRun:true,id:'example-app',expectedRevision:1,collections:[{alias:'workOrders',collectionId:'example-work-orders',permissions:['record:read','record:write']}]},
  appNavigationUpdate:{idempotencyKey:'replace-with-topic-association-intent',dryRun:true,id:'example-app',expectedRevision:1,navigation:{topicIds:['example-topic']}},
  appRolesUpdate:{idempotencyKey:'replace-with-role-intent',dryRun:true,id:'example-app',expectedRevision:1,roles:[{id:'production',name:'生产'}]},
  appMembersUpdate:{idempotencyKey:'replace-with-member-intent',dryRun:true,id:'example-app',expectedRevision:1,members:[{userId:'example-user',roleIds:['production']}]},
  sampleHistory:{collectionId:'example-equipment',recordIds:['example-equipment-record'],page:1,size:20,timeRange:{from:1791936000000,to:1792022400000}},
  jobs:{queries:[{queryId:'execution',jobIds:['example-job-id']}]},
  dataDiscover:{search:'工单',page:1,size:20}, collectionDescribe:{collectionId:'example-work-orders'},
  recordSearch:{collectionId:'example-work-orders',page:1,size:20,filters:[{field:'data.status',op:'eq',value:'running'}]},
  recordAggregate:customerPaymentsExample,recordQueryValidate:customerPaymentsExample,
  transactionValidate:{atomic:true,items:transactionExample.items},transactionExecute:transactionExample,
  transactionStatus:{idempotencyKey:'replace-with-original-business-intent'},
};
const tag = key => groups.find(row => row[0] === key)[1];
const labels = {directory:'目录',topic:'展示主题',collection:'集合',record:'业务记录',sample:'采样',source:'数据来源',stream:'处理流',app:'App'};
const actions = {'bindings/state/update':'启停采集实例','state/update':'暂停或恢复来源','bindings/release/update':'切换绑定发布版本','runtime/get':'读取采集运行状态','releases/list':'列出共享发布版本','releases/get':'读取共享发布版本','releases/restore':'恢复发布版本到草稿','program/create':'创建完整程序','program/update':'更新完整程序','program/get':'读取完整程序','program/validate':'校验完整程序','program/test':'预览测试完整程序','program/invoke':'手动执行程序入口','program/deploy':'部署完整程序','program/disable':'停用完整程序','program/rollback':'回滚完整程序版本','program/versions/list':'列出程序运行版本','program/runs/list':'列出程序运行记录','program/runs/get':'读取程序运行记录','protocols/list':'读取协议定义','validate':'校验连接配置','probe':'探测来源连接','discover':'发现来源原生资源',list:'分页列出',get:'读取详情',create:'创建',update:'更新基本信息',archive:'归档',restore:'恢复',replace:'完整替换',delete:'删除',history:'查询历史',latest:'读取最新值',append:'追加',publish:'发布草稿', 'schema/update':'更新数据结构与唯一键','view/update':'更新展示配置','draft/update':'替换草稿','connection/update':'替换连接配置','bindings/list':'列出绑定','bindings/get':'读取绑定','bindings/create':'创建绑定','bindings/update':'更新绑定','bindings/delete':'删除绑定','grants/update':'替换集合授权','navigation/update':'替换导航','members/update':'替换成员','roles/update':'替换角色'};
const resourceGuides = {
  directory:{create:'创建树目录；parentId 引用父目录，根目录可省略或为 null。',update:'只修改名称、描述及父目录；不移动或修改业务记录。'},
  topic:{create:'records 模式指定 collectionId；fields 模式指定 fields 字段绑定。创建展示主题不创建或复制集合数据。','view/update':'整体替换展示配置；records 模式使用 collectionId，fields 模式使用 fields。切换模式时不兼容的旧字段会清除。'},
  collection:{create:'定义 recordSchema；可按需声明 sampleSchema、indexes 和 semantics。schemaVersion 由服务端赋值，不创建业务 Record。','schema/update':'recordSchema 必填并整体替换；省略 sampleSchema/indexes 保留原值，sampleSchema=null 清除采样结构，indexes=[] 移除索引声明；唯一索引异步构建，indexStatus.state=ready 后生效。既有数据必须满足新约束；较重校验可能返回 accepted+jobId。'},
  source:{create:'选择 adapter 并提供对应 connection；http-api/oauth2 需 http，modbus-tcp 需 modbus。不能用 http 代替 http-api。保存连接不代表采集已运行。','connection/update':'整体替换非密钥连接配置，省略的普通连接字段不会按字段合并。保留现有 credentialRef；此接口不创建或轮换密钥。','bindings/list':'id 是父 Source ID，返回父 revision 与 bindingId 到 Binding 的映射；需要版本保护时携带该 revision。','bindings/get':'id 是父 Source ID，bindingId 是其中一个绑定的键；不是独立元数据 ID。','bindings/create':'在父 Source 下创建 bindingId；binding 指定已发布 streamId/streamRelease、目标集合、采集配置、参数和映射。enabled 必须为 false，参数满足所选发布版本 parametersSchema。','bindings/update':'整体替换父 Source 中指定 bindingId 的配置；expectedRevision 是父 Source 版本。固定引用已发布版本，enabled 必须为 false。','bindings/delete':'删除父 Source 中指定 bindingId 的配置；expectedRevision 是父 Source 版本。不会通过此请求删除历史业务数据。'},
  stream:{create:'draft 描述支持协议、参数 Schema、JavaScript 处理器与输出 Schema；创建草稿不执行，完整程序沿用原生 Source/Stream API 与 SDK，存储策略独立配置。','draft/update':'整体替换 draft；不会改写既有 releases。修改后如需新的固定版本，再调用 publish。',publish:'从当前草稿生成不可变 release；expectedRevision 是 Stream 当前版本。完整程序使用原生发布生命周期，返回后仍须核验运行状态和交付数据。'},
  app:{create:'创建 App 配置，初始集合授权、成员、角色和关联 Topic 通过各专用 update 接口设置；不创建部署或页面。','grants/update':'整体替换 collections 授权列表；未列出的原授权被移除，[] 清空。alias 是 App 内集合别名，permissions 明确 record:read/write/delete 与 sample:read/write。关联 Topic 不授予这些权限。','members/update':'整体替换 members；userId 引用用户，roleIds 引用此 App 角色。未列出的原成员被移除，[] 清空；不是平台角色授权接口。','roles/update':'整体替换 roles；每个角色包含 id/name。未列出的原角色被移除，[] 清空；集合数据权限仍由 grants 配置。'},
  record:{list:'按 collectionId 分页读取 Record，可加 filters/orderBy；includeLatestSample 按授权附带最新采样。需要联表使用 records/aggregate。',get:'按 collectionId+id 读取一条记录和 revision；修改前以此核对当前版本。',create:'data 必须符合集合 recordSchema；id 可由服务端生成，也可提供稳定业务 ID。不会自动生成采样。',replace:'完整替换 Record 的 data，未提供的旧字段不会保留；可选携带当前 expectedRevision。只改字段或原子加减使用 records/update。',delete:'删除指定记录，可选携带当前 expectedRevision；不要把删除用作绕过唯一约束或版本冲突的手段。'},
  sample:{history:'按 collectionId 及可选 recordIds 读取历史；timeRange 使用业务时间区间 [from,to)，Unix 毫秒。分页数据不代表已取得全部历史。',latest:'recordIds 必填，1 至 200 个；返回每条记录的最新采样，缺测不能解释为测量值为 0。',append:'给已有 Record 追加一次符合 sampleSchema 的测量；time 是业务时间、quality 是质量标记，receivedAt 由服务端维护。需按幂等意图重试，不能重复造样本。'},
};
const verbGuides = {list:'按 page/size/search 分页列出配置；archived=true 只查已归档项。',get:'只读取对象定义、ID 和 revision，不展开业务记录或采样。',create:'schemaVersion 由服务端维护；业务 ID 从返回结果取得。',update:'只修改本接口列出的基本信息；省略字段保留原值，不能在此更新 Schema、连接、草稿或授权。',archive:'归档配置对象；可选传当前 expectedRevision，引用约束仍会检查。不是清空集合数据的入口。',restore:'恢复已归档对象；可选传当前 expectedRevision，恢复不代表启动采集或部署 App。'};
const explicit = {
  appPlatformPermissionsUpdate:['app','替换平台能力授权 · App','整体替换 App 的 platformPermissions；[] 撤销全部平台能力。集合 grants 不授予这些权限，角色和关联 Topic 也不替代平台授权。实际可用能力仍与 Key 范围及所属用户权限取交集；更新可选传当前 expectedRevision，dryRun=true 仅校验。'],
  appNavigationUpdate:['app','设置关联 Topic · App','整体替换 App 关联的 Topic 列表；传空列表可清空关联。navigation 是保留的接口字段名，表示 Topic 关联配置，不是页面导航栏或菜单。关联不会授予集合数据权限，也不会自动修改 App 页面。idempotencyKey 和 expectedRevision 可选，提供时生效；dryRun=true 仅校验。'],
  dataDiscover:['discover','搜索可访问集合','按名称或描述搜索当前身份可访问的集合。先发现真实 ID，再读取字段语义。'],
  collectionDescribe:['discover','读取集合字段、关系与统计口径','返回集合 Schema 与显式语义；未声明的关系、单位或粒度不能由字段名臆测。'],
  transactionValidate:['transaction','预检批量操作（不提交）','校验操作、权限与依赖；不预留库存、不持有跨请求锁，正式执行时仍重新检查。'],
  transactionExecute:['transaction','执行批量操作 · 原子提交','所有新增批量写入使用此入口，包括目录、Topic、Collection、Source、Stream、App 配置及 Record/Sample 操作；items[].action 决定每项动作。同一业务意图使用稳定 idempotencyKey；全部成功才提交。accepted 需继续查询状态；外部网络调用不属于此数据库事务。'],
  transactionStatus:['transaction','按幂等键查询批量执行状态','执行结果不明时先查原 idempotencyKey；不得为重试更换键导致重复写入。'],
  recordSearch:['query','筛选和分页查询业务记录','单集合普通查询；返回记录及分页信息。需要联表、投影或统计时使用 records/aggregate。'],
  recordAggregate:['query','联表、独立聚合与计算列','使用 joins/select/where/groupBy 描述查询；返回平面投影行。客户合同与回款等独立事实先在 subqueries 中分别按客户聚合，再以最终 LEFT JOIN 合并，避免一对多乘积放大金额。expression 提供 coalesce/加减乘/safeDivide 计算列；where 使用来源字段，having/orderBy 使用输出别名。'],
  recordQueryValidate:['query','预检并解释联表统计查询','校验查询计划及授权，不执行结果查询。预检通过不保证后续数据不变，也不为写事务锁定快照。'],
  catalog:['configuration','读取 API 能力目录','返回当前服务支持的操作、Schema 和限额；新连接或版本变化后调用。'],
  appSessionInspect:['identity','验证 App 用户会话','由 App 服务端携带与 appId 匹配的 App Key，验证平台会话 token 并读取可信用户身份及 App 角色；不签发登录凭据，不提升数据权限。'],
  appContext:['identity','读取当前 App 身份与授权','仅 App 数据 Key 可调用；返回实际集合授权，不能用于提升权限。'],
  appKeyCreate:['app','签发 App 数据 Key','密钥仅创建时返回；保存到受信服务端，禁止放入浏览器。'],
  appKeyRevoke:['app','撤销 App 数据 Key','撤销指定 Key 后该 Key 无法继续访问。'],
  records:['query','查询业务记录','queries 数组支持多个独立查询；pipeline 统一过滤、关联、计算及分组，支持 $distinct 去重、$frequencies 频次、$exists 判断存在。resultMode 默认 all 返回分页 items，first 返回 item 或 null 且禁止 page。末阶段 $count/$exists 不接受显式 page，空输入仍返回 0/false。所有参与集合独立鉴权；任一失败整批失败。'],
  samples:['sample','查询采样数据','queries 每项必填 collectionId、recordId、pipeline；recordId 与 pipeline 同级，限定父 Record 范围。通过 $match 筛选 time/receivedAt 与 data 字段，通过 $sort 与 first 查询最新一条；与 Record 共用 $distinct/$frequencies/$exists。末阶段 $count/$exists 不接受显式 page。多个父 Record 用多条 query。时间均为 Unix 毫秒。'],
  jobs:['jobs','查询异步作业状态','accepted 表示已受理；等待作业最终状态才能确认提交成功。'],
  metadata:['configuration','跨类型查询配置对象','按类型、ID、名称等条件读取配置对象；只读配置，不等于读取集合中的业务记录。'],
  namespace:['namespace','批量读取目录树','读取 directory/topic 展示结构；不会把 Record 展开为树节点。'],
  collections:['collection','组合读取集合与数据','可在一次查询中组合读取定义、记录和采样；只需某一类数据时可使用相应专用接口。'],
  topics:['namespace','读取主题展示数据','按照 Topic 已保存的 records/fields 视图读取，不创建数据副本。'],
  references:['configuration','查询对象引用关系','用于了解配置对象之间的引用，不推断业务批次关联。'],
  apply:['compatibility','已弃用 · 批量原子变更兼容入口','新调用按对象选择 meta/batch、/uns/records:transact 或普通 Record/Sample 资源接口 items 数组。此路径仅保留兼容；既有未决请求恢复时保留原幂等键与完整请求，不以换路径或换 key 重复执行。'],
};
const metaBatchExample={idempotencyKey:'create-workspace-001',items:[
 {opId:'directory',action:'meta.create',kind:'directory',id:'workshop-1',data:{schemaVersion:1,name:'车间一',parentId:null}},
 {opId:'child',action:'meta.create',kind:'directory',id:'line-1',data:{schemaVersion:1,name:'产线一',parentId:{ref:'directory'}}}
]};
const recordBatchExample={idempotencyKey:'reserve-order-001',items:[
 {opId:'check',action:'record.check',collectionId:'inventory',filter:{id:'material-1','data.available':{$gte:2}}},
 {opId:'decrement',action:'record.update',collectionId:'inventory',filter:{id:'material-1'},dependsOn:['check'],operations:[{op:'increment',path:'data.available',value:-2}]},
 {opId:'create',action:'record.create',collectionId:'reservations',id:'reservation-1',dependsOn:['decrement'],data:{materialId:'material-1',quantity:2}}
]};
for(const [scope,example,title] of [['meta',metaBatchExample,'元数据'],['record',recordBatchExample,'业务记录']]){
 operationExamples[scope+'BatchExecute']=example;
 operationExamples[scope+'BatchValidate']={items:example.items};
 operationExamples[scope+'BatchStatus']={idempotencyKey:example.idempotencyKey};
 explicit[scope+'BatchExecute']=[scope==='meta'?'configuration':'query',title+'批量操作 · 整批原子提交','只允许'+title+'操作，保留依赖、条件与适用的结果引用；不同类型不得混用。失败整体回滚，accepted 必须查询对应 /status。同一意图保留原请求、原身份和原路径重试；不因超时换接口或幂等键。删除/归档仍须独立删除授权。'];
 explicit[scope+'BatchValidate']=[scope==='meta'?'configuration':'query','预检'+title+'批次','按执行时相同的权限和领域规则回滚预检。不保留锁、不占用执行键，提交时重新检查。'];
 explicit[scope+'BatchStatus']=[scope==='meta'?'configuration':'query','查询'+title+'批次状态','使用原始 idempotencyKey。状态只对应此类型入口；unknown 不是回滚证明。'];
}
operationExamples.sampleAppendBatch={collectionId:'devices',recordId:'device-1',idempotencyKey:'samples-001',dryRun:false,samples:[{id:'sample-001',time:1760000000000,quality:'good',data:{temperature:25.5}}]};
operationExamples.sampleAppendBatchStatus={idempotencyKey:'samples-001'};
operationExamples.recordUpdate={collectionId:'orders',filter:{id:'order-1'},idempotencyKey:'update-order-001',dryRun:false,operations:[{op:'set',path:'data.status',value:'completed'}]};
explicit.sampleAppendBatch=['sample','批量追加采样 · 整批原子提交','请求外层必填 collectionId、recordId，samples[] 中填写 time/data 和可选 id/quality，禁止逐项 recordId。要求 Record 已存在；不修改 Record 或 Meta。dryRun=true 仅预检。沿用动态批次上限和 auto/sync/async；结果 opId=sample-0、sample-1 对应输入下标。重试保留同一路径与幂等键。'];
explicit.sampleAppendBatchStatus=['sample','查询采样批量追加状态','使用 append-batch 的原始幂等键，accepted 后查询直至终态。'];
explicit.recordUpdate=['query','局部更新一条业务记录','operations 支持 set/unset/increment/append/addToSet/pull，只允许已声明的 data.* 字段。默认 exactOne；显式 selection:first 可配 sort，returnDocument:before/after 返回文档。expectedRevision 可选；filter 在写入事务内检查，Schema/唯一键/来源保护保持不变。'];
for(const name of ['transactionExecute','transactionValidate','transactionStatus','apply'])explicit[name]=['compatibility','已弃用 · '+explicit[name][1].replace(/^已弃用 · /,''),'保留原有跨类型批次语义供既有客户端恢复请求。新调用按对象选择 meta/batch、/uns/records:transact 或普通 Record/Sample 资源接口 items 数组；不得把未决旧请求换入口重放。'];
delete operationExamples.metaBatchExecute.idempotencyKey;
operationExamples.recordTransactionExecute=structuredClone(recordBatchExample);delete operationExamples.recordTransactionExecute.idempotencyKey;
operationExamples.recordTransactionValidate={items:recordBatchExample.items};
explicit.recordTransactionExecute=['transaction','业务事务 · 查询、条件与混合操作','跨授权 Collection 编排 query/check/read/create/update/replace/delete 和结果绑定，整批原子提交。record.query 的 query 支持 filter、sort、limit（默认20，最多100）、expectCount.min/max（limit前总匹配数断言）。后续项可绑定 id=query.records.N.id，仅同一 Collection，省略静态 id；同一记录只写一次。查询型事务采用可串行化隔离和记录锁，冲突整体回滚，不跳过锁定行；不支持事务内联表聚合或 allowPartial。普通同类操作使用资源接口 items 数组。幂等键可选，accepted 按 jobId 查询；超时先核对业务状态。'];
explicit.recordTransactionValidate=['transaction','预检业务事务','真正执行并回滚，不保留锁，不预留资源。输入 items 与正式事务一致。'];
explicit.metaBatchExecute=['configuration','元数据批量操作','仅元数据配置与 stream.publish。expectedRevision 和 idempotencyKey 均可选，提供时生效。失败整批回滚，异步按 jobId 查询。'];
explicit.metaBatchValidate=['configuration','预检元数据批次','按当前权限和领域规则执行后回滚；版本号可选，提供时校验；不接受 allowPartial。'];
for(const name of ['recordBatchExecute','recordBatchValidate','recordBatchStatus','sampleAppendBatch','sampleAppendBatchStatus','metaBatchStatus']) explicit[name]=['compatibility','兼容旧接口 · '+name,'仅用于旧请求恢复。普通 Record CUD 和 Sample 追加使用资源接口 items 数组；业务事务使用 /uns/records:transact。新调用可按需传幂等键。'];
for(const [name,label] of [['recordCreate','新增'],['recordUpdate','局部更新'],['recordReplace','完整替换'],['recordDelete','删除'],['sampleAppend','追加采样']]) explicit[name]=[name==='sampleAppend'?'sample':'query',label+' · 支持一条或多条', '统一 collectionId、dryRun、items 数组和可选 mode。默认整批回滚；allowPartial=true 按项独立提交，results 按输入顺序返回，每项失败包含 error.code/path/reason/repair。部分成功为 partiallyCommitted，检查 summary 和 jobId；dryRun 仅校验。幂等键和 Record 版本均可选。accepted 按 jobId 查询，结果不明不得自动重复提交。'];
explicit.sampleAppend[2]+=' Sample 请求外层必须提供 recordId，与 collectionId、items 同级；items 只填写 time/data 和可选 id/quality，不允许逐项覆盖父 Record。';
for(const name of ['recordUpdate','recordReplace','recordDelete']) explicit[name][2]+=' 每项 filter 与查询 $match 共用语法，必须精确选择一条记录；0 条或多条均拒绝写入，expectedRevision 可追加版本保护。';
const novaSystemFields = new Set(['createdAt','updatedAt','revision','time','receivedAt','recordId','quality','sourceId','streamId','bindingId']);
const novaField = p => p.startsWith('data.') ? p.slice(5) : p === 'id' ? '_id' : novaSystemFields.has(p) ? '_meta.'+p : p;
function normalizeDocumentExample(value) {
 if (Array.isArray(value)) return value.map(normalizeDocumentExample);
 if (!value || typeof value !== 'object') return typeof value === 'string' && value.startsWith('$data.') ? '$'+value.slice(6) : value;
 return Object.fromEntries(Object.entries(value).map(([key,item]) => {
  if (['sort','$sort'].includes(key)) return [key,item.map(row=>Array.isArray(row)?[novaField(row[0]),row[1]]:[novaField(row.field),row.direction==='desc'?-1:1])];
  if (['filter','$match'].includes(key)) return [key,Object.fromEntries(Object.entries(item).map(([path,v])=>[novaField(path),normalizeDocumentExample(v)]))];
  if (['path','field','localField','foreignField'].includes(key) && typeof item === 'string' && item.startsWith('data.')) return [key,novaField(item)];
  if (key === 'resultPath' || key === 'bindings') return [key,item];
  return [key,normalizeDocumentExample(item)];
 }));
}
for (const name of Object.keys(operationExamples)) operationExamples[name]=normalizeDocumentExample(operationExamples[name]);
export function operationDocumentation(op) {
  if (op.metadataAction) return {tags:['元数据管理'],summary:op.summary,description:op.description};
  if (['namespaceCopy','sourceReplace','streamReplace','appReplace'].includes(op.name)) return {tags:[tag('configuration')],summary:op.summary,description:op.description};
  if (op.novaAction) return {tags:[tag(op.mutation ? (op.resource==='sample'?'sample':'query') : 'dataQuery')],summary:op.summary,description:'4.0 文档动作；业务字段省略 data.，系统字段使用 _id/_meta.*，sort 为 [[field,1|-1]]。dryRun 默认 false；限额、原子性和返回结构以本接口 Schema 为准。'};
  if (['records','samples','recordGet','recordList','sampleHistory','sampleLatest','recordSearch','recordAggregate','recordQueryValidate'].includes(op.name)) return {tags:[tag('dataQuery')],summary:op.summary || op.name,description:'Record/Sample 使用统一业务字段路径和权限、分页预算；优先使用 Collection 作用域的 records:find/aggregate 与 samples:query。'};
  const known = explicit[op.name];
  if (known) return {tags:[tag(known[0])], summary:known[1], description:known[2],...(op.name==='apply'?{deprecated:true}:{})};
  const parts = op.suffix.split('/').filter(Boolean);
  const family = parts[1];
  const verb = parts.slice(2).join('/');
  if (['discovery','query','batch'].includes(family)) {
    const key = {discovery:'discover',query:'query',batch:'transaction'}[family];
    const names = {search:'搜索可访问的数据',schema:'读取字段与关系语义',preview:'预检并解释查询',validate:'预检批量操作（不提交）',execute:'执行批量操作',status:'按幂等键查询批量执行状态'};
    return {tags:[tag(key)],summary:names[verb] || op.summary || op.name,description:op.summary || '使用当前权限执行；参数以当前接口 Schema 为准。'};
  }
  const kind = op.resource || family?.replace(/s$/, '');
  const category = ['directory','topic'].includes(kind) ? 'namespace' : kind === 'record' ? 'query' : kind;
  return {tags:[tag(groups.some(row=>row[0]===category)?category:'configuration')],summary:`${actions[verb] || verb} · ${labels[kind] || kind}`,description:[resourceGuides[kind]?.[verb] || verbGuides[verb] || op.summary || '',op.mutation?'写入需要当前身份的对应权限；支持的预校验、幂等及版本保护参数以本接口 Schema 为准。':'读取受当前调用身份权限约束。'].join('\n\n'),...(op.deprecated?{deprecated:true}:{})};
}
const descriptions = {
  indexes:'统一索引声明，例如 [{"path":"orderNo","unique":true}] 或 [{"paths":["factoryId","externalId"],"unique":true}]。省略保留，[] 移除；字段不带 data.，唯一索引仅 Record B-tree，异步构建并通过 indexStatus 检查就绪。',
  indexStatus:'只读索引状态：name/spec/state/error；只有 ready 表示当前声明的物理索引有效，pending/building/failed 均不能用于业务键 upsert；挂载集合由源端管理唯一约束。',
  upsert:'true 时 filter 必须有 _id 等值条件，或覆盖某个 ready 唯一索引全部字段的等值条件，否则返回 UPSERT_REQUIRES_UNIQUE_INDEX。未匹配则插入，唯一冲突返回 409 DUPLICATE_KEY。',
  navigation:'App 关联的 Topic 配置；保留的字段名，不表示页面菜单，不授予数据权限。',
  id:'对象 ID（字符串），从发现或读取接口取得；不要把名称当成 ID。', collectionId:'集合 ID，业务记录和采样所属的 Collection。',
  expectedRevision:'期望版本号；读取当前对象后携带。冲突须重新读取并决策，不能盲目覆盖。',
  idempotencyKey:'本次业务意图的唯一键。相同请求重试必须复用；不同请求不得复用。',
  allowPartial:'仅普通数组写入支持，默认 false。true 按项独立提交；检查 summary 和 results[].error，失败项修正后作为新请求提交。事务接口不接受。',
  dryRun:'true 仅校验，不提交；校验不会预留资源，执行时仍重新检查。',
  page:'页码，从 1 开始。', size:'每页数量；上限以本接口的 maximum 或能力目录为准。', search:'按名称或支持的文本字段搜索；不是 SQL。',
  queryId:'本批查询内唯一标识，用于对应结果。', opId:'本事务内唯一操作标识，供依赖和结果引用使用。',
  dependsOn:'必须先完成的 opId；服务端检查依赖循环。',
  time:'业务采样时间，Unix 毫秒；不是服务器接收时间。', from:'时间范围起点，Unix 毫秒。', to:'时间范围终点，Unix 毫秒。',
  recordIds:'记录 ID 列表，不是集合 ID。', recordId:'采样所属业务记录 ID。',
  archived:'true 只列出已归档对象；false 只列出活动对象。',
  atomic:'true 整批原子提交，文档动作最多 200 项；false 为逐文档提交。' ,
  relational:'结构化联表和聚合计划；不可与根 filters/orderBy/search/recordIds/includeLatestSample 混用。',
  groupBy:'使用来源字段，如 base.data.stationId；所有非聚合投影须满足分组规则。',
  having:'聚合结果过滤，field 引用 select.as 输出名。',
  subqueries:'独立事实的命名聚合，最多 4 个。每项必须至少一个聚合及 1..8 个 groupBy；不递归、无分页、排序或计算列，所有子查询的原始 JOIN 总数最多 4。每个子查询必须被最终 JOIN 引用，且 ON 覆盖每个分组输出，保持基表每行最多匹配一条。最终层禁止 distinct=true、原始集合 JOIN、非空 groupBy 和 aggregate。',
  expression:'结构化计算列，不能同时设置 field、aggregate 或 distinct。缺失值仅通过显式 coalesce 转为 0；safeDivide 遇零/null分母返回 null。',
  bindings:'把先前操作结果绑定到本操作的指定字段；不执行字符串模板或任意代码。',
};
// Keep each legacy batch endpoint's visible request focused on its own domain.
const queryFields = {
  metadata:'page size search kinds ids includeArchived', namespace:'page size search kinds ids parentId includeAncestors includeArchived',
  collections:'page size search mode ids collectionId includeArchived include records selectedRecord',
  records:'page size search collectionId recordIds includeLatestSample filters orderBy relational paginationMode cursor asOf includeTotal',
  samples:'page size mode collectionId recordIds timeRange fieldPath maxPoints paginationMode cursor asOf includeTotal',
  topics:'topicId include records selectedRecord', references:'page size targets relationKinds', jobs:'jobIds',
};
export function decorateSchemas(schemas, operations) {
  for (const name of ['UnsV2Query', 'UnsV2RecordSearchReq']) {
    if (schemas[name]) Object.assign(schemas[name].properties, structuredClone(cursorPage));
  }
  for (const [name,maximum] of [['UnsV2RecordSearchReq',200],['UnsV2RecordAggregateReq',200],['UnsV2DiscoverySearchReq',200]]) {
    if (!schemas[name]) continue;
    schemas[name].properties.page = {type:'integer',minimum:1,default:1};
    schemas[name].properties.size = {type:'integer',minimum:1,maximum,default:20,description:`每页数量，最多 ${maximum}。`};
  }
  // PaginationResult and all paged response fields come from ResponseSchemas;
  // resource routes reuse the same source rather than maintaining a second list.
  const responseModels = {recordTransactionExecute:'Outcome',recordTransactionValidate:'TransactionValidation',metaBatchExecute:'Outcome',recordBatchExecute:'Outcome',sampleAppendBatch:'Outcome',metaBatchValidate:'TransactionValidation',recordBatchValidate:'TransactionValidation',metaBatchStatus:'TransactionStatusResult',recordBatchStatus:'TransactionStatusResult',sampleAppendBatchStatus:'TransactionStatusResult',dataDiscover:'DiscoveryResult',collectionDescribe:'CollectionDescription',recordSearch:'RecordSearchResult',recordAggregate:'RecordAggregateResult',recordQueryValidate:'RecordQueryValidation',transactionExecute:'Outcome',transactionValidate:'TransactionValidation',transactionStatus:'TransactionStatusResult'};
  for (const op of operations) if (responseModels[op.name]) {
    op.response = `${op.handler}Response`;
    schemas[op.response] = {...structuredClone(schemas.UnsV2Envelope),properties:{...structuredClone(schemas.UnsV2Envelope.properties),data:{$ref:`#/components/schemas/${responseModels[op.name]}`}}};
  }
  if (schemas.UnsV2TransactionValidateReq) {
    const validation = schemas.UnsV2TransactionValidateReq;
    validation.required = ['items'];
    validation.properties.items = structuredClone(schemas.UnsV2ApplyReq.properties.items);
    validation.properties.atomic = {type:'boolean',enum:[true],default:true};
    validation.properties.idempotencyKey = {type:'string',description:'可省略；预检忽略此值，使用临时身份并回滚，不占用正式执行的幂等键。'};
    validation.properties.mode = {type:'string',description:'预检忽略此值，服务端强制回滚校验，不排队执行写入。'};
    validation.properties.dryRun = {type:'boolean',description:'预检始终为 true；即使传 false 也不会提交。'};
  }
  if (schemas.TransactionValidation) {
    const props = schemas.TransactionValidation.properties;
    props.status = {type:'string',enum:['validated']};
    for (const key of ['writesCommitted','locksHeldAfterResponse','executionGuaranteed']) props[key] = {type:'boolean',enum:[false]};
  }
  if (schemas.TransactionStatusResult) {
    schemas.TransactionStatusResult.properties.status = {type:'string',enum:['unknown','accepted','committed','queued','running','succeeded','failed'],description:'unknown 不是已回滚的证明；按 retryAdvice 使用原请求和原 key 恢复。succeeded 表示异步作业已成功，committed 表示已有本机提交回执；accepted 表示持久接收等待上传，不是中心落库。'};
    schemas.TransactionStatusResult.properties.committedAt = {type:'integer',description:'提交回执时间，Unix 毫秒；不是设备/WMS实际执行时间。'};
  }
  schemas.RetryAdvice = {type:'object',additionalProperties:false,required:['recommendedAction','canRetrySameRequest','keepIdempotencyKey'],properties:{recommendedAction:{type:'string',description:'下一步恢复动作，不构成额外业务授权。',enum:['inspectErrorAndCorrectRequest','queryTransactionStatusBeforeRetry','rereadAndReplan','queryTransactionStatusBeforeReplanning','queryOriginalTransactionStatusDoNotReplaceKey','restoreAuthorizationBeforeRetry','narrowQueryAndRetry','retryOriginalRequestWithSameKey','useCommittedOutcomeDoNotExecuteAgain','pollTransactionStatus','inspectJobFailureBeforeReplanning']},canRetrySameRequest:{type:'boolean',description:'是否可重发完全相同的请求；若建议先查状态，必须先查。'},keepIdempotencyKey:{type:'boolean',description:'恢复原意图时保留原幂等键；不能换键重复执行。'}}};
  for (const schema of Object.values(schemas)) {
    if (!schema.properties) continue;
    // Registry primitives may be reused by several fields. Decorate each field
    // independently, otherwise id/recordId/key descriptions grow on every use.
    schema.properties = Object.fromEntries(Object.entries(schema.properties).map(([name, property]) => [name, structuredClone(property)]));
    for (const [name, property] of Object.entries(schema.properties)) {
      if (schema === schemas.UnsV2TransactionValidateReq && ['idempotencyKey','mode','dryRun'].includes(name)) continue;
      if (descriptions[name] && !property.$ref) property.description = [descriptions[name],property.description].filter(Boolean).join(' ');
    }
  }
  const requests = new Map();
  if (schemas.UnsV2ApplyReq?.properties.mode) schemas.UnsV2ApplyReq.properties.mode.description = 'auto 自动选择，sync 请求同步，async 请求排队；最终以返回状态为准。';
  for (const op of operations) {
    if (['recordSearch','recordAggregate','recordQueryValidate'].includes(op.name)) {
      const source = schemas[op.request];
      const fields = op.name === 'recordSearch' ? 'collectionId page size filters search recordIds includeLatestSample orderBy paginationMode cursor asOf includeTotal' : 'collectionId page size subqueries joins select where groupBy having orderBy distinct';
      const requestName = `${op.handler}DocumentedRequest`;
      schemas[requestName] = {...structuredClone(source),properties:Object.fromEntries(fields.split(' ').filter(key=>source.properties[key]).map(key=>[key,structuredClone(source.properties[key])])),required:op.name==='recordSearch'?['collectionId']:['collectionId','select']};
      requests.set(op.name,requestName);
      continue;
    }
    if (op.request !== 'UnsV2QueryReq' || !queryFields[op.name]) continue;
    const itemName = `${op.handler}Query`;
    const fields = ['queryId', ...queryFields[op.name].split(' ')];
    schemas[itemName] = {...structuredClone(schemas.UnsV2Query), properties:Object.fromEntries(fields.filter(key=>schemas.UnsV2Query.properties[key]).map(key=>[key,structuredClone(schemas.UnsV2Query.properties[key])])),required:['queryId']};
    if (op.name === 'samples') schemas[itemName].properties.mode = {type:'string',enum:['history','latest','trend'],default:'history',description:'history 原始历史，latest 最新采样，trend 趋势降采样。'};
    if (op.name === 'collections') schemas[itemName].properties.mode = {type:'string',description:'detail 按 collectionId 组合读取定义和数据；省略时分页列出集合。'};
    const requestName = `${op.handler}DocumentedRequest`;
    schemas[requestName] = {type:'object',additionalProperties:false,required:['queries'],properties:{queries:{type:'array',minItems:1,maxItems:20,items:{$ref:`#/components/schemas/${itemName}`}}}};
    requests.set(op.name, requestName);
  }
  schemas.UnsV2ErrorResponse = {type:'object',required:['code','msg','requestId','error'],properties:{code:{type:'integer'},msg:{type:'string'},requestId:{type:'string'},data:{nullable:true},error:{type:'object',required:['code','retryable','details'],properties:{code:{type:'string'},retryable:{type:'boolean'},rolledBack:{type:'boolean'},opId:{type:'string'},path:{type:'string'},details:{type:'array',items:{type:'object',additionalProperties:true}},repair:{type:'string'},reason:{type:'string'},retryAdvice:{type:'object',additionalProperties:true}}}}};
  schemas.UnsV2ErrorResponse.properties.error.properties.retryAdvice = {$ref:'#/components/schemas/RetryAdvice'};
  schemas.UnsV2ErrorResponse.properties.error.properties.queryId = {type:'string',description:'独立查询失败时标识对应 queries 项；error.path 同时指出输入位置。整批失败，不返回此前查询的部分结果。'};
  schemas.UnsV2ErrorResponse.properties.error.required.push('retryAdvice');
  return requests;
}
export const errorResponses = Object.fromEntries(Object.entries({401:'未认证：检查 Key，不要更换业务意图或重复写入。',403:'权限不足：请求授权，不得绕过集合限制。',404:'对象不存在或不可访问。',409:'条件、版本或唯一键冲突：读取最新状态并重新决策。',413:'请求或响应超限：缩小范围，不得擅自拆分原子意图。',422:'请求不合法：按 error.path 和修复建议修改参数。',500:'服务端错误：结果不明时先查询事务状态。',503:'暂时不可用：按重试建议处理，保留原幂等键。',504:'查询超时：缩小时间范围、关联或聚合范围。'}).map(([status,description])=>[status,{description,content:{'application/json':{schema:{$ref:'#/components/schemas/UnsV2ErrorResponse'}}}}]));
