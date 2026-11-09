import { LOGIN_URL } from '@/common-types/constans.ts';

const PUBLIC_ACCESS_PATHS = new Set(['/403', '/404', '/share', '/cli-auth', LOGIN_URL]);

export const isPublicAccessPath = (pathname = window.location.pathname) => PUBLIC_ACCESS_PATHS.has(pathname);
