// UNS Topic 结构类型
export interface UNSTopicPath {
  enterprise: string
  site: string
  area: string
  line: string
  cell: string
  device: string
  namespace: 'State' | 'Action' | 'Metric'
  topic: string
}

// State 数据类型
export interface CurrentJob {
  job_id: number
  product_id?: number
  planned_quantity?: number
  completed_quantity?: number
  status: number
  [key: string]: unknown
}

export interface AlarmStatus {
  code?: string
  msg?: string
  level?: 'info' | 'warning' | 'error'
  [key: string]: unknown
}

// Metric 数据类型
export interface BoardCycleTime {
  cycle_time_ms: number
  ts: number | string
  [key: string]: unknown
}

export interface BoardsCount {
  good_count?: number
  bad_count?: number
  total_count?: number
  [key: string]: unknown
}

// Action 数据类型
export interface StartJobAction {
  job_id: number
  product_id: number
  planned_quantity?: number
  [key: string]: unknown
}

export interface StopJobAction {
  job_id: number
  reason?: string
  [key: string]: unknown
}

