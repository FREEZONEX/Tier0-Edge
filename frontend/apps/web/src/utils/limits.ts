export const MAX_LENGTHS = {
  name: 63,
  description: 512,
  alias: 128,
  displayName: 128,
  apiKeyName: 100,
  connectionName: 128,
  // Notification contract: backend/internal/domain/notify/constants.go.
  notificationTitle: 50,
  notificationContent: 800,
  notificationLinkBytes: 500,
} as const;
