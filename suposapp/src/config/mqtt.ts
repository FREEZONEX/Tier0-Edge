import { UNSTopicPath } from '../types'

// 判断是否为生产环境
const isProduction = import.meta.env.PROD

// 从环境变量读取配置，提供默认值
// 生产环境默认使用 emqx，开发环境使用 localhost
const defaultHost = isProduction ? 'emqx' : 'localhost'
const defaultProtocol = import.meta.env.VITE_MQTT_PROTOCOL || 'ws'

// 根据协议类型确定默认端口
// WebSocket (ws/wss) 使用 8083，TCP (mqtt/mqtts) 使用 1883
const getDefaultPort = (protocol: string): number => {
  if (protocol === 'ws' || protocol === 'wss') {
    return 8083
  }
  return 1883
}

export const mqttConfig = {
  host: import.meta.env.VITE_MQTT_HOST || defaultHost,
  port: import.meta.env.VITE_MQTT_PORT
    ? parseInt(import.meta.env.VITE_MQTT_PORT, 10)
    : getDefaultPort(defaultProtocol),
  protocol: defaultProtocol,
  path: import.meta.env.VITE_MQTT_PATH || '/mqtt',
}

// UNS 层级配置
export const unsConfig = {
  enterprise: import.meta.env.VITE_ENTERPRISE || 'Plant_Name',
  site: import.meta.env.VITE_SITE || 'SMT-Area-1',
  area: import.meta.env.VITE_AREA || 'SMT-Line-1',
  line: import.meta.env.VITE_LINE || 'Printer-Cell',
  device: import.meta.env.VITE_DEVICE || 'Printer01',
}

// 构建 MQTT 连接 URL
export function getMqttUrl(): string {
  const { host, port, protocol, path } = mqttConfig
  if (protocol === 'ws' || protocol === 'wss') {
    return `${protocol}://${host}:${port}${path}`
  }
  return `${protocol}://${host}:${port}`
}

// 构建 UNS Topic 路径
export function buildUNSTopic(path: UNSTopicPath): string {
  const { enterprise, site, area, line, cell, device, namespace, topic } = path
  return `v1/${enterprise}/${site}/${area}/${line}/${cell}/${device}/${namespace}/${topic}`
}

// 获取完整 Topic 路径（使用配置的层级）
// 根据需求文档，实际 topic 结构是：v1/<Enterprise>/<Site>/<Area>/<Line>/<Device>/<Namespace>/<Topic>
// 没有 Cell 层级，所以直接构建为：v1/<Enterprise>/<Site>/<Area>/<Line>/<Device>/<Namespace>/<Topic>
export function getTopic(namespace: 'State' | 'Action' | 'Metric', topic: string): string {
  // 直接构建，不使用 Cell 层级
  const topicPath = `v1/${unsConfig.enterprise}/${unsConfig.site}/${unsConfig.area}/${unsConfig.line}/${unsConfig.device}/${namespace}/${topic}`
  console.log(`Built topic for ${namespace}/${topic}:`, topicPath)
  return topicPath
}

// 预定义的 Topic
export const topics = {
  currentJob: getTopic('State', 'current_job'),
  alarmStatus: getTopic('State', 'alarm_status'),
  boardCycleTime: getTopic('Metric', 'board_cycle_time'),
  startJob: getTopic('Action', 'start_job'),
  stopJob: getTopic('Action', 'stop_job'),
}

