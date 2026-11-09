# Connect 协议标识

用于识别连接协议，保留原始品牌图形和颜色，随应用本地发布。

| 文件         | 来源                                                                                                                                                                             | 展示用途          |
| ------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------- |
| `mqtt.svg`   | [MQTT 官方素材库](https://github.com/mqtt/mqttorg-graphics/blob/master/svg/mqtt-icon-solid.svg)                                                                                  | MQTT、MQTT Device |
| `modbus.png` | [Modbus 官网](https://www.modbus.org/)使用的[原始图片](https://assets.noviams.com/novi-file-uploads/modbus/structure/main_logo.png)                                              | Modbus            |
| `opcua.png`  | [OPC UA 标识素材](https://seeklogo.com/vector-logo/355019/opc-unified-architecture-ua)，已与 [OPC Foundation 官方手册](https://opcfoundation.org/opcua-en.pdf)封面的技术标识核对 | OPC UA            |

素材获取日期：2026-10-26。OPC UA 图片为第三方提供的官方标识副本，不使用基金会组织标识或产品认证标识。

MQTT 素材的使用说明见[官方 README](https://github.com/mqtt/mqttorg-graphics/blob/master/README.md)；各标识的商标权归相应权利人所有。

`SourceProtocolGlyph` 通过 SVG viewBox 排除 PNG 素材四周留白，完整显示标识并等比缩放。HTTP Client、HTTP Server、SQL、UNS 及未知协议继续使用语义图标，不以特定厂商的品牌代表通用协议。
