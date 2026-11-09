import type { Dayjs } from 'dayjs';
import { MAX_LENGTHS } from '@/utils/limits';
import { isNotificationExternalLink, isNotificationTargetPath } from '@/apis/core-api/notifications';
import type { RecipientSubjectValue } from './components/RecipientSelector';

export type SendFormValues = {
  audienceType: 1 | 2;
  recipientSubjects?: RecipientSubjectValue[];
  sendMode: 'now' | 'scheduled';
  scheduledAt?: Dayjs;
};

export type NotificationFormValues = SendFormValues & {
  type: string;
  title: string;
  content: string;
  link?: string;
  asModal?: boolean;
};

export const SEND_INITIAL_VALUES: SendFormValues = { audienceType: 1, sendMode: 'now' };
export const NOTIFICATION_INITIAL_VALUES = { ...SEND_INITIAL_VALUES, type: 'inbox', asModal: false };
export const characterCount = (value: string) => Array.from(value).length;

// 与后端 validateLink 保持一致：支持内网 HTTP，拒绝危险协议、空白、反斜杠及不可见字符。
export const isValidNotificationLink = (link: string) =>
  !link ||
  (new TextEncoder().encode(link).length <= MAX_LENGTHS.notificationLinkBytes &&
    (isNotificationExternalLink(link) || isNotificationTargetPath(link)));

export const scheduleTimestamp = (values: SendFormValues) =>
  values.sendMode === 'scheduled' ? values.scheduledAt?.valueOf() : 0;
