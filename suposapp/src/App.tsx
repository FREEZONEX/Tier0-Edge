import { useState } from 'react'
import { Dashboard } from './components/Dashboard'
import { JobEntry } from './components/JobEntry'
import { ToastContainer } from './components/Toast'

type Tab = 'dashboard' | 'jobEntry'

interface Toast {
  id: string
  message: string
  type?: 'success' | 'error' | 'info'
}

function App() {
  const [activeTab, setActiveTab] = useState<Tab>('dashboard')
  const [toasts, setToasts] = useState<Toast[]>([])

  const showToast = (message: string, type: 'success' | 'error' | 'info' = 'info') => {
    const id = Date.now().toString()
    setToasts((prev) => [...prev, { id, message, type }])
  }

  const removeToast = (id: string) => {
    setToasts((prev) => prev.filter((toast) => toast.id !== id))
  }

  return (
    <div className="min-h-screen bg-gray-100">
      {/* 导航栏 */}
      <nav className="bg-white shadow-md">
        <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8">
          <div className="flex justify-between h-16">
            <div className="flex">
              <div className="flex-shrink-0 flex items-center">
                <h1 className="text-xl font-bold text-gray-800">UNS 工业物联网前端</h1>
              </div>
              <div className="hidden sm:ml-6 sm:flex sm:space-x-8">
                <button
                  onClick={() => setActiveTab('dashboard')}
                  className={`${
                    activeTab === 'dashboard'
                      ? 'border-blue-500 text-gray-900'
                      : 'border-transparent text-gray-500 hover:border-gray-300 hover:text-gray-700'
                  } inline-flex items-center px-1 pt-1 border-b-2 text-sm font-medium transition-colors`}
                >
                  实时数据看板
                </button>
                <button
                  onClick={() => setActiveTab('jobEntry')}
                  className={`${
                    activeTab === 'jobEntry'
                      ? 'border-blue-500 text-gray-900'
                      : 'border-transparent text-gray-500 hover:border-gray-300 hover:text-gray-700'
                  } inline-flex items-center px-1 pt-1 border-b-2 text-sm font-medium transition-colors`}
                >
                  报工工单录入
                </button>
              </div>
            </div>
          </div>
        </div>
      </nav>

      {/* 主内容区 */}
      <main className="max-w-7xl mx-auto py-6">
        {activeTab === 'dashboard' && <Dashboard />}
        {activeTab === 'jobEntry' && <JobEntry onSuccess={(msg) => showToast(msg, 'success')} />}
      </main>

      {/* Toast 通知 */}
      <ToastContainer toasts={toasts} onRemove={removeToast} />
    </div>
  )
}

export default App

