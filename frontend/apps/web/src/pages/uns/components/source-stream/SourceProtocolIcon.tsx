import { Api, Cloud, Document, DataBase, Plug } from '@carbon/icons-react';
import styles from './registry.module.scss';

// Cloud SourceProtocolGlyph 的同源素材与 viewBox；Edge 协议名在入口适配。
const logos: Record<string, { src: string; width: number; height: number; viewBox: string }> = {
  mqtt: { src: 'mqtt.svg', width: 320, height: 320, viewBox: '0 0 320 320' },
  opcua: { src: 'opcua.png', width: 600, height: 600, viewBox: '25 245 550 110' },
  'modbus-tcp': { src: 'modbus.png', width: 500, height: 200, viewBox: '50 25 400 150' },
};
export default function SourceProtocolIcon({ protocol }: { protocol: string }) {
  const logo = logos[protocol];
  const Icon =
    protocol === 'edge-sql'
      ? DataBase
      : protocol === 'file-read'
        ? Document
        : protocol === 'oauth2'
          ? Cloud
          : protocol === 'http-api'
            ? Api
            : Plug;
  return (
    <span className={styles.icon} aria-hidden="true">
      {logo ? (
        <svg viewBox={logo.viewBox} width={32} height={24} focusable="false">
          <image href={`/images/protocols/${logo.src}`} width={logo.width} height={logo.height} />
        </svg>
      ) : (
        <Icon size={24} />
      )}
    </span>
  );
}
