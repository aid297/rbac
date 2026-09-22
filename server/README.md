# rbac

一个用 Go 编写的轻量级 RBAC（基于关系的访问控制）授权微服务。它以**有向图**建模权限：每条边表示一个主体（subject）到目标（target）的授权关系，支持时间窗口、场景（scenario）等条件，并提供 HTTP / HTTPS API 与一个带鉴权的管理预览页。

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
- **多种访问方式**：HTTP、HTTPS（自签 CA 自动生成）、管理预览页（Basic Auth）。
- **持久化与缓存**：策略文件为唯一事实来源；可选 Redis 副本加速读取，写入即时进 Redis、异步落盘。
- **静态加密**：策略文件可选 AES-256-GCM / AES-128-GCM / SM4 加密，支持密钥轮换（旧密钥迁移）。
- **写时复制内核**：策略图采用 COW 不可变快照 + 原子指针，读路径无锁，写路径串行化。

## 项目结构

| 包 | 职责 |
| --- | --- |
| `cmd/rbac` | 进程入口，装配配置、存储、缓存、PKI 与各服务 |
| `policy` | 权限引擎内核：数据模型、条件求值、可达性遍历、序列化、COW 快照 |
| `persist` | 策略存储：文件读写、加密、Redis 缓存、异步落盘、对账（reconcile）与暂停控制 |
| `cache` | Redis 客户端封装 |
| `crypto` | 加密算法注册表（AES-GCM、SM4）与密钥生成 |
| `config` | 基于 viper 的配置加载、默认值补全、密钥/CA 落地、配置热加载 |
| `pki` | 自签 CA 与服务端 TLS 证书生成 |
| `httpsvc` | 对外 HTTP / HTTPS REST API |
| `adminui` | 管理预览页（HTML + 只读 JSON API，Basic Auth） |

## 快速开始

要求：Go 1.27+。

```bash
# 构建
go build -o rbac ./cmd/rbac

# 用默认配置运行（读取 ./config.yaml，缺失则用内置默认值）
./rbac

# 指定配置文件
./rbac --config /path/to/config.yaml
# 或用环境变量（优先级高于 --config）
RBAC_CONFIG=/path/to/config.yaml ./rbac
```

默认所有服务（HTTP / HTTPS / admin）都是关闭的，进程仅完成存储初始化后等待信号。要对外提供服务，请在 `config.yaml` 中打开对应开关。例如开启 HTTP 与管理页：

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
| `server.http.*` | HTTP 监听开关、host、port；host 同时用作 HTTPS 证书 SAN | 关闭 / `0.0.0.0` / `8080` |
| `server.https.*` | HTTPS 监听开关与端口（TLS 证书由内置 CA 签发） | 关闭 / `8443` |
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
go test ./policy/... -cover
```

`policy` 包采用标准库 `testing` 的表驱动测试，无第三方断言依赖。

## 许可证

[MIT](LICENSE)
