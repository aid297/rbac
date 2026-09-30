# 本地 Demo 与集成测试

仓库中的 **`demo/`**（monorepo 根）与 **`kernal/demo/`** 仅用于**本机**联调、Admin 预览、SDK 手工测试，**不纳入 Git 追踪**（见根目录 [`.gitignore`](../.gitignore)）。克隆仓库后需自行创建；已有目录不会被提交。

## 目录约定

| 路径 | 用途 |
| --- | --- |
| `demo/` | 根级 demo：可选 `config.yaml`、空策略目录、`demo-go/` 等对 live server 的 SDK 回归 |
| `kernal/demo/` | 在 `kernal/` 下启动 `rbac-server` 时的本地配置（如 `run-local.yaml`）、日志、样例 `data/` |

运行时数据仍落在 **`kernal/stats/`、`kernal/secret/`、`kernal/logs/`**（已由 `kernal/.gitignore` 忽略），与 demo 配置里的 `policy.dir` / `ca.dir` / `log.file` 相对路径有关。

## 启动 kernal（HTTP API + Admin UI）

1. 在 `kernal/demo/` 新建 `run-local.yaml`（端口按本机空闲情况修改；8080/9090 被占用时可改用 18080/19090）：

```yaml
log:
  level: info
  file: logs/rbac.log
  debug: true
policy:
  dir: stats
  encrypt: false
ca:
  dir: secret
cache:
  enable: false
server:
  http:
    enable: true
    host: 127.0.0.1
    port: 18080
  https:
    enable: false
  grpc:
    enable: false
  grpc_tls:
    enable: false
admin:
  enable: true
  username: admin
  password: admin
  host: 127.0.0.1
  port: 19090
```

2. 构建并运行（**勿**使用 `-o rbac`，避免与 `kernal/rbac/` 包目录冲突）：

```bash
cd kernal
go build -o rbac-server ./cmd/rbac
./rbac-server --config demo/run-local.yaml
```

3. 验证：

- API：`curl -s http://127.0.0.1:18080/healthz`
- Admin：浏览器打开 `http://127.0.0.1:19090/`，Basic Auth **admin / admin**（或 `http://admin:admin@127.0.0.1:19090/`）

公开 `/v1` 契约见 [`kernal/api/`](../kernal/api/README.md)；Admin `/api/*` 不在 OpenAPI 中。

## SDK 功能测试规格

多 SDK 对 live server 的用例定义见 **[`local-demo-spec.md`](local-demo-spec.md)**（原 `demo/demo-spec.md`）。典型流程：

1. 用 `demo/config.yaml` 或等价配置在 **127.0.0.1:8080** 起仅 HTTP 的服务（无加密、无缓存、空策略）。
2. 在本地 `demo/demo-go/` 运行 `go run . -addr http://127.0.0.1:8080`（该目录为本地资源，需自行从备份或历史提交恢复，或按 spec 自写脚本）。

## 与 CI / 正式测试的关系

- 各 `sdk-*` 与 `kernal` 包内 **`go test`** 不依赖 `demo/` 目录。
- `demo/` 仅供开发者本机；**不要**把密钥、真实策略或 `logs/` 提交进仓库。
