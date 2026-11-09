import type { SystemInfoProps } from '@/stores/types';

export const resolveMqttWebsocketUrl = (systemInfo?: SystemInfoProps): string => {
  if (systemInfo?.mqttWebsocketUrl) return systemInfo.mqttWebsocketUrl;
  const secure = window.location.protocol === 'https:';
  const port = secure
    ? systemInfo?.mqttWebsocketTslPort || window.location.port || '8084'
    : systemInfo?.mqttWebsocketPort || window.location.port || '8083';
  return `${secure ? 'wss' : 'ws'}://${window.location.hostname}:${port}/mqtt`;
};

export const resolveMqttWebsocketPort = (url: string): string => {
  const endpoint = new URL(url);
  return endpoint.port || (endpoint.protocol === 'wss:' ? '443' : '80');
};
