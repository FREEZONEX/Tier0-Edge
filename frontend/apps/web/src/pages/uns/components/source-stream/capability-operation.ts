import type { CapabilityOperation } from '@/apis/core-api/capabilities';

export function operationReady(operation: CapabilityOperation | undefined, now = Date.now()): boolean {
  return (
    !!operation &&
    operation.allowed &&
    operation.releaseEnabled &&
    operation.enabled &&
    operation.state === 'ready' &&
    (!operation.validUntil || Date.parse(operation.validUntil) > now)
  );
}
const reasonKeys: Record<string, string> = {
  READY: 'ready',
  RELEASE_DISABLED: 'releaseDisabled',
  CAPABILITY_DISABLED: 'disabled',
  ROUTE_NOT_REGISTERED: 'routeMissing',
  MAINTENANCE: 'maintenance',
  MIGRATION_REQUIRED: 'migration',
  SCHEMA_NOT_READY: 'schema',
  RUNTIME_UNREACHABLE: 'runtime',
  WORKER_NOT_REGISTERED: 'worker',
  READINESS_EXPIRED: 'expired',
  TRANSPORT_UNAVAILABLE: 'transport',
  CREDENTIAL_SERVICE_UNAVAILABLE: 'credentials',
  PROTOCOL_RUNTIME_UNAVAILABLE: 'protocol',
  READINESS_NOT_SUPPORTED: 'unsupported',
  DEPENDENCIES_NOT_READY: 'dependencies',
};
export function readinessReasonKey(reason: string): string {
  return 'SourceStream.readiness.' + (reasonKeys[reason] ?? 'dependencies');
}
