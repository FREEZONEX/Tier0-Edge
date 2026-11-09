# Tier0 App data client

Server-side SDK for OpenAPI v2 **4.0.0 / data/3**. Keep API keys in the trusted server; all operations check current App Collection grants. The [unified contract](../../doc/uns-v2/data-api.md) describes API shapes, limits and the retained platform pipeline grammar.

```ts
const client = createAppDataClient({apiBase:'https://platform.example/openapi/v2',apiKey:serverSecret});
const orders = await client.collection('orders');
await orders.insertOne({status:'new',quantity:1},{id:'order-1',idempotencyKey:'order-1-create'});
await orders.updateOne({_id:'order-1'},{$inc:{quantity:2}},{idempotencyKey:'order-1-add-2'});
const rows = await orders.find({filter:{status:'new'},sort:[['_meta.createdAt',-1],['_id',1]],limit:20});
await orders.deleteOne({_id:'order-1'},{idempotencyKey:'order-1-delete'});
```

查询条件使用裸业务字段和 `_id` / `_meta.*`。普通 Record 响应统一为 `{id,collectionId,data,revision,createdAt,updatedAt,sourceId,sourceTime,lock?}`，Sample find/latest 统一为 `{id,collectionId,recordId,time,receivedAt,quality,data,sourceId,streamId?,bindingId?}`。仅 aggregate 返回 pipeline 自定义文档；查询 `temperature` 对应资源结果的 `data.temperature`。

接口统一为 `/uns/collections/{collectionId}/records:find` 等集合动作。新增单条使用 `/records`，findOneAndUpdate/Replace/Delete 返回 `{value:Record|null}`。upsert 通过单条 update/replace 的 `upsert:true` 选项设置。删除 Record 为软删除；调用 `records.restore(collectionId, ids)` 原子恢复多个 ID，新增同 ID 不隐式恢复。历史 Sample 保留。

批量写入的 insertedIds/upsertedIds 为原始输入索引到 ID 的对象，例如 `{"0":"r1","2":"r3"}`；writeErrors[].index 同样指原始索引。检查 status/replayed/writeErrors/hasMore。SDK 不自动重试写入；超时不证明回滚，重试须保持原请求和 idempotencyKey。

`samples.find(collectionId,{recordIds?,from,to,filter?,sort?,projection?,page?})` 和 `samples.aggregate(collectionId,pipeline,{recordIds?,from,to,page?})` 支持最多 20 个 Record，省略 recordIds 表示当前授权集合。时间范围为 `[from,to)`。latest/delete 必须明确 recordIds；没有采样返回 null。append 通过 `/collections/{collectionId}/records/{recordId}/samples` 追加数组。find/aggregate 返回 items/pageInfo；游标和 page/limit 不能混用。

使用 `records.lock/renew/unlock` 管理租约，lock 返回 locks 中每项的 token 和 canonical record；写入通过 lockToken（批量用 lockTokens）携带租约。restore 接收 ids 数组。`client.transact(items,{dryRun?,idempotencyKey?})` 使用 `/uns/records:transact`，只接受 Record 操作，支持依赖、条件和结果绑定；Sample 通过 append 单独追加，不保证与 Record 事务一起原子提交。事务结果的 document/query.records 也是 canonical Record，data 仍为绑定使用的业务值。旧混合查询、重复 CRUD 和批处理别名已移除。

## Cloud v2 Stream 请求

新发布的 `entry.request` 和平台生成的隐式请求使用 `/openapi/v2/stream-requests`：

```ts
const requests = await client.streamRequests.discover({search: 'device', size: 20});
const request = client.streamRequests.use<{deviceId: string}, {accepted: boolean}>({code: 'device.start'});
const contract = await request.describe();
const receipt = await request.invoke({deviceId: 'device-01'}, {idempotencyKey: savedIntentId});
// receipt: {code, contractVersion, result, runID, status, replayed}
```

发现/详情要求 `stream.requests.read`，普通服务端 Key 的 `stream.requests.manage` 也可读取。调用单独要求 `stream.requests.invoke`；有写入副作用的合同另需 `stream.requests.write`，且必须携带持久幂等键。只读合同按 `idempotency` 决定是否需键；无输入使用 `null`。请求体为 `{input,idempotencyKey?}`，最多 1 MiB；键最多 128 个 UTF-8 字节。

App 通过现有 `platformPermissions` 显式授予 read/invoke/write 后签发 Key；权限与所属用户、Key 当前范围取交集，实时检查 App 停用与撤权。此接入尚不包含 Cloud 外部 stream App 注册或 hosted stream policy。`controls` 继续支持旧接口。

错误通过 `AppDataError.code` 保留 Cloud 的 `STREAM_REQUEST_*` 或 `IDEMPOTENCY_CONFLICT`。网络失败或超时不能证明写入未发生；SDK 不自动重试，也不替调用方生成新幂等键。成功时 `result` 可为 `null`，`runID` 是不透明运行标识；执行成功不等于设备已完成动作。

## 调用 Stream 注册的控制口

App 使用稳定 `code`，不需要保存 Stream ID、入口或部署版本：

```ts
const controls = await client.controls.discover({search: 'device', status: 'active'});
const start = client.controls.use<{deviceId: string}, {accepted: boolean}>({code: 'device.start'});
const contract = await start.describe();
const receipt = await start.invoke({deviceId: 'device-01'}, {idempotencyKey: savedIntentId});
```

`savedIntentId` 必须由业务服务在首次发送前持久保存。同键重放只读取原运行结果，不再次执行设备操作；SDK 不自动重试。`receipt.result` 是业务输出，`receipt.execution` 是平台运行回执，不代表设备已完成动作。

控制入口使用服务端 API Key：发现/详情需要 `control.read`，调用需要 `control.execute`，有设备副作用的控制还需要 `control.write`。正式 App 必须在 `platformPermissions` 中显式声明这些权限，再签发绑定该 App 的 Key；App Key 不支持 `control.manage`，旧 Key 不会因 App 后续扩权而自动获得新权限。每次请求重新检查当前 App 状态和授权，撤权、停用 App 或撤销 Key 均会阻止后续调用。凭据仅放服务端，不把 Source 密钥或 API Key 下发浏览器。

业务 JSON 原样作为 Body，幂等键在 Header；同步调用不经过 NATS。异步也可通过稳定 Control 入口可靠写入专用命令 Record，再由 UNS 事件 → 独立下发 Stream → Source 执行；入口返回“已受理”不代表设备执行完成，须查询业务回执并回读状态。不需要为此授予 App 通用命令数据写权限或新增队列 SDK。多协议验收和部署记录统一维护在文档仓库 TASK-031 第 74 篇。跨语言验收入口：

```sh
cd backend
# 独立测试 PG + Node >=22.18；不访问部署环境。
go test -buildvcs=false ./internal/logic/openapi/v1/operationresource -count=1
```
