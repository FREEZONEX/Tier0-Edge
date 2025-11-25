import { useEffect, useState, useCallback } from 'react'
import { useMqtt } from '../hooks/useMqtt'
import { topics, unsConfig } from '../config/mqtt'
import { CurrentJob, AlarmStatus, BoardCycleTime } from '../types'
import { LineChart, Line, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer } from 'recharts'

interface CycleTimeDataPoint {
  time: string
  cycleTime: number
}

export function Dashboard() {
  const [currentJob, setCurrentJob] = useState<CurrentJob | null>(null)
  const [alarmStatus, setAlarmStatus] = useState<AlarmStatus | null>(null)
  const [cycleTime, setCycleTime] = useState<number>(0)
  const [cycleTimeHistory, setCycleTimeHistory] = useState<CycleTimeDataPoint[]>([])
  const [boardsCount, setBoardsCount] = useState({ good: 0, bad: 0, total: 0 })

  // 添加调试：打印订阅的 topics
  useEffect(() => {
    console.log('Dashboard - Subscribing to topics:', {
      currentJob: topics.currentJob,
      alarmStatus: topics.alarmStatus,
      boardCycleTime: topics.boardCycleTime,
    })
  }, [])

  const { isConnected, subscribe } = useMqtt({
    topics: [topics.currentJob, topics.alarmStatus, topics.boardCycleTime],
    onMessage: useCallback((topic: string, message: Buffer) => {
      console.log('Dashboard - Received message:', { 
        topic, 
        expectedTopics: {
          currentJob: topics.currentJob,
          alarmStatus: topics.alarmStatus,
          boardCycleTime: topics.boardCycleTime,
        },
        message: message.toString() 
      })
      try {
        const data = JSON.parse(message.toString())

        // 使用严格匹配
        if (topic === topics.currentJob) {
          console.log('Dashboard - Processing currentJob:', data)
          setCurrentJob(data as CurrentJob)
        } else if (topic === topics.alarmStatus) {
          console.log('Dashboard - Processing alarmStatus:', data)
          setAlarmStatus(data as AlarmStatus)
        } else if (topic === topics.boardCycleTime) {
          console.log('Dashboard - Processing boardCycleTime:', data)
          const cycleData = data as BoardCycleTime
          setCycleTime(cycleData.cycle_time_ms || 0)

          // 更新历史数据（保留最近50个点）
          const now = new Date().toLocaleTimeString()
          setCycleTimeHistory((prev) => {
            const newHistory = [...prev, { time: now, cycleTime: cycleData.cycle_time_ms || 0 }]
            return newHistory.slice(-50)
          })

          // 如果有 boards_count 数据，更新计数
          if ('boards_count' in cycleData) {
            const count = cycleData.boards_count as { good?: number; bad?: number; total?: number }
            setBoardsCount({
              good: count.good || 0,
              bad: count.bad || 0,
              total: count.total || 0,
            })
          }
        }
      } catch (error) {
        console.error('Failed to parse MQTT message:', error)
      }
    }, []),
  })

  const hasAlarm = alarmStatus && (alarmStatus.code || alarmStatus.msg)
  const statusColor = hasAlarm ? 'bg-red-500' : 'bg-green-500'

  return (
    <div className="p-6 space-y-6">
      <div className="mb-6">
        <h1 className="text-3xl font-bold text-gray-800">实时数据看板</h1>
        <div className="mt-2 text-sm text-gray-600">
          <span className="font-semibold">Plant:</span> {unsConfig.enterprise} →{' '}
          <span className="font-semibold">Device:</span> {unsConfig.device}
        </div>
        <div className="mt-2 flex items-center gap-2">
          <div className={`w-3 h-3 rounded-full ${isConnected ? 'bg-green-500' : 'bg-red-500'}`} />
          <span className="text-sm text-gray-600">
            MQTT {isConnected ? '已连接' : '未连接'}
          </span>
        </div>
      </div>

      {/* 状态卡片 */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
        {/* 当前任务卡片 */}
        <div className={`bg-white rounded-lg shadow-md p-6 border-l-4 ${statusColor}`}>
          <h2 className="text-xl font-semibold mb-4 text-gray-800">当前任务状态</h2>
          {currentJob ? (
            <div className="space-y-2">
              <div className="flex justify-between">
                <span className="text-gray-600">工单号:</span>
                <span className="font-semibold">{currentJob.job_id}</span>
              </div>
              {currentJob.product_id && (
                <div className="flex justify-between">
                  <span className="text-gray-600">产品号:</span>
                  <span className="font-semibold">{currentJob.product_id}</span>
                </div>
              )}
              {currentJob.planned_quantity !== undefined && (
                <div className="flex justify-between">
                  <span className="text-gray-600">计划数量:</span>
                  <span className="font-semibold">{currentJob.planned_quantity}</span>
                </div>
              )}
              {currentJob.completed_quantity !== undefined && (
                <div className="flex justify-between">
                  <span className="text-gray-600">完成数量:</span>
                  <span className="font-semibold">{currentJob.completed_quantity}</span>
                </div>
              )}
              <div className="flex justify-between">
                <span className="text-gray-600">状态码:</span>
                <span className="font-semibold">{currentJob.status}</span>
              </div>
            </div>
          ) : (
            <div className="text-gray-400">等待数据...</div>
          )}
        </div>

        {/* 报警状态卡片 */}
        <div className={`bg-white rounded-lg shadow-md p-6 border-l-4 ${hasAlarm ? 'border-red-500' : 'border-green-500'}`}>
          <h2 className="text-xl font-semibold mb-4 text-gray-800">报警状态</h2>
          {hasAlarm ? (
            <div className="space-y-2">
              {alarmStatus?.code && (
                <div className="flex justify-between">
                  <span className="text-gray-600">报警代码:</span>
                  <span className="font-semibold text-red-600">{alarmStatus.code}</span>
                </div>
              )}
              {alarmStatus?.msg && (
                <div className="flex justify-between">
                  <span className="text-gray-600">报警信息:</span>
                  <span className="font-semibold text-red-600">{alarmStatus.msg}</span>
                </div>
              )}
            </div>
          ) : (
            <div className="text-green-600 font-semibold">正常运行</div>
          )}
        </div>
      </div>

      {/* 指标图表区域 */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        {/* 周期时间 */}
        <div className="bg-white rounded-lg shadow-md p-6">
          <h2 className="text-xl font-semibold mb-4 text-gray-800">周期时间 (ms)</h2>
          <div className="text-4xl font-bold text-blue-600 mb-4">{cycleTime.toLocaleString()}</div>
          {cycleTimeHistory.length > 0 && (
            <ResponsiveContainer width="100%" height={200}>
              <LineChart data={cycleTimeHistory}>
                <CartesianGrid strokeDasharray="3 3" />
                <XAxis dataKey="time" />
                <YAxis />
                <Tooltip />
                <Line type="monotone" dataKey="cycleTime" stroke="#3b82f6" strokeWidth={2} />
              </LineChart>
            </ResponsiveContainer>
          )}
        </div>

        {/* 良品/不良品数 */}
        <div className="bg-white rounded-lg shadow-md p-6">
          <h2 className="text-xl font-semibold mb-4 text-gray-800">生产计数</h2>
          <div className="space-y-4">
            <div className="flex justify-between items-center p-4 bg-green-50 rounded-lg">
              <span className="text-gray-700">良品数:</span>
              <span className="text-2xl font-bold text-green-600">{boardsCount.good}</span>
            </div>
            <div className="flex justify-between items-center p-4 bg-red-50 rounded-lg">
              <span className="text-gray-700">不良品数:</span>
              <span className="text-2xl font-bold text-red-600">{boardsCount.bad}</span>
            </div>
            <div className="flex justify-between items-center p-4 bg-blue-50 rounded-lg">
              <span className="text-gray-700">总数:</span>
              <span className="text-2xl font-bold text-blue-600">{boardsCount.total}</span>
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}

