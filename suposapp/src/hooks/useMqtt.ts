import { useEffect, useRef, useState, useCallback, useMemo } from 'react'
import mqtt, { MqttClient } from 'mqtt'
import { getMqttUrl } from '../config/mqtt'

export interface UseMqttOptions {
  topics?: string[]
  onMessage?: (topic: string, message: Buffer) => void
}

export interface UseMqttReturn {
  client: MqttClient | null
  isConnected: boolean
  publish: (topic: string, payload: string | object) => void
  subscribe: (topic: string) => void
  unsubscribe: (topic: string) => void
}

// 全局 MQTT 客户端实例（单例模式）
let globalMqttClient: MqttClient | null = null
let globalConnectionCount = 0
// 全局消息处理器列表（支持多个组件订阅）
const globalMessageHandlers = new Set<(topic: string, message: Buffer) => void>()

export function useMqtt(options: UseMqttOptions = {}): UseMqttReturn {
  const { topics: initialTopics = [], onMessage } = options
  const [client, setClient] = useState<MqttClient | null>(null)
  const [isConnected, setIsConnected] = useState(false)
  const clientRef = useRef<MqttClient | null>(null)
  
  // 使用 useMemo 稳定 initialTopics 引用
  const stableInitialTopics = useMemo(() => initialTopics, [initialTopics.join(',')])
  const topicsRef = useRef<Set<string>>(new Set(stableInitialTopics))
  
  // 使用 useRef 保存 onMessage 回调，避免依赖变化导致重新连接
  const onMessageRef = useRef(onMessage)
  const previousOnMessageRef = useRef<typeof onMessage>()
  
  // 更新 onMessage 引用，但不触发重新连接
  useEffect(() => {
    // 如果回调变化，更新全局处理器列表
    if (previousOnMessageRef.current && globalMessageHandlers.has(previousOnMessageRef.current)) {
      globalMessageHandlers.delete(previousOnMessageRef.current)
    }
    if (onMessage && globalMqttClient && globalMqttClient.connected) {
      globalMessageHandlers.add(onMessage)
    }
    previousOnMessageRef.current = onMessage
    onMessageRef.current = onMessage
  }, [onMessage])

  useEffect(() => {
    // 如果已有全局连接，直接使用
    if (globalMqttClient && globalMqttClient.connected) {
      console.log('Reusing existing MQTT connection')
      clientRef.current = globalMqttClient
      setClient(globalMqttClient)
      setIsConnected(true)
      globalConnectionCount++
      
      // 订阅初始 topics（延迟订阅确保连接稳定）
      setTimeout(() => {
        if (globalMqttClient && globalMqttClient.connected) {
          stableInitialTopics.forEach((topic) => {
            if (!topicsRef.current.has(topic)) {
              topicsRef.current.add(topic)
              console.log(`Subscribing to topic: ${topic}`)
              globalMqttClient!.subscribe(topic, { qos: 0 }, (err) => {
                if (err) {
                  console.error(`Failed to subscribe to ${topic}:`, err)
                } else {
                  console.log(`Successfully subscribed to ${topic}`)
                }
              })
            } else {
              console.log(`Already subscribed to ${topic}`)
            }
          })
        }
      }, 100)
      
      // 更新 topicsRef
      stableInitialTopics.forEach((topic) => {
        topicsRef.current.add(topic)
      })
      
      // 添加当前组件的消息处理器到全局列表
      if (onMessageRef.current) {
        globalMessageHandlers.add(onMessageRef.current)
      }
      
      // 确保全局消息分发器已设置
      if (!globalMqttClient.listenerCount('message')) {
        globalMqttClient.on('message', (topic: string, message: Buffer) => {
          console.log(`Global message handler - Received on topic: ${topic}`, message.toString())
          // 分发消息给所有注册的处理器
          globalMessageHandlers.forEach((handler) => {
            try {
              handler(topic, message)
            } catch (error) {
              console.error('Error in message handler:', error)
            }
          })
        })
      }
      
      return () => {
        // 移除当前组件的消息处理器
        if (onMessageRef.current) {
          globalMessageHandlers.delete(onMessageRef.current)
        }
        
        globalConnectionCount--
        // 只有当没有其他组件使用连接时才关闭
        if (globalConnectionCount <= 0 && globalMqttClient) {
          console.log('Closing MQTT connection (no more subscribers)')
          globalMqttClient.end()
          globalMqttClient = null
          globalConnectionCount = 0
          globalMessageHandlers.clear()
        }
      }
    }

    // 创建新连接
    const mqttUrl = getMqttUrl()
    console.log('Connecting to MQTT:', mqttUrl)

    const mqttClient = mqtt.connect(mqttUrl, {
      clientId: `uns-frontend-${Math.random().toString(16).substring(2, 10)}`,
      reconnectPeriod: 5000,
      connectTimeout: 10000,
      keepalive: 60, // 60秒心跳，保持长连接
      clean: true, // 清理会话
      resubscribe: true, // 自动重新订阅
    })

    mqttClient.on('connect', () => {
      console.log('MQTT Connected')
      setIsConnected(true)
      
      // 延迟订阅，确保连接完全建立
      setTimeout(() => {
        if (mqttClient.connected) {
          // 订阅初始 topics
          stableInitialTopics.forEach((topic) => {
            topicsRef.current.add(topic)
            console.log(`Subscribing to topic: ${topic}`)
            mqttClient.subscribe(topic, { qos: 0 }, (err) => {
              if (err) {
                console.error(`Failed to subscribe to ${topic}:`, err)
              } else {
                console.log(`Successfully subscribed to ${topic}`)
              }
            })
          })
        } else {
          console.warn('MQTT client not connected when trying to subscribe')
        }
      }, 100) // 延迟 100ms 确保连接稳定
    })

    mqttClient.on('error', (error) => {
      console.error('MQTT Error:', error)
      setIsConnected(false)
    })

    mqttClient.on('close', () => {
      console.log('MQTT Disconnected')
      setIsConnected(false)
      // 如果这是全局连接，清除引用
      if (globalMqttClient === mqttClient) {
        globalMqttClient = null
        globalConnectionCount = 0
      }
    })

    mqttClient.on('reconnect', () => {
      console.log('MQTT Reconnecting...')
    })

    mqttClient.on('offline', () => {
      console.log('MQTT Offline')
      setIsConnected(false)
    })

    // 添加当前组件的消息处理器到全局列表
    if (onMessageRef.current) {
      globalMessageHandlers.add(onMessageRef.current)
    }
    
    // 设置全局消息分发器
    mqttClient.on('message', (topic, message) => {
      // 添加调试日志
      console.log(`Global message handler - Received on topic: ${topic}`, message.toString())
      // 分发消息给所有注册的处理器
      globalMessageHandlers.forEach((handler) => {
        try {
          handler(topic, message)
        } catch (error) {
          console.error('Error in message handler:', error)
        }
      })
    })

    clientRef.current = mqttClient
    setClient(mqttClient)
    globalMqttClient = mqttClient
    globalConnectionCount++

    return () => {
      // 移除当前组件的消息处理器
      if (onMessageRef.current) {
        globalMessageHandlers.delete(onMessageRef.current)
      }
      
      globalConnectionCount--
      // 只有当没有其他组件使用连接时才关闭
      if (globalConnectionCount <= 0) {
        console.log('Closing MQTT connection (no more subscribers)')
        if (mqttClient) {
          mqttClient.end()
        }
        globalMqttClient = null
        globalConnectionCount = 0
        globalMessageHandlers.clear()
      }
    }
  }, [stableInitialTopics]) // 只在 topics 真正变化时重新连接

  const publish = useCallback((topic: string, payload: string | object) => {
    if (clientRef.current && isConnected) {
      const payloadStr = typeof payload === 'string' ? payload : JSON.stringify(payload)
      clientRef.current.publish(topic, payloadStr, (err) => {
        if (err) {
          console.error(`Failed to publish to ${topic}:`, err)
        } else {
          console.log(`Published to ${topic}:`, payloadStr)
        }
      })
    } else {
      console.warn('MQTT client not connected, cannot publish')
    }
  }, [isConnected])

  const subscribe = useCallback((topic: string) => {
    if (clientRef.current && isConnected) {
      if (!topicsRef.current.has(topic)) {
        topicsRef.current.add(topic)
        console.log(`Manually subscribing to topic: ${topic}`)
        clientRef.current.subscribe(topic, { qos: 0 }, (err) => {
          if (err) {
            console.error(`Failed to subscribe to ${topic}:`, err)
          } else {
            console.log(`Successfully subscribed to ${topic}`)
          }
        })
      } else {
        console.log(`Already subscribed to ${topic}`)
      }
    } else {
      console.warn(`Cannot subscribe to ${topic}: client not connected`)
    }
  }, [isConnected])

  const unsubscribe = useCallback((topic: string) => {
    if (clientRef.current && isConnected) {
      topicsRef.current.delete(topic)
      clientRef.current.unsubscribe(topic, (err) => {
        if (err) {
          console.error(`Failed to unsubscribe from ${topic}:`, err)
        } else {
          console.log(`Unsubscribed from ${topic}`)
        }
      })
    }
  }, [isConnected])

  return {
    client,
    isConnected,
    publish,
    subscribe,
    unsubscribe,
  }
}

