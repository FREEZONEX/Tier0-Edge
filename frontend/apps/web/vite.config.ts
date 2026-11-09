import { defineConfig, transformWithEsbuild } from 'vite';
import react from '@vitejs/plugin-react';
import legacy from '@vitejs/plugin-legacy';
// import { federation } from '@module-federation/vite';
import fs from 'fs';
import path from 'path';
import packageJson from './package.json';
import { getDevInfo, getProxy, logBuildTime, logDevInfo } from './config/dev.config';
// import { mfConfig } from './config/mfConfig.ts';

const devInfo = getDevInfo();
const proxy = getProxy(
  devInfo.API_PROXY_URL,
  devInfo.SINGLE_API_PROXY_LIST,
  devInfo.SINGLE_API_PROXY_URL,
  devInfo.VITE_PROXY_SECURE
);
delete proxy['/files/system/resource'];
logDevInfo(devInfo);
const buildTime = logBuildTime();
// 开发和生产预览共用显式主机白名单，保留 Vite 默认的 Host 校验。
const allowedHosts = [
  'edge-dev.local',
  'host.docker.internal',
  '127.0.0.1',
  'localhost',
  ...(devInfo.VITE_ALLOWED_HOSTS?.split(',')
    .map((host) => host.trim())
    .filter(Boolean) || []),
];
const localResourceRoot = path.resolve(__dirname, '../../../deploy/mount/edge/system/resource');

const localResourcePlugin = () => {
  const serveLocalResource = (req: any, res: any, next: any) => {
    const url = req.url || '';
    if (url.split('?')[0] === '/runtime-env.js') {
      if (devInfo.API_PROXY_URL) {
        next();
        return;
      }
      res.setHeader('Content-Type', 'application/javascript; charset=utf-8');
      res.setHeader('Cache-Control', 'no-store');
      res.end(
        `window.__TIER0_RUNTIME_CONFIG__ = ${JSON.stringify({
          launchpadPort: devInfo.VITE_LAUNCHPAD_PORT || devInfo.LAUNCHPAD_PORT || '',
          launchpadStandalone: false,
        })};\n`
      );
      return;
    }

    const prefix = '/files/system/resource/';
    if (!url.startsWith(prefix)) {
      next();
      return;
    }

    const relativePath = decodeURIComponent(url.slice(prefix.length).split('?')[0]);
    const filePath = path.resolve(localResourceRoot, relativePath);
    if (!filePath.startsWith(localResourceRoot)) {
      next();
      return;
    }
    if (!fs.existsSync(filePath) || fs.statSync(filePath).isDirectory()) {
      next();
      return;
    }

    const ext = path.extname(filePath).toLowerCase();
    const contentTypeMap: Record<string, string> = {
      '.svg': 'image/svg+xml',
      '.png': 'image/png',
      '.jpg': 'image/jpeg',
      '.jpeg': 'image/jpeg',
      '.gif': 'image/gif',
      '.webp': 'image/webp',
      '.json': 'application/json; charset=utf-8',
      '.ico': 'image/x-icon',
    };
    res.setHeader('Content-Type', contentTypeMap[ext] || 'application/octet-stream');
    fs.createReadStream(filePath).pipe(res);
  };

  return {
    name: 'local-resource-plugin',
    configureServer(server: any) {
      server.middlewares.use(serveLocalResource);
    },
    configurePreviewServer(server: any) {
      server.middlewares.use(serveLocalResource);
    },
  };
};

const jsAsJsxPlugin = () => ({
  name: 'js-as-jsx',
  enforce: 'pre' as const,
  async transform(code: string, id: string) {
    if (!id.endsWith('.js') || id.includes('node_modules')) {
      return null;
    }
    return transformWithEsbuild(code, id, {
      loader: 'jsx',
      jsx: 'automatic',
      format: 'esm',
      sourcemap: true,
    });
  },
});

// https://vite.dev/config/
export default defineConfig({
  base: devInfo.VITE_ASSET_PREFIX || '/',
  esbuild: {
    drop: ['debugger'],
    pure: ['console.log'],
    supported: {
      'top-level-await': true,
    },
  },
  plugins: [
    jsAsJsxPlugin(),
    react({
      babel: () => ({
        plugins: [],
      }),
    }),
    localResourcePlugin(),
    legacy({
      targets: ['chrome>=89', 'safari>=15', 'firefox>=89', 'edge>=89'],
      modernPolyfills: true,
    }),
    // federation(mfConfig),
  ],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, 'src'),
    },
    // 导入文件时省略的扩展名（优先使用源码 .tsx/.ts，避免加载 tsc 编译出的 CJS .js 产物）
    extensions: ['.mjs', '.tsx', '.ts', '.jsx', '.js'],
  },
  define: {
    'process.env': { ...devInfo },
    'process.env.LAUNCHPAD_PORT': JSON.stringify(devInfo.LAUNCHPAD_PORT || ''),
    'import.meta.env.VITE_APP_VERSION': JSON.stringify(packageJson.version),
    'import.meta.env.VITE_APP_BUILD_TIMESTAMP': JSON.stringify(buildTime),
    'import.meta.env.VITE_LAUNCHPAD_PORT': JSON.stringify(devInfo.VITE_LAUNCHPAD_PORT || ''),
  },
  envPrefix: ['REACT_APP_', 'VITE_', 'OPENAI_'],
  server: {
    host: devInfo.VITE_ENABLE_HOST,
    allowedHosts,
    origin: devInfo.VITE_ASSET_PREFIX,
    fs: {
      allow: [
        path.resolve(__dirname, '..'),
        path.resolve(__dirname, '../../../frontend'),
        path.resolve(__dirname, '../../../../Tier0/enterprise/frontend'),
      ],
    },
    proxy: {
      ...proxy,
      ...(devInfo.VITE_ASSET_PREFIX !== '1' && devInfo.API_PROXY_URL
        ? {
            '/plugin/': {
              target: devInfo.API_PROXY_URL,
              changeOrigin: true,
              secure: devInfo.API_PROXY_URL?.startsWith('https://') ? (devInfo.VITE_PROXY_SECURE ?? false) : undefined,
            },
          }
        : {
            '/mf-manifest.json': devInfo.VITE_ASSET_PREFIX,
          }),
    },
  },
  optimizeDeps: {
    esbuildOptions: {
      loader: {
        '.js': 'jsx',
      },
    },
  },
  // 预览默认继承 server.proxy，保留 API、iframe 和插件资源的完整代理。
  preview: {
    host: devInfo.VITE_ENABLE_HOST || undefined,
    allowedHosts,
  },
  build: {
    rollupOptions: {
      output: {
        manualChunks: {
          vendor: ['react', 'react-dom', 'react-router'],
          antd: ['antd', '@ant-design/icons'],
          charts: ['reactflow'],
          utils: ['ahooks', 'lodash-es', 'dayjs'],
        },
      },
    },
    target: ['chrome89', 'edge89', 'firefox89', 'safari15'],
  },
});
