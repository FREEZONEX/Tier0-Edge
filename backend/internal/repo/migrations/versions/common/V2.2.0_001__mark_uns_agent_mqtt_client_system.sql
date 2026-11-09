UPDATE uns_connect_client
SET type = 4,
    updated_time = CURRENT_TIMESTAMP
WHERE deleted_time = 0
  AND type = 1
  AND name = 'uns-agent-browser'
  AND description = 'uns-agent 浏览器 MQTT-over-WS 桥自动创建，供 UNS Agent 画布卡片实时订阅使用，可在「MQTT 鉴权」页管理/吊销。'
  AND client_id_random_suffix_enabled = true;
