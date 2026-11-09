# Native validation MQTT adapter

Windows 原生验证包专用；当前 Docker 部署继续使用 EMQX。

采用 Aedes 1.2.0 + ws，MQTT 3.1.1，TCP/WS 均仅监听回环地址。内部 Backend
凭据由安装器随机生成；其他连接调用现有 `/api/core/mqtt/auth`，发布/订阅调用
`/api/core/mqtt/acl`，超时或异常默认拒绝。现有 Backend 的踢客户端请求通过
带 Basic 认证的 `DELETE /api/v5/clients/:id` 适配。接口不模拟 EMQX Dashboard。

会话、retained、离线队列仅保存在内存，不具备 EMQX 集群或 MQTT 5 能力；
验证包尚未声明生产兼容。客户端 auth、QoS1 pub/sub 和 WS 在验收中验证。
启动入口为 `server.mjs`，环境参数由 `native-launcher` 提供，不在这里维护固定密码。
