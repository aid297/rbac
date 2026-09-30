# 集成与贡献指南

面向**人类开发者**与 **AI 助手**：在本 monorepo 里对接 RBAC、生成第三方客户端，或修改公开 API 时应读本文。机器可读契约见 [`server/api/`](../server/api/README.md)。

## 1. 两种对外面

| 范围 | 路径前缀 | 鉴权 | 是否在 OpenAPI / proto 中 |
| --- | --- | --- | --- |
| **公开集成 API** | `/healthz`、`/v1/*`；gRPC `rbac.v1.RbacService` | 无（需网络隔离或网关） | **是** — 契约的单一事实来源 |
| **管理预览** | `/`、`/api/*`（HTML + JSON） | HTTP Basic Auth | **否** — 仅 [`server/adminui`](../server/adminui/)，不保证稳定 |

第三方集成、官方 SDK、OpenAPI Generator / protoc 生成代码 **只应依赖公开面**。改 admin 行为不必同步 openapi/proto，但也不应把 admin 端点写进公开契约。

## 2. 契约与实现的对应关系

```
server/api/openapi.yaml          ← HTTP JSON 形状（第三方 REST）
server/api/proto/rbac/v1/rbac.proto   ← gRPC 形状（权威 proto）
        │
        ├─► server/httpsvc/       HTTP 实现
        ├─► server/grpcsvc/       gRPC 实现（与 HTTP 语义对齐）
        └─► server/api/gen/       Go 服务端 stub（make proto）

sdk-go/     … 引用 ../server/api/proto，internal/rbacv1 生成代码
sdk-ts/     … vendored proto/rbac/v1/rbac.proto + @grpc/proto-loader
sdk-rust/   … vendored proto + src/pb/rbac.v1.rs
sdk-csharp/ … vendored proto + src/Rbac.Sdk/Generated/
```

**Proto 权威路径**：[`server/api/proto/rbac/v1/rbac.proto`](../server/api/proto/rbac/v1/rbac.proto)。  
修改 RPC 或 message 时先改此文件，再同步 vendored 副本并重新生成各语言 stub。

**OpenAPI 权威路径**：[`server/api/openapi.yaml`](../server/api/openapi.yaml)。  
HTTP 行为以 [`server/httpsvc/`](../server/httpsvc/) 为准；openapi 必须与 handler 一致（含 503 暂停、GET `/v1/bindings` 列表 vs 单条等）。

## 3. 修改公开 API 时的检查表

按变更类型勾选（Wire 变更 = 请求/响应/路径/状态码/RPC 签名变化）。

### 3.1 仅 HTTP 行为或 JSON（无 proto 变更）

- [ ] 实现：[`server/httpsvc/`](../server/httpsvc/)（及共享的 policy 错误映射）
- [ ] 契约：[`server/api/openapi.yaml`](../server/api/openapi.yaml) — bump `info.version`（见 §4）
- [ ] 文档：[`server/README.md`](../server/README.md) 表格 / 示例（若用户可见行为变了）
- [ ] 官方 SDK（若暴露同一语义）：`sdk-go`、`sdk-ts`、`sdk-rust`、`sdk-csharp` 的 HTTP 路径与测试
- [ ] gRPC：若 HTTP 与 gRPC 仍应对齐，确认 [`server/grpcsvc/`](../server/grpcsvc/) 未漂移（即使 proto 未改）

### 3.2 gRPC / 共用 message（proto 变更）

- [ ] [`server/api/proto/rbac/v1/rbac.proto`](../server/api/proto/rbac/v1/rbac.proto)（注释里更新 HTTP 对照）
- [ ] `cd server && make proto` → 提交 `server/api/gen/`
- [ ] 同步 vendored proto（在 monorepo 根目录）：
  ```bash
  cp server/api/proto/rbac/v1/rbac.proto sdk-ts/proto/rbac/v1/rbac.proto
  cp server/api/proto/rbac/v1/rbac.proto sdk-rust/proto/rbac/v1/rbac.proto
  cp server/api/proto/rbac/v1/rbac.proto sdk-csharp/proto/rbac/v1/rbac.proto
  ```
- [ ] 各 SDK 重新生成 stub：`cd sdk-go && make proto`；`sdk-rust` / `sdk-csharp` 各自 `make proto`（TS 无生成步骤，改 proto 即可）
- [ ] 实现：[`server/grpcsvc/`](../server/grpcsvc/) + 必要时 [`server/httpsvc/`](../server/httpsvc/)
- [ ] [`server/api/openapi.yaml`](../server/api/openapi.yaml)（若 JSON _wire_ 与 message 应对齐）
- [ ] 四个官方 SDK：客户端方法、错误映射、测试、README；按需 bump 包版本并发布

### 3.3 仅 SDK  ergonomics（不改服务端 wire）

- [ ] 只改对应 `sdk-*`，测试与 README；**不要**改 openapi/proto，除非文档示例纠正

### 3.4 策略引擎 / 持久化（`policy` / `persist`）

- [ ] 设计参考：[`docs/superpowers/specs/2026-09-18-rbac-policy-engine-design.md`](superpowers/specs/2026-09-18-rbac-policy-engine-design.md)（子系统范围以代码为准）
- [ ] 若只影响 Enforce 结果而不改 API 形状：更新 server 测试；SDK 通常无需改
- [ ] 若新增错误类型：对齐 HTTP 状态码、gRPC code、SDK 的 `Is*` 助手

## 4. 契约版本怎么 bump

|  artifact | 何时 bump | 怎么做 |
| --- | --- | --- |
| **OpenAPI** `info.version` | 任意公开 HTTP 形状或文档化语义变更 | 编辑 [`openapi.yaml`](../server/api/openapi.yaml)；release note 说明 |
| **Proto** | 新增 field/RPC（兼容）或 breaking 变更 | 遵循 protobuf 兼容规则；breaking 应极少，并写 release note |
| **Git tag** | 对外 pin 契约 | 集成方使用 `raw.githubusercontent.com/.../<tag>/server/api/...`；见 [`server/api/README.md`](../server/api/README.md) |
| **官方 SDK 包版本** | 随 SDK 发布节奏 | 各包独立 semver（如 `rbac-sdk-ts 0.1.x`）；与服务端版本无强制绑定 |

OpenAPI `info.version` **不等于** npm/crates/NuGet 版本号；它只标记 **HTTP 契约文档** 修订。

## 5. 跨语言语义（生成客户端必须对齐）

以下在 OpenAPI/proto 里只有部分体现，**以官方 SDK 与服务端为准**：

| 主题 | 约定 |
| --- | --- |
| **暂停** | 除 `GET /healthz` 外 HTTP **503**；gRPC 业务暂停 **`FAILED_PRECONDITION`**（不是 `UNAVAILABLE`） |
| **Health** | 暂停时 HTTP 503 body `{"status":"paused","reason"}`；正常 200 `{"status":"ok"}` |
| **超时** | 官方 SDK：`timeout <= 0` / zero duration → **无 deadline**（无限等待） |
| **时间** | `now` / protobuf `Timestamp` 在服务端按 **秒** 精度参与条件求值 |
| **JSON** | 请求体最大 **1 MiB**；未知字段 **400** |
| **传输错误** | 连不上、超时等 **不** 等同于「服务暂停」 |

生成 Python/Java 等客户端时，在 [`server/api/README.md`](../server/api/README.md) 基础上，对照 [`sdk-go/README.md`](../sdk-go/README.md) 或 [`sdk-ts/README.md`](../sdk-ts/README.md) 的错误与超时章节。

## 6. AI / 新贡献者：按任务选阅读顺序

**任务 A — 在业务里调用 RBAC（用官方 SDK）**

1. 根 [`README.md`](../README.md) → 对应 `sdk-*/README.md`
2. 本地起服务：[`server/README.md`](../server/README.md) 快速开始

**任务 B — 生成或手写第三方客户端（无官方 SDK 的语言）**

1. [`server/api/openapi.yaml`](../server/api/openapi.yaml) + [`server/api/proto/rbac/v1/rbac.proto`](../server/api/proto/rbac/v1/rbac.proto)
2. [`server/api/README.md`](../server/api/README.md)
3. 本文 §5 语义表 + 任选一份官方 SDK README 的错误处理

**任务 C — 改服务端或新增公开端点**

1. 本文 §2–§4
2. [`server/README.md`](../server/README.md) 项目结构
3. 实现 `httpsvc` + `grpcsvc` + 契约 + SDK 检查表（§3）

**任务 D — 改 policy / 存储 / 配置**

1. [`server/README.md`](../server/README.md) + `server/policy`、`server/persist` 源码
2. [`docs/superpowers/specs/`](superpowers/specs/)（注意日期较早的 spec 可能未反映 gRPC / monorepo 现状，**以代码为准**）

## 7. 验证

在 monorepo 根目录或各子目录：

```bash
cd server && go test -race ./...
cd sdk-go && go test -race ./...
cd sdk-rust && cargo test
cd sdk-csharp && dotnet test
cd sdk-ts && npm test
```

变更 openapi 后（可选）：

```bash
npx --yes @redocly/cli lint server/api/openapi.yaml
```

## 8. 刻意不在范围内的文档

- [`docs/superpowers/specs/`](superpowers/specs/)：立项设计快照；各文件文首有 **「文档勘误」**，正文过时处以勘误与本文为准。
- Admin UI、Docker 细节、发布到 npm/NuGet/crates 的步骤：见各子目录 README 与发布脚本，不在本文展开。

---

维护 openapi/proto 或公开 API 时，优先更新 **§3 检查表** 涉及项，再在 PR / release note 中写明契约版本与 SDK 版本，便于集成方 pin raw URL 或包版本。
