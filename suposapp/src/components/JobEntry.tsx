import { useState } from 'react'
import { useMqtt } from '../hooks/useMqtt'
import { topics } from '../config/mqtt'
import { StartJobAction, StopJobAction } from '../types'

interface JobEntryProps {
  onSuccess?: (message: string) => void
}

export function JobEntry({ onSuccess }: JobEntryProps) {
  const [jobId, setJobId] = useState<string>('')
  const [productId, setProductId] = useState<string>('')
  const [plannedQuantity, setPlannedQuantity] = useState<string>('')
  const [stopReason, setStopReason] = useState<string>('')

  const { isConnected, publish } = useMqtt()

  const handleStartJob = () => {
    if (!jobId || !productId) {
      alert('请填写工单号和产品号')
      return
    }

    const payload: StartJobAction = {
      job_id: parseInt(jobId, 10),
      product_id: parseInt(productId, 10),
    }

    if (plannedQuantity) {
      payload.planned_quantity = parseInt(plannedQuantity, 10)
    }

    publish(topics.startJob, payload)
    if (onSuccess) {
      onSuccess('开始任务指令已下发')
    }

    // 清空表单
    setJobId('')
    setProductId('')
    setPlannedQuantity('')
  }

  const handleStopJob = () => {
    if (!jobId) {
      alert('请填写工单号')
      return
    }

    const payload: StopJobAction = {
      job_id: parseInt(jobId, 10),
    }

    if (stopReason) {
      payload.reason = stopReason
    }

    publish(topics.stopJob, payload)
    if (onSuccess) {
      onSuccess('停止任务指令已下发')
    }

    // 清空表单
    setJobId('')
    setStopReason('')
  }

  return (
    <div className="p-6">
      <h1 className="text-3xl font-bold text-gray-800 mb-6">报工工单录入</h1>

      {!isConnected && (
        <div className="mb-4 p-4 bg-yellow-50 border border-yellow-200 rounded-lg">
          <p className="text-yellow-800">警告: MQTT 未连接，无法发送指令</p>
        </div>
      )}

      <div className="bg-white rounded-lg shadow-md p-6 space-y-6">
        {/* 开始任务表单 */}
        <div>
          <h2 className="text-xl font-semibold mb-4 text-gray-800">开始任务</h2>
          <div className="space-y-4">
            <div>
              <label className="block text-sm font-medium text-gray-700 mb-2">
                工单号 (Job ID) <span className="text-red-500">*</span>
              </label>
              <input
                type="number"
                value={jobId}
                onChange={(e) => setJobId(e.target.value)}
                className="w-full px-4 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 focus:border-transparent"
                placeholder="请输入工单号"
              />
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-700 mb-2">
                产品号 (Product ID) <span className="text-red-500">*</span>
              </label>
              <input
                type="number"
                value={productId}
                onChange={(e) => setProductId(e.target.value)}
                className="w-full px-4 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 focus:border-transparent"
                placeholder="请输入产品号"
              />
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-700 mb-2">
                计划数量 (Planned Quantity)
              </label>
              <input
                type="number"
                value={plannedQuantity}
                onChange={(e) => setPlannedQuantity(e.target.value)}
                className="w-full px-4 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 focus:border-transparent"
                placeholder="请输入计划数量（可选）"
              />
            </div>
            <button
              onClick={handleStartJob}
              disabled={!isConnected}
              className="w-full bg-blue-600 text-white py-3 px-6 rounded-lg font-semibold hover:bg-blue-700 disabled:bg-gray-400 disabled:cursor-not-allowed transition-colors"
            >
              开始任务 (Start Job)
            </button>
          </div>
        </div>

        <div className="border-t border-gray-200 pt-6">
          {/* 停止任务表单 */}
          <h2 className="text-xl font-semibold mb-4 text-gray-800">停止任务</h2>
          <div className="space-y-4">
            <div>
              <label className="block text-sm font-medium text-gray-700 mb-2">
                工单号 (Job ID) <span className="text-red-500">*</span>
              </label>
              <input
                type="number"
                value={jobId}
                onChange={(e) => setJobId(e.target.value)}
                className="w-full px-4 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 focus:border-transparent"
                placeholder="请输入工单号"
              />
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-700 mb-2">
                停止原因 (Reason)
              </label>
              <input
                type="text"
                value={stopReason}
                onChange={(e) => setStopReason(e.target.value)}
                className="w-full px-4 py-2 border border-gray-300 rounded-lg focus:ring-2 focus:ring-blue-500 focus:border-transparent"
                placeholder="请输入停止原因（可选，如：finish）"
              />
            </div>
            <button
              onClick={handleStopJob}
              disabled={!isConnected}
              className="w-full bg-red-600 text-white py-3 px-6 rounded-lg font-semibold hover:bg-red-700 disabled:bg-gray-400 disabled:cursor-not-allowed transition-colors"
            >
              停止任务 (Stop Job)
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}

