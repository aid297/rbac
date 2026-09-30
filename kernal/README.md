# kernal

Go module `github.com/aid297/rbac/kernal`：**RBAC 授权内核**（策略引擎 + 持久化）与可选的对外服务（HTTP、gRPC、Admin UI）。单进程二进制 `cmd/rbac` 通过配置开关各 listener；核心业务在 [`rbac/`](rbac/README.md)，可被其它 Go 程序**直接 import**（无需走 SDK / 网络）。

它以**有向图**建模权限：每条边表示一个主体（subject）到目标（target）的授权关系，支持时间窗口、场景（scenario）等条件。

权限判定通过图的可达性完成：`Enforce(subject, target)` 判断在给定上下文下，subject 能否经由若干条**有效边**到达 target。

## 能做什么

- **成对授权建模**：把权限表达为有向边 `Src → Dst`，资源 ID 是不透明字符串（用户、角色、资源都可以是节点）。
- **传递可达**：授权可沿边链传递（受 `maxDepth = 32` 跳数上限约束），`Enforce(x, x)` 恒为 `true`。
- **条件化边**：每条边可携带条件——
  - `ALL`：无条件恒真；
  - `TIME`：半开区间 `[start, end)`，秒级精度（RFC3339）。
  - 同类条件之间 **OR**，不同类条件之间 **AND**；`ALL` 不可与其他条件混用。
- **场景维度**：边可绑定 `scenario`，查询时传入场景列表进行匹配。
- **启用/禁用**：每条边可单独 `enabled` 开关，无需删除。
- **多种访问方式**：HTTP、HTTPS（自签 CA）、gRPC、gRPC+TLS、管理预览页（Basic Auth）。
- **持久化与缓存**：策略文件为唯一事实来源；可选 Redis 副本加速读取，写入即时进 Redis、异步落盘。
- **静态加密**：策略文件可选 AES-256-GCM / AES-128-GCM / SM4 加密，支持密钥轮换（旧密钥迁移）。
- **写时复制内核**：策略图采用 COW 不可变快照 + 原子指针，读路径无锁，写路径串行化。

## 项目结构

| 路径 | 职责 |
| --- | --- |
| [`rbac/policy`](rbac/policy/) | 权限引擎：数据模型、条件求值、可达性、序列化、COW 快照 |
| [`rbac/persist`](rbac/persist/) | 策略存储：文件、加密、Redis 副本、对账与暂停 |
| [`rbac/cache`](rbac/cache/) | Redis 客户端（供 persist 选用） |
| [`rbac/crypto`](rbac/crypto/) | 策略文件加密算法（AES-GCM、SM4） |
| [`http-server`](http-server/) | 对外 HTTP / HTTPS `/v1` REST（package `httpserver`） |
| [`grpc-server`](grpc-server/) | 对外 gRPC / gRPC+TLS（package `grpcserver`） |
| [`adminui`](adminui/) | 管理预览页（独立端口；可与 HTTP API 分别开关） |
| [`api/`](api/) | 公开契约：OpenAPI、`proto`、Go 生成代码 |
| `config` | viper 配置、热加载 |
| `pki` | 自签 CA 与服务端 TLS 证书 |
| `svcctl` | HTTP/gRPC 共享的 in-flight 引流门闩 |
| `cmd/rbac` | **单一二进制**：按配置启动 admin / http-server / grpc-server |

## 快速开始

要求：Go 1.27+。

```bash
# 构建（注意：输出名不要用 rbac —— kernal/rbac 是库包目录，
# `go build -o rbac` 会把二进制写进该目录）
go build -o rbac-server ./cmd/rbac

# 用默认配置运行（读取 ./config.yaml，缺失则用内置默认值）
./rbac-server

# 指定配置文件
./rbac-server --config /path/to/config.yaml
# 或用环境变量（优先级高于 --config）
RBAC_CONFIG=/path/to/config.yaml ./rbac-server
```

默认所有服务（HTTP / HTTPS / gRPC / gRPC+TLS / admin）都是关闭的，进程仅完成存储初始化后等待信号。要对外提供服务，请在 `config.yaml` 中打开对应开关。例如开启 HTTP 与管理页：

```yaml
server:
  http:
    enable: true
    host: 0.0.0.0
    port: 8080
admin:
  enable: true
  username: admin
  password: admin
  host: 127.0.0.1
  port: 9090
```

启动后会打印一行运行摘要（配置来源、策略目录、加密状态、各监听地址、已加载边数等）。`SIGINT` / `SIGTERM` 会触发优雅退出并把缓存刷盘。

## 配置

配置通过 YAML 文件加载，路径优先级：`RBAC_CONFIG` > `--config` > `<工作目录>/config.yaml`。缺失的字段会在首次启动时补全并写回文件（文件权限 `0600`）。

主要字段：

| 字段 | 说明 | 默认 |
| --- | --- | --- |
| `policy.dir` | 策略文件目录，文件名固定为 `policy.rbac` | `stats` |
| `policy.encrypt` | 是否加密策略文件；留空则首次启动写 `false` | 空 |
| `policy.crypto` | 加密算法：`aes-256-gcm` / `aes-128-gcm` / `sm4` | `aes-256-gcm` |
| `policy.key` | 十六进制密钥；开启加密且留空时首次启动自动生成 | 空 |
| `ca.dir` | CA 目录，文件名固定 `ca.crt` / `ca.key`；缺失则启动时生成 | `secret` |
| `cache.enable` / `cache.kind` | 开启 Redis 副本（`kind: redis`） | `false` |
| `cache.addr` / `cache.db` | Redis 地址与库号 | `127.0.0.1:6379` / `0` |
| `server.http.*` | HTTP 监听开关、host、port；host 同时用作 HTTPS/gRPC-TLS 证书 SAN | 关闭 / `0.0.0.0` / `8080` |
| `server.https.*` | HTTPS 监听开关与端口（TLS 证书由内置 CA 签发） | 关闭 / `8443` |
| `server.grpc.*` | 明文 gRPC 监听开关与端口 | 关闭 / `9080` |
| `server.grpc_tls.*` | gRPC+TLS 监听开关与端口（复用 HTTPS 同款服务端证书） | 关闭 / `9443` |
| `admin.*` | 管理页开关、Basic Auth 用户名/密码、host、port | 关闭 / `admin` / `admin` / `127.0.0.1:9090` |

环境变量：

| 变量 | 作用 |
| --- | --- |
| `RBAC_CONFIG` | 覆盖配置文件路径 |
| `RBAC_POLICY_KEY` | 覆盖 `policy.key`（十六进制），不写入配置文件 |
| `RBAC_POLICY_KEY_PREV` | 旧密钥（十六进制），用于密钥轮换时迁移已加密的策略 blob |
| `RBAC_CACHE_PASSWORD` | 覆盖 `cache.password` |
| `RBAC_DEBUG=1` | 调试：即使配置开启加密也不对策略文件加密 |

> `admin.username` / `admin.password` 留空时回退为 `admin` / `admin`。修改配置文件中的 admin 凭据无需重启即可生效（配置热加载）。

## HTTP API

**机器可读契约**：[`api/openapi.yaml`](api/openapi.yaml)（OpenAPI 3）。集成说明与其它语言代码生成见 [`api/README.md`](api/README.md)。

所有接口以 JSON 交互，请求体上限 1 MiB。当存储处于暂停状态（如对账失败）时，除 `/healthz` 外的接口返回 `503`。

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/healthz` | 健康检查，暂停时返回 `503` 与原因 |
| `POST` | `/v1/enforce` | 判定 subject 是否可访问 target |
| `GET` | `/v1/reachable` | 列出 subject 在给定上下文下可达的全部节点 |
| `GET` | `/v1/bindings` | 列出全部边；带 `src`+`dst`(+`scenario`) 时查询单条 |
| `POST` | `/v1/bindings` | 新增边（重复返回 `409`） |
| `PUT` | `/v1/bindings` | 更新已存在的边（不存在返回 `404`，不会新增） |
| `PATCH` | `/v1/bindings/enabled` | 启用/禁用某条边 |
| `DELETE` | `/v1/bindings` | 删除边（`?src=&dst=&scenario=`，不存在返回 `404`） |

## gRPC API

**机器可读契约**：[`api/proto/rbac/v1/rbac.proto`](api/proto/rbac/v1/rbac.proto)（与 [`api/openapi.yaml`](api/openapi.yaml) 语义对齐）。生成代码在 `api/gen/rbac/v1/`。语义与上表 HTTP `/v1`（含 `/healthz`、`/v1/ca-cert`）一一对应：

| RPC | 对应 HTTP | 典型错误码 |
| --- | --- | --- |
| `Health` | `GET /healthz` | 暂停 → `FAILED_PRECONDITION`（SDK 映射为 503；与传输层 `UNAVAILABLE` 区分） |
| `GetCACert` | `GET /v1/ca-cert` | |
| `Enforce` | `POST /v1/enforce` | |
| `Reachable` | `GET /v1/reachable` | |
| `ListBindings` / `GetBinding` | `GET /v1/bindings` | 缺失 → `NOT_FOUND` |
| `AddBinding` | `POST /v1/bindings` | 重复 → `ALREADY_EXISTS` |
| `UpdateBinding` | `PUT /v1/bindings` | 缺失 → `NOT_FOUND` |
| `SetEnabled` | `PATCH /v1/bindings/enabled` | |
| `RemoveBinding` | `DELETE /v1/bindings` | 缺失 → `NOT_FOUND` |

开启示例：

```yaml
server:
  grpc:
    enable: true
    port: 9080
  grpc_tls:
    enable: true
    port: 9443
```

重新生成 stub（需本机 `protoc`、`protoc-gen-go`、`protoc-gen-go-grpc`）：

```bash
make proto
```

### 判定权限

```bash
curl -s -X POST http://localhost:8080/v1/enforce \
  -H 'Content-Type: application/json' \
  -d '{"subject":"alice","target":"doc:42","scenarios":["VIP"]}'
# {"allow":true}
```

请求体字段：`subject`、`target`（必填），`scenarios`（可选字符串数组），`now`（可选 RFC3339 时间，用于时间窗口判定；缺省为服务端当前时间）。

### 可达节点

```bash
curl -s 'http://localhost:8080/v1/reachable?subject=alice&scenario=VIP'
# {"subject":"alice","reachable":["alice","doc:42","role:editor"]}
```

> 结果**包含 subject 自身**；如需排除请在展示层过滤。

### 新增带时间窗口的边

```bash
curl -s -X POST http://localhost:8080/v1/bindings \
  -H 'Content-Type: application/json' \
  -d '{
    "src":"alice","dst":"role:editor","scenario":"",
    "enabled":true,
    "conditions":[{"kind":"TIME","start":"2026-06-01T00:00:00Z","end":"2026-07-01T00:00:00Z"}]
  }'
```

`conditions[].kind` 取 `ALL` 或 `TIME`；`TIME` 的 `start` / `end` 为 RFC3339，可只给其一表示无界。省略 `conditions` 等价于 `ALL`。`enabled` 缺省为 `true`。

错误码：参数/校验失败 `400`，未找到 `404`，重复 `409`，服务暂停 `503`。

## 管理预览页

开启 `admin.enable` 后，访问 `http://<admin.host>:<admin.port>/`，使用 Basic Auth 登录。页面提供只读的可视化预览：

- `GET /api/bindings`：全部边的 JSON；
- `GET /api/policy`：当前策略的规范文本；
- `POST /api/enforce`、`GET /api/reachable`：在页面上直接试算。

管理页仅用于预览与试算，写操作请走 `/v1/*` API。

## 策略文件格式

策略以类 Casbin 的行格式持久化（文件 `stats/policy.rbac`，开启加密时为密文 blob）。规范行：

```
# rbac-policy v1
b, <src>, <dst>, <scenario>, <enabled>, <conditions>
```

- `<enabled>`：`1` 或 `0`；
- `<conditions>`：`ALL`，或以 `;` 分隔的 `TIME:<start>~<end>` 列表（start/end 可空表示无界）。

示例：

```
# rbac-policy v1
b, alice, role:editor, , 1, ALL
b, role:editor, doc:42, , 1, TIME:2026-06-01T00:00:00Z~2026-07-01T00:00:00Z
```

加载（`Load`）宽容解析，导出（`Serialize`）严格规范化：按 `(src, dst, scenario)` 排序、恒真条件统一写成 `ALL`、字段以 `, ` 分隔，因此 `Load(Serialize(x)) == Serialize(x)` 幂等。资源 ID 不能为空、不能包含保留字符 `, ; ~ # \n \r` 或任何控制字符。

## 持久化、缓存与加密

- **文件为事实来源**：所有变更最终落到 `policy.rbac`。
- **Redis 副本（可选）**：写入即时进 Redis、异步落盘；崩溃后下次启动以文件为准覆盖 Redis；正常退出（SIGINT/SIGTERM）会刷盘。
- **静态加密（可选）**：开启 `policy.encrypt` 后策略文件以选定算法加密；密钥可由配置或 `RBAC_POLICY_KEY` 提供，缺失时自动生成并写回配置。
- **密钥轮换**：通过 `RBAC_POLICY_KEY_PREV` 提供旧密钥，启动对账时会用旧密钥解开并迁移到新密钥。对账无法安全完成时，服务会进入**暂停**状态（API 返回 `503`）以避免数据损坏。

## 开发

```bash
# 运行全部测试（含竞态检测）
go test -race ./...

# 查看 policy 内核覆盖率
go test ./rbac/policy/... -cover
```

`policy` 包采用标准库 `testing` 的表驱动测试，无第三方断言依赖。

## 生产部署与限制

在把 `cmd/rbac` 或公开 API 接到生产环境前，请默认按**单机授权内核 + 需外层补安全与 HA**来规划，而不是内置多租户网关。

| 主题 | 现状 | 建议 |
| --- | --- | --- |
| **公开 API 鉴权** | `/v1` 与 gRPC **`RbacService` 均无认证**（Admin 为 Basic Auth，且不在 openapi/proto 中） | 仅内网或经 **API 网关 / mTLS / 自研鉴权** 暴露；契约见 [`api/README.md`](api/README.md) |
| **部署拓扑** | **单进程**；`policy.rbac` 为**唯一事实来源**，Redis 仅为 sealed blob **副本**（可选），无内置选主或集群 | 高可用靠外部编排（单写者、共享存储、主备切换等）；避免多实例无协调共写同一文件 |
| **规模** | 查询全在**内存**（COW 快照，读无锁）；v1 设计假设**万级边**、写低频 | 超大图受单机内存限制；`Reachable` **无分页**，返回完整集合，调用方需过滤或限流 |
| **语义** | **`Enforce(x, x)` 恒为 `true`**（自环视为允许） | 若产品要禁止「访问自身」，在业务或网关层额外判断 |
| **可观测性** | 结构化日志（zap）；**无内置 metrics / 分布式追踪** | 生产可接 Prometheus、OpenTelemetry 等（需自行埋点或 sidecar） |
| **构建产物** | 模块内库路径为 **`kernal/rbac/`** | 构建二进制请用 **`-o rbac-server`**（或其它非 `rbac` 名），勿 `go build -o rbac` 与库目录冲突 |

**代码侧相对成熟的部分**：分层清晰（[`rbac/`](rbac/README.md) 内核 vs `http-server` / `grpc-server`）、写路径原子落盘、可选加密与 reconcile 失败时**暂停 API**、公开契约（openapi + proto）与官方 SDK 对齐。主要风险在**部署形态与威胁模型**，而非单测覆盖的每一层（例如 `cmd/rbac` 入口、`logging` 多无单测，属常见情况；传输层无单独 superpowers spec，以契约与 [`docs/INTEGRATION.md`](../docs/INTEGRATION.md) 为准）。

进程内集成（不走网络）见 [`rbac/README.md`](rbac/README.md)。Module 路径刻意写作 **`kernal`**（全仓库一致，非 `kernel` 笔误待改）。

## 许可证

[MIT](../LICENSE)
