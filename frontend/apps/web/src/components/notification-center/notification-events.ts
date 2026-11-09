export const NOTIFICATIONS_CHANGED_EVENT = 'tier0:notifications-changed';

export const notifyNotificationChanged = () => {
  window.dispatchEvent(new CustomEvent(NOTIFICATIONS_CHANGED_EVENT));
};
