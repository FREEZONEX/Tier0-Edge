import { createTheme } from '@uiw/codemirror-themes';
import { tags } from '@lezer/highlight';
import { ThemeType } from '@/stores/theme-store';

const createStreamTheme = (theme: 'light' | 'dark') =>
  createTheme({
    theme,
    settings: {
      background: 'var(--ui-modal-color)',
      foreground: 'var(--ui-text-color)',
      caret: 'var(--ui-text-color)',
      selection: 'var(--ui-menu-hover-color)',
      selectionMatch: 'var(--ui-fill-secondary)',
      lineHighlight: 'transparent',
      gutterBackground: 'var(--ui-modal-color)',
      gutterForeground: 'color-mix(in srgb, var(--ui-text-color) 75%, var(--ui-modal-color))',
      gutterBorder: 'var(--ui-codemirror-gutter-border, var(--ui-line-color))',
    },
    styles: [
      {
        tag: tags.comment,
        color: 'color-mix(in srgb, var(--ui-text-color) 75%, var(--ui-modal-color))',
      },
      { tag: tags.variableName, color: 'var(--ui-tag-info-color)' },
      {
        tag: [tags.string, tags.number, tags.bool, tags.null, tags.operator, tags.typeName, tags.propertyName],
        color: 'var(--ui-text-color)',
      },
      { tag: tags.keyword, color: 'var(--ui-text-color)', fontWeight: '600' },
    ],
  });

const lightTheme = createStreamTheme('light');
const darkTheme = createStreamTheme('dark');

export const getStreamCodemirrorTheme = (theme: string) => (theme === ThemeType.Dark ? darkTheme : lightTheme);
