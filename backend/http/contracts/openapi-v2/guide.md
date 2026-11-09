# Enterprise OpenAPI v2 接口总册

契约版本 **{{version}}**，包含 **{{operationCount}} 个当前公开接口**。这是 OpenAPI v2 的唯一使用手册：先按身份选择能力，再查接口参数和完整 Schema。Swagger 与本手册的接口参考由同一份契约生成，不展示内部专用或已弃用接口。

## 阅读导航

- [认证与边界](#authentication)
- [平台层（Workspace）](#workspace)
- [业务 App 层](#app)
- [通用能力：用户、通知、文件、任务、控制](#common)
- [写入、预检、幂等与并发](#writes)
- [查询与统计](#queries)、[业务事务](#transactions)、[分页](#pagination)
- [异步任务结果](#jobs)、[错误处理](#errors)、[兼容接口](#legacy)
- [逐接口参考](#reference)、[完整字段 Schema](#schemas)

<a id="authentication"></a>
## 认证与边界

普通请求使用 `POST /openapi/v2/{接口后缀}`、`Content-Type: application/json` 和 `X-API-Key` 请求头。记录变更订阅使用 `GET /openapi/v2/uns/records:subscribe`、`Accept: text/event-stream` 和相同 API Key；集合与记录 ID 使用重复查询参数，断线续传使用 `Last-Event-ID`。接口参考中的 `/uns/...`、`/platform/...`、`/notifications/...`、`/files/...` 都需要加 `/openapi/v2` 前缀。

平台层与业务 App 层按使用职责分类，不是两套服务器，也不是仅凭 URL 前缀判断权限。平台身份可在授权范围内读写业务数据；App 身份可使用部分只读元数据及显式授权的通用能力。Swagger 的 tag 只是导航，不会授予权限。

| 身份 | 用途 | 权限来源 |
| --- | --- | --- |
| 平台 API Key | Workspace 集成、配置管理、数据处理及公共服务 | Key 范围与所属用户当前权限的交集 |
| 绑定 App 的 API Key | 业务 App 服务端访问获授权的数据及公共服务 | App grants / platformPermissions、Key 范围、所属用户当前权限的交集 |
| 平台用户会话 | 管理后台及 App 用户身份校验 | 用户当前权限及 App 显式成员、角色配置；不能代替 OpenAPI 的 API Key |

App 身份由认证后的 Key 绑定关系决定，不能在请求中填写另一个 `appId` 来切换身份。API Key 只保存在受信服务端，不能打包进浏览器代码、URL 或日志。`full_access` 也不等于自动获得所有 Source、Stream、App 专用管理权限。

本册只覆盖公开 OpenAPI。管理后台使用的内部接口不在此列，也不能把内部路径改成 OpenAPI 前缀后调用。能力目录说明可用契约，不代表当前调用者拥有目录中所有操作的执行权限；每次请求仍独立鉴权。

请求示例（替换域名、Key 和业务 ID）：

```http
POST /openapi/v2/uns/apps/context
Content-Type: application/json
X-API-Key: <APP_API_KEY>

{}
```

响应通常为 `{code,msg,requestId,data}`；失败附带结构化 `error`。以具体接口 Schema 为准，不能只根据 HTTP 200 判断写入已提交。时间字段按接口定义使用 Unix 毫秒；ID 和字段名区分大小写，对外字段使用小驼峰。

<a id="workspace"></a>
## 平台层（Workspace）

平台层负责工作空间资源建模、配置和授权，以及供各调用者复用的公共服务。

| 能力 | 主要入口 | 作用 |
| --- | --- | --- |
| 元数据发现与编排 | `uns/catalog/get`、`metadata:find`、`references/query`、`metadata:transact` | 查询契约、配置和引用；成组修改配置 |
| 目录与 Topic | `uns/metadata:*`（kind=directory/topic） | 目录组织层级；Topic 引用 Collection 或字段，作为展示视图 |
| Collection | `uns/collections/*` | 定义 Record/Sample Schema、唯一键与语义 |
| Source 与 Binding | `uns/sources/*` | 来源连接、绑定和采集运行管理 |
| Stream | `uns/streams/*` | 草稿、发布版本与程序运行管理 |
| App 管理 | `uns/apps/*` 中的配置、grants、角色、成员、Key 管理 | 定义业务 App 及其权限；运行身份接口另归业务 App 层 |
| 通用服务 | `platform/*`、`notifications/*`、`files/*`、`uns/jobs/query` | 实例信息、用户、通知、文件和作业状态 |

推荐配置顺序：

1. 创建 Collection，确定 `recordSchema`、可选 `sampleSchema`、`indexes` 和字段语义。
2. 按需创建目录与 Topic，关联已经存在的 Collection。Topic 不复制业务数据，也不是新的授权边界。
3. 需要采集时配置 Source、Stream 和 Binding。Binding 固定引用发布版本；保存配置、发布版本、部署程序、启动采集是不同动作。
4. 创建 App，设置集合 grants、成员与角色；需要公共服务时再设置 `platformPermissions`。
5. 签发绑定该 App 的 Key，交给 App 服务端。之后先检查 App context，再读写业务数据。

多个元数据变更需要原子组合时使用 `metadata:transact`，预检使用 `metadata:transact` 的 `dryRun:true`。不要用 Record 事务修改元数据。`navigation.topicIds` 只是 App 关联 Topic 的配置，不是页面菜单定义，也不自动授予集合权限。

<a id="app"></a>
## 业务 App 层

业务 App 主要做三件事：确认服务端和用户身份、发现获授权的数据结构、处理 Record/Sample 业务数据。

### 身份与用户会话

- `uns/apps/context`：确认当前 Key 对应的 App 和实际授权，用于服务端启动、自检和能力判断。
- `uns/apps/session/inspect`：结合 App Key、真实平台用户 token 和 App 成员/角色配置检查用户身份。平台管理员不自动成为 App 成员。
- inspect 不签发或续期用户 token，也不会把后续服务 Key 请求自动切换成该用户的身份。App 必须在自身业务入口校验用户行为。

### App 能调用哪些元数据接口

“UNSMeta 只读”指受限的只读能力，不代表整个元数据 API 对 App 开放。

| 接口或资源 | App 范围 |
| --- | --- |
| `discovery/search` | 发现具备 `record:read` 授权的集合 |
| `discovery/describe`、`collections/get`、`collections/query` 的 detail 模式 | 读取授权集合定义；Sample Schema 还要求相应 Sample 读权限；关系目标按授权裁剪 |
| `topics/view` | 只读展示数据，逐一检查引用集合的读权限 |
| 全量 metadata、namespace、references 查询，目录/Topic/Collection 列表，目录/Topic get | 不对 App 数据身份开放 |
| Source、Stream、App 配置管理及元数据写入 | 不对 App 数据身份开放；签发/撤销 App Key 也属于管理操作 |

### 业务数据流程

先 `discovery/search` 找集合，再 `discovery/describe` 确认结构、粒度、单位和显式关系。不要仅凭名字猜测字段或关系。之后按需要调用：

- Record：集合路径 `/uns/collections/{collectionId}/records` 及 `records:find/count/distinct/aggregate` 读取、文档动作写入、租约和恢复；完整路径、请求与返回见 [数据接口契约](uns-v2/data-api.md)。
- 业务事务：`/uns/records:transact` 提交多个有关联的 Record 操作，`dryRun:true` 预检。
- Sample：集合下 `/records/{recordId}/samples` 追加，`samples:find/aggregate/latest` 查询，`samples:delete` 删除明确范围。Collection 必须定义 Sample Schema，关联 Record 必须存在。

Record 身份为 `(collectionId,id)`；Sample 身份为 `(collectionId,recordId,id)`。Sample 的 `time` 是业务发生时间，`receivedAt` 是服务端接收时间，两者不能混用。

<a id="common"></a>
## 通用能力

这些能力统一归平台层，业务 App 获显式授权后可以复用，不在 Swagger 中重复列两套接口。

| 能力 | 权限 | 注意事项 |
| --- | --- | --- |
| 实例与身份 | `openapi.base` | 读取当前身份或实例信息 |
| 用户目录 | `platform.users.read` | 不是按 App 成员过滤后的名单 |
| 项目成员 | `platform.projects.members.read` | 还检查 Key 所属用户对项目的访问权 |
| 通知 | `notifications.send`、`notifications.read` | 目标用户可以超出 App 成员；状态查询限定当前 API Key 的通知 |
| 文件 | `asset.file.upload`、`asset.file.download`、`asset.file.delete` | 按文件可信归属及相应能力校验 |
| UNS 作业状态 | 对应业务操作授权与作业归属 | App 查询限制在相同用户、App、API Key 下，并复核当前业务授权 |
| 设备控制 | `control.read`、`control.execute`，有副作用另需 `control.write` | 使用稳定 Control code；不授予 Source、Stream 或 Control 管理权限 |
| Stream 请求 | `stream.requests.read`、`stream.requests.invoke`，写合同另需 `stream.requests.write` | 新 request 合同；发现与调用权限独立；App 不支持 manage |

给 App 增加 `platformPermissions` 不会扩大已经签发的 Key scope；撤销授权会在后续鉴权中生效。集合 grants 不自动授予公共服务权限。

Stream 请求使用 `POST /openapi/v2/stream-requests/discover`、`/{requestCode}/describe`、`/{requestCode}/invoke`；内部对应前缀为 `/api/core/v2/stream-requests`。发现只列活动 request 合同，返回 `{total,list}`；详情直接返回公开合同。调用 Body 为 `{input,idempotencyKey?}`，无业务输入须传 `input:null`，路径编码不可放入 Body 或 Query；Body 必须是合法 UTF-8、最多 1 MiB，幂等键最多 128 个 UTF-8 字节。写合同必须有持久键，只读合同遵循其 `idempotency`。成功直接返回 `{code,contractVersion,result,runID,status,replayed}`；错误为 `{errorCode,message}`，结果未知使用 HTTP 504 / `STREAM_REQUEST_RESULT_UNKNOWN`。SDK 入口为 `client.streamRequests.use({code})`，不自动重试。Native App 使用独立 Stream policy，并在停止后显式刷新凭据应用新增权限；Project App 使用稳定身份和当前发布状态。按 Request code 与目标逐项委托的授权管理尚未迁移。

Control 是单独的公开契约，端点为 `POST /openapi/v1/controls/discover`、`POST /openapi/v1/controls/{code}/describe` 与 `POST /openapi/v1/controls/{code}/invoke`，不是将本册 V2 前缀直接替换为 V1。通过服务端 SDK 的 `client.controls.use({code}).invoke(body,{idempotencyKey})` 调用，业务 JSON 原样放在 Body，持久化的幂等键放在 `Idempotency-Key` Header。同步执行不经过 NATS；受理异步命令只代表已提交，须另查业务回执。平台运行成功、协议接受和设备实际执行完成不是同一件事。SDK 不自动重试设备操作，超时先核对原意图的结果，不更换幂等键重复控制。

文件 API 是 JSON 控制面：先申请上传/下载凭证，再按返回的 URL、HTTP 方法和 headers 传输文件内容；分片上传使用对应的创建、签名、完成或中止接口。申请上传凭证不等于文件已经上传。不要向对象存储 URL 附带平台 API Key，文件二进制不放进 OpenAPI JSON。

通知发送和对象存储操作不参加 UNS 数据库事务，不能承诺随 Record 事务一起回滚。

<a id="writes"></a>
## 写入、预检、幂等与并发

### 选择入口

| 需求 | 入口 | 原子边界 |
| --- | --- | --- |
| 普通元数据资源增删改 | `metadata`、`metadata:updateOne/replaceOne/deleteOne` 及对应批量动作 | 默认整批提交或回滚 |
| 多种元数据操作编排 | `metadata:transact`，预检 `dryRun:true` | 一个元数据批次 |
| Record 普通增删改 | 集合文档动作 | 单条原子；many/bulk 默认逐文档，atomic:true 整批且最多 200 项 |
| Record 读、条件和混合写入 | `/uns/records:transact`，预检 `dryRun:true` | 始终整个事务 |
| Sample 追加 | `/uns/collections/{collectionId}/records/{recordId}/samples`，`items` 数组 | 默认整批；显式部分模式逐项返回结果 |

集合文档动作的 dryRun 默认 false。保留的普通数组资源写接口要求显式 `dryRun`：`true` 仅校验并回滚，`false` 提交。程序调用、部署、探测等运行时接口保留各自单目标结构，不能机械套用数组包装；以逐接口参数为准。预检不保留锁、不预留资源，正式提交会重新检查。

Sample 追加路径为 `/uns/collections/{collectionId}/records/{recordId}/samples`，正文为 `{dryRun,items:[{time,data,id?,quality?}],mode?}`。一批只追加到 URL 指定的已存在父 Record，items 禁止携带 recordId；写多个父 Record 时分别请求。

`idempotencyKey` 和 `expectedRevision` 都可选。提供版本时必须匹配，不匹配失败；省略版本表示操作当前存在记录，不意味着不存在时自动创建。业务防重应同时使用稳定业务 ID、唯一键或事务条件。

普通资源写入的幂等键在同一主体内共用命名空间，不按 URL 隔离；建议每个业务动作使用独立键。相同键与相同请求可恢复已保存结果，不同内容复用键会冲突。部分批次适配器有自己的键派生规则，恢复未决请求必须保持原入口、原键和原请求，不能换到另一接口重放。未提供键时，重复 HTTP 请求不会自动去重。

最多 10000 项、请求体最多 16 MiB；同步模式最多 200 项，`mode:"auto"` 可转异步。必须原子提交的操作不能为了绕过限制而静默拆批。`allowPartial:true` 时应检查 `summary` 及逐项结果；不要重试已经提交的项。普通结果预览和 MCP 输出各有上限，完整状态按回执/分页查询，不能将预览当成全部结果。

<a id="queries"></a>
## 查询与统计

数据接口仅分为「业务数据」「时序数据」两类，管理接口各归其类。完整路径和统一响应见 [数据接口契约](uns-v2/data-api.md)。下列路径均带公共前缀 /openapi/v2。

Record 的普通列表使用 POST /uns/collections/{collectionId}/records:find，正文为 {filter?,sort?,projection?,limit?,cursor?,page?}；单独计数用 records:count，去重用 records:distinct，复杂统计或关联用 records:aggregate。没有 Record latest 接口。Sample 使用同一集合下 samples:find、samples:aggregate、samples:latest；追加使用 /records/{recordId}/samples，显式范围删除使用 samples:delete。

samples:find/aggregate 必填 Unix 毫秒 from/to，范围为 [from,to)；可选 recordIds 最多 20 个，省略表示当前授权集合，空数组或 null 无效。samples:latest/delete 必须显式指定 recordIds；delete 还必须指定 from/to，可 dryRun。集合、记录、时间范围由服务端独立约束，filter/pipeline 只能进一步收窄，OR 条件不能放大授权和数据范围。分页覆盖所选全部记录，不是逐记录分别分页。

查询字段直接写 quantity、customer.name；系统字段写 _id、_meta.updatedAt、_meta.time 等。filter 与 pipeline 的 $match 使用同一谓词，sort 为 [[field,1|-1]]。支持 $eq/$ne/$gt/$gte/$lt/$lte/$in/$nin/$exists，$and/$or/$nor/$not，以及 $elemMatch/$all/$size 和受限 $expr。字段路径的字面点用反斜杠转义，JSON 字符串再次转义；不提交 SQL 或 JavaScript。

普通 find 返回 {items,pageInfo}，Record 对象为 {id,collectionId,data,revision,createdAt,updatedAt,sourceId,sourceTime,lock?}；Sample 对象为 {id,collectionId,recordId,time,receivedAt,quality,data,sourceId,streamId?,bindingId?}。Sample find/latest 共用同一对象，latest 无数据时 sample=null。业务字段在 data 内，缺失来源为 null；时间为 Unix 毫秒。projection 只选择业务内容，身份和系统字段保留。findOneAnd、lock.record、事务 document/query.records 复用同一 Record 对象。

查询字段与响应位置不同是明确契约：filter 写 {"temperature":{"$gt":20}}，响应读 data.temperature；filter 写 _meta.updatedAt，响应读 updatedAt。不能为了匹配响应而给业务查询字段增加 data. 前缀，这会查询业务对象中名为 data 的真实嵌套字段。事务 record.read 的 data 仍是结果绑定使用的业务载荷，document 才是规范 Record 对象。

聚合请求正文为 {pipeline,page?}，Sample 额外有 from/to/recordIds；返回 {items,pageInfo}，items 字段完全由 pipeline 决定，不强行包装为 Record/Sample。输入仍为 {_id,_meta,...业务字段}。支持 $match/$project/$set/$sort/$distinct/$frequencies/$exists/$group/$lookup/$unwind/$skip/$limit/$count；平台支持的精确形状以 catalog Schema 为准，不推断 MongoDB 全量兼容。

$project 选择列或改名，$set 计算结果列，均不修改数据库；$unwind 展开数组。独立一对多事实应在各自 $lookup.pipeline 内先聚合，再组合，避免明细交叉放大。参见 [客户合同回款示例](uns-v2/examples/customer-fact-summary.json)，将其中 collectionId 放入 URL 后直接发送 pipeline/page 正文。所有关联集合独立鉴权。kind=samples 的 lookup 仍要求固定 recordId，它是关联子查询约束，不等于顶层 samples:find 的多 recordIds 选项。

时间桶可用 $dateTrunc；binSize=1 支持日历单位和 IANA 时区，较大 binSize 支持 UTC millisecond/second/minute/hour/day 固定时长桶并以 Unix epoch 对齐。例如 {"$group":{"_id":{"$dateTrunc":{"date":"$_meta.time","unit":"hour","timezone":"Asia/Shanghai"}},"average":{"$avg":"$temperature"}}}。

示例：POST /openapi/v2/uns/collections/equipment/samples:find

~~~json
{"recordIds":["equipment-001","equipment-002"],"from":1791936000000,"to":1792022400000,"filter":{"_meta.quality":"good","temperature":{"$gte":20}},"sort":[["_meta.time",-1],["_id",-1]],"limit":20}
~~~

records:count 成功返回完整 {count}，超时返回错误，不返回截断计数。records:distinct 返回 {values,pageInfo}，values 保留 JSON 值类型，使用 page:{page,size} 做 offset 分页，不能同时传 limit。

Collection 使用 indexes 声明普通查询索引与 Record 唯一索引，业务字段不用 data. 前缀；describe.indexStatus.state=ready 后约束才生效。Record 的业务唯一索引在表上以 deleted_at IS NULL 过滤，软删除释放业务键。upsert 仅用于单条更新/替换，filter 必须有 _id 等值，或覆盖某个 ready 唯一索引全部字段等值；范围条件与 OR 条件不能替代。

<a id="transactions"></a>
## Record 业务事务

路径 `/uns/records:transact`，提交结构为 `{items,mode?,idempotencyKey?,dryRun?}`；预检设置 `dryRun:true`。每项 `opId` 唯一，可组合 `record.query/check/read/create/update/replace/delete`，不能混入 Meta 或 Sample，不接受 `allowPartial`、`atomic`、`ordered` 来改变原子语义。

`dependsOn` 表达执行依赖；`bindings` 可将前面操作结果绑定至允许的字段，例如 `data.foo`、`operations.0.value`。filter 使用 `filter/` 开头的 JSON Pointer，例如 `filter/quantity/$gte`；只能替换已存在的标量或 null 占位值，不能替换谓词结构。来源支持 `id/revision/collectionId/data.foo`；查询结果作为写入目标 ID 时只允许同 Collection。不能绑定权限、目标 Collection 或脚本；循环及无效引用导致整批失败。

`record.check` 支持存在性、条件和可选版本校验；`record.read` 使用统一 filter 选择记录，不接受版本校验。`record.update` 使用上述声明字段操作，在锁内检查条件；每个记录同一批次最多写一次。update/modify/replace 可使用 first 选择及 before/after 文档返回；事务中的条件查询、数量断言和动态目标绑定遵循服务端并发控制，失败整体回滚。

预检会真实执行后回滚。提交返回 `accepted` 时继续查询 Job；网络断开或超时不能证明回滚，不能直接把整个业务事务重发。

<a id="pagination"></a>
## 分页与有界读取

find 支持 limit/cursor 或 page，page 不与 limit/cursor 混用；aggregate 支持 page 或 limit。pageInfo 统一含 mode、hasMore、nextCursor、nextPage、snapshotScope=thisRequestOnly；不适用的下一页字段为 null。普通查询签名游标绑定查询、集合和当前身份；聚合使用 offset。继续翻页应保持查询和范围不变，不混用游标与页码。

默认 20 行，最多 200。跨请求不冻结快照，排序值修改、插入或删除可能影响后续翻页；items.length 不是总匹配数。需要计数单独调用 records:count。$skip/$limit 改变 pipeline 输入，page 只对完整 pipeline 的输出分页。元数据继续使用各自的 page/size 参数。

<a id="jobs"></a>
## 异步任务与提交结果

返回 `accepted` 只表示受理，使用 `jobId` 调用 `uns/jobs/query`。请分开判断执行进度和数据结果：

| 字段 | 值 | 含义 |
| --- | --- | --- |
| `executionStatus` | queued / running / finished | 工作器是否仍在处理 |
| `outcome` | pending / committed / partiallyCommitted / rolledBack / unknown | 持久回执能证明的写入结果 |
| `status` | queued / running / succeeded / failed | 旧兼容状态，不能单独用来判断回滚 |

queued/running 的 outcome 为 pending。finished 后 committed/partiallyCommitted 依据已保存结果判断；只要存在提交不明的项，unknown 优先，即便另一些项明确已提交。检查 `result.results` 和 `result.summary` 确认逐项结果。

rolledBack 要有明确证据，例如错误携带 `rolledBack:true`；单纯 failed、缺少结果或鉴权失败不证明没有写入。只重试已确认回滚且确实需要重试的失败项，不重试已提交项。unknown 先核对业务状态；finished + unknown 已是终态，持续轮询不保证变成确定结果。SDK 的 `getJobOutcome` 同样保守解释旧回执，不自动重试写入。

<a id="errors"></a>
## 错误处理

检查 HTTP 状态、业务 code、结构化 error，并保留 `requestId` 用于定位。`path` 定位字段，`opId` 定位事务步骤，`conditionIndex` 定位条件；`rolledBack` 只有明确返回时才构成回滚证据。不同接口错误字段见 Schema。

- 参数/Schema 错误：修正结构、类型和真实 ID；不要通过删掉约束来重试。
- 未认证/无权限：核对 Key、所属用户当前权限、App grants 和 platformPermissions。
- 版本或幂等冲突：读取现状后由业务决定下一步，不能悄悄覆盖或换键重复写入。
- 超时、连接中断、unknown：先查 Job 或业务状态。没有幂等保障时不自动重发写入。
- 部分提交：只处理明确失败且已回滚的项；通知和文件操作需独立核实。

<a id="legacy"></a>
## 兼容接口

旧混合数据查询和重复 Record/Sample 写入路径已从路由、契约和客户端移除，不保留兼容别名。新调用只使用本册资源路径。其他领域是否保留兼容接口以当前 catalog 为准，不推断数据接口仍兼容。

幂等重放必须保持当前有效操作、原键和原请求；不能把旧接口请求改形后重放为新意图。记录软删除恢复使用 records:restore 的 ids 数组，最多 100 个；整批校验当前 Schema/唯一约束，冲突整体回滚。同 ID 新增不会隐式复活。

## 文档维护

本文件由说明源 `backend/http/contracts/openapi-v2/guide.md` 与公开 OpenAPI 契约共同生成。修改契约或说明后运行 `node scripts/generate-uns-v2-api.mjs`，再运行 `node scripts/generate-mcp-docs.mjs` 同步 MCP 文档包；不手改 Swagger 或本总册生成内容。架构和验收资料保留在工程文档中，不作为另一份接口使用规范。
