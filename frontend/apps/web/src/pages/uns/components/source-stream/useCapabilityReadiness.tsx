import { useEffect, useState } from 'react';
import { Alert, Space } from 'antd';
import { discoverCapabilities, type PublicCapability } from '@/apis/core-api/capabilities';
import { useSourceStreamScope } from '@/apis/core-api/source-stream-scope';
import { useTranslate } from '@/hooks';
import { operationReady, readinessReasonKey } from './capability-operation';

// UI explicitly refreshes platform evidence while visible. It never extends proof
// expiry or maps aggregate readiness to an individual resource execution state.
export function useCapabilityReadiness(code: 'source' | 'stream') {
  const { nodeId } = useSourceStreamScope();
  const [capability, setCapability] = useState<PublicCapability>();
  const [failed, setFailed] = useState(false);
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (nodeId) return;
    let stopped = false;
    let timer: ReturnType<typeof setTimeout>;
    const refresh = async () => {
      if (!stopped) setNow(Date.now());
      if (document.hidden) {
        timer = setTimeout(refresh, 1000);
        return;
      }
      try {
        const result = await discoverCapabilities();
        if (!stopped) {
          setCapability(result.capabilities.find((item) => item.code === code));
          setFailed(false);
        }
      } catch {
        if (!stopped) {
          setCapability(undefined);
          setFailed(true);
        }
      } finally {
        if (!stopped) timer = setTimeout(refresh, 1000);
      }
    };
    void refresh();
    return () => {
      stopped = true;
      clearTimeout(timer);
    };
  }, [code, nodeId]);
  const blocked = (name: string) => {
    if (nodeId) return false; // Fleet keeps its own resource-bound status/auth path.
    if (name === 'disable' && !capability) return false; // Safe stop still rechecks real auth and revision guards.
    return !operationReady(capability?.operations?.find((item) => item.operation === name));
  };
  return { capability, failed, blocked, remote: !!nodeId, now };
}
const operationLabels: Record<string, string> = {
  read: 'common.detail',
  configure: 'SourceStream.connection',
  test: 'SourceStream.testConnection',
  discover: 'SourceStream.discover',
  enable: 'SourceStream.enableSource',
  disable: 'SourceStream.disableSource',
  validate: 'SourceStream.validate',
  deploy: 'SourceStream.publish',
  execute: 'SourceStream.invoke',
};
export function CapabilityReadinessNotice({ value }: { value: ReturnType<typeof useCapabilityReadiness> }) {
  const t = useTranslate();
  if (value.remote) return null;
  const expired = !!value.capability?.validUntil && Date.parse(value.capability.validUntil) <= value.now;
  const reason = value.failed
    ? 'DEPENDENCIES_NOT_READY'
    : expired
      ? 'READINESS_EXPIRED'
      : value.capability?.state !== 'ready'
        ? value.capability?.reasonCode
        : undefined;
  const unavailable = value.capability?.operations?.filter((operation) => !operationReady(operation, value.now)) ?? [];
  if (!reason && unavailable.length === 0) return null;
  return (
    <Alert
      type="warning"
      showIcon
      message={t('SourceStream.platformReadiness')}
      description={
        <Space direction="vertical" size={0}>
          {reason && <span>{t(readinessReasonKey(reason))}</span>}
          {unavailable.map((operation) => (
            <span key={operation.operation}>
              {t(
                operation.operation === 'configure' && value.capability?.code === 'stream'
                  ? 'SourceStream.editProgram'
                  : operation.operation === 'disable' && value.capability?.code === 'stream'
                    ? 'SourceStream.disableStream'
                    : operationLabels[operation.operation]
              )}
              : {t(readinessReasonKey(operation.reasonCode ?? 'DEPENDENCIES_NOT_READY'))}
            </span>
          ))}
        </Space>
      }
    />
  );
}
