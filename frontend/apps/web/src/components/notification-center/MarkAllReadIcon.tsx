import type { SVGProps } from 'react';

/** Mark-all-read glyph from Tier0 notification Figma. */
const MarkAllReadIcon = (props: SVGProps<SVGSVGElement>) => (
  <svg width="16" height="16" viewBox="0 0 16 16" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden {...props}>
    <path
      d="M5.33333 2H1.33333V12H6C7.13333 12 8 12.8667 8 14V4.66667C8 3.2 6.8 2 5.33333 2Z"
      stroke="currentColor"
      strokeWidth="1.33333"
      strokeLinecap="round"
      strokeLinejoin="round"
    />
    <path
      d="M10.6667 7.99984L12 9.33317L14.6667 6.6665"
      stroke="currentColor"
      strokeWidth="1.33333"
      strokeLinecap="round"
      strokeLinejoin="round"
    />
    <path
      d="M14.6667 4V2H10.6667C9.2 2 8 3.2 8 4.66667V14C8 12.8667 8.86667 12 10 12H14.6667V10.4667"
      stroke="currentColor"
      strokeWidth="1.33333"
      strokeLinecap="round"
      strokeLinejoin="round"
    />
  </svg>
);

export default MarkAllReadIcon;
