/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_MQTT_HOST: string
  readonly VITE_MQTT_PORT: string
  readonly VITE_MQTT_PROTOCOL: string
  readonly VITE_MQTT_PATH: string
  readonly VITE_API_BASE_URL: string
  readonly VITE_ENTERPRISE: string
  readonly VITE_SITE: string
  readonly VITE_AREA: string
  readonly VITE_LINE: string
  readonly VITE_DEVICE: string
}

interface ImportMeta {
  readonly env: ImportMetaEnv
}

