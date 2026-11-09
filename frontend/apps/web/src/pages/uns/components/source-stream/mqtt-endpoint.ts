type MqttScheme = 'mqtt' | 'mqtts';

export function readMqttEndpoint(endpoint: string) {
  const match = /^(mqtts?):\/\/(\[[^\]]*\]|[^:/?#@]*)(?::(\d*))?(\/?)$/i.exec(endpoint);
  if (!match) return;
  const scheme = match[1].toLowerCase() as MqttScheme;
  return {
    scheme,
    host: match[2].replace(/^\[|\]$/g, ''),
    port: match[3] === undefined ? (scheme === 'mqtts' ? 8883 : 1883) : Number(match[3]),
    explicitPort: match[3],
    suffix: match[4],
  };
}

export function updateMqttEndpoint(endpoint: string, patch: { host?: string; port?: number; scheme?: MqttScheme }) {
  const current = readMqttEndpoint(endpoint);
  if (!current) return endpoint;
  const host = (patch.host ?? current.host).trim().replace(/^\[|\]$/g, '');
  const authority = host.includes(':') ? `[${host}]` : host;
  const port = patch.port === undefined ? current.explicitPort : String(patch.port);
  return `${patch.scheme ?? current.scheme}://${authority}${port === undefined ? '' : `:${port}`}${current.suffix}`;
}
