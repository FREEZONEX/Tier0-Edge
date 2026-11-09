import { coreApi } from './core-adapter';

export interface HIDInputDevice {
  endpoint: string;
  legacyEndpoint?: string;
  label: string;
}

export const usbHidApi = {
  devices: (signal: AbortSignal) =>
    coreApi.post('/uns/sources/usb-hid/devices', {}, { signal }) as Promise<{ devices: HIDInputDevice[] }>,
  preview: (endpoint: string, signal: AbortSignal) =>
    coreApi.post(
      '/uns/sources/usb-hid/preview',
      { endpoint, timeoutMs: 12000 },
      { signal, timeout: 16000 }
    ) as Promise<{
      received: boolean;
      text?: string;
    }>,
};
