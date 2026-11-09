// 静态承载 web 主应用 + /anchor/ 子应用：
// serve CLI 的 rewrites 是链式应用且不支持排除模式，无法表达
// "/anchor/** 回退子应用 index、其余回退主应用 index"，改用 serve-handler 按前缀分流。
import http from 'node:http';
import handler from 'serve-handler';

const root = process.env.WEB_DIST || '/app/web-dist';
const port = Number(process.env.PORT || 3000);

const anchorOptions = {
  public: root,
  rewrites: [{ source: '/anchor/**', destination: '/anchor/index.html' }],
};
const webOptions = {
  public: root,
  rewrites: [{ source: '**', destination: '/index.html' }],
};

http
  .createServer((req, res) => {
    const path = (req.url || '/').split('?')[0];
    const isAnchor = path === '/anchor' || path.startsWith('/anchor/');
    return handler(req, res, isAnchor ? anchorOptions : webOptions);
  })
  .listen(port, () => {
    process.stdout.write(`static server listening on :${port}, root=${root}\n`);
  });
