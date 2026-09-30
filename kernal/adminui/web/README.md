# RBAC 管理预览（React）

Vite + React 前端，构建产物输出到 [`../static/`](../static/)，由 Go [`//go:embed`](../embed.go) 打进 `rbac-server` 二进制。

## 开发

1. 本地启动 admin 端口（例如 `kernal/demo/run-local.yaml` 中的 `19090`）。
2. 在本目录执行：

```bash
npm ci
npm run dev
```

Vite 会把 `/api` 代理到 `http://127.0.0.1:9090`；若端口不同，请改 [`vite.config.ts`](vite.config.ts) 中的 `server.proxy`。

浏览器需对 admin 端口完成 Basic 认证（例如 `http://admin:admin@127.0.0.1:19090/`），再在 dev server 打开页面时同样会带 cookie/认证——**推荐**直接访问已嵌入的 admin 端口做联调，或使用 dev proxy 前先登录 admin 一次。

更简单的方式：改 `vite.config.ts` 的 proxy `target` 为你的 admin 地址，并用浏览器扩展或 curl 带 Basic Auth；日常以 **build + go run** 为主。

## 发布到二进制

```bash
cd kernal/adminui/web
npm ci
npm run build
cd ../../..
go build -o rbac ./kernal/cmd/rbac
```

或在 `kernal/` 目录：`make admin-ui`。

修改 UI 后必须重新 `npm run build`，再 `go build`，否则 embed 仍是旧静态文件。
