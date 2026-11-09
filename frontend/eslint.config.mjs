import js from '@eslint/js';
import globals from 'globals';
import tsEslint from 'typescript-eslint';
import eslintPluginPrettierRecommended from 'eslint-plugin-prettier/recommended';
import reactHooks from 'eslint-plugin-react-hooks';
import reactRefresh from 'eslint-plugin-react-refresh';

export default tsEslint.config(
  { ignores: ['**/**/dist', 'build', '**/node_modules/*'] },
  js.configs.recommended,
  ...tsEslint.configs.recommended,
  // 此插件附带了一个 eslint-plugin-prettier/recommended 配置，可一次性设置 eslint-plugin-prettier 和 eslint-config-prettier
  eslintPluginPrettierRecommended,
  {
    files: ['**/*.{ts,tsx,js,jsx,mjs}'],
    languageOptions: {
      globals: {
        ...globals.browser,
        ...globals.node,
      },
    },
    plugins: {
      'react-hooks': reactHooks,
      'react-refresh': reactRefresh,
    },
    rules: {
      ...reactHooks.configs.recommended.rules,
      // 'no-unused-vars': 'off',
      // '@typescript-eslint/no-unused-vars': 'off',
      '@typescript-eslint/no-explicit-any': 'off',
      // '@typescript-eslint/no-require-imports': 'off',
      // '@typescript-eslint/no-unused-expressions': 'warn',
      'react-hooks/set-state-in-effect': 'warn', // 从 "error" 改为 "warn"
      'react-hooks/preserve-manual-memoization': 'warn', // 从 "error" 改为 "warn"
      'react-hooks/use-memo': 'warn', // 从 "error" 改为 "warn"
      'react-hooks/immutability': 'warn', // 从 "error" 改为 "warn"
      'react-hooks/refs': 'warn', // 从 "error" 改为 "warn"
    },
  },
  {
    files: ['**/*.cjs'],
    languageOptions: {
      globals: {
        ...globals.node,
      },
    },
    rules: {
      '@typescript-eslint/no-require-imports': 'off',
      '@typescript-eslint/no-unused-vars': ['error', { argsIgnorePattern: '^_', varsIgnorePattern: '^_' }],
    },
  }
);
