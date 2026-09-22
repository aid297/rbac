# sdk-go 设计：rbac 微服务的 Go 客户端 SDK

- 日期：2026-09-22
- 状态：已批准，待实现
- 范围：① 将仓库重构为 monorepo（服务端收入 `server/`）；② 新建 `sdk-go` 项目，作为对接 rbac 微服务的官方 Go 客户端 SDK。第一版仅覆盖服务端已支持的 HTTP / HTTPS 两种协议。

## 1. 背景与目标

rbac 微服务当前对外暴露 HTTP / HTTPS 的 `/v1` REST API（无鉴权）与一个 Basic Auth 的管理预览页。第三方 Go 程序若想接入，目前只能手写 `net/http` 调用、自行处理 JSON 编解码、TLS 信任与错误状态码。

本设计提供一个**独立、零第三方依赖（仅标准库）**的 Go SDK，封装：

- `/v1` 全部端点：健康检查、权限判定（Enforce）、可达性查询（Reachable）、绑定（Binding）的增删改查与启用开关。
- HTTPS 下对服务端**自签 CA** 的信任配置。
- 统一、可编程判断的错误模型。

非目标（v1 不做，YAGNI）：

- 重试 / 退避、连接池调优、熔断。
- 管理页（`/api/*`，Basic Auth）相关接口——SDK 只面向 `/v1`。
- 鉴权头注入（`/v1` 当前无鉴权）。
- gRPC / 其他协议（服务端尚不支持）。
- 从 OpenAPI 生成代码（无 spec，端点数量有限）。

## 2. 仓库重构（monorepo）

目标布局：

```
rbac/
├── server/                 # 微服务（由仓库根迁入）
│   ├── go.mod              # module github.com/aid297/rbac/server
│   ├── go.sum
│   ├── cmd/rbac/main.go
│   ├── policy/ persist/ cache/ crypto/ config/ pki/ httpsvc/ adminui/
│   ├── config.yaml
│   └── README.md           # 原根 README（服务端详解）迁入
├── sdk-go/                 # 新 SDK：module github.com/aid297/rbac/sdk-go
├── docs/superpowers/specs/ # 留在根：服务端与 SDK 的设计文档同置
├── LICENSE                 # 留在根
├── README.md               # 新写：monorepo 概览，指向 server/ 与 sdk-go/
└── .gitignore              # 留在根
```

迁移步骤：

1. 用 `git mv` 将服务端文件移入 `server/`（保留历史）：`go.mod`、`go.sum`、`cmd/`、`policy/`、`persist/`、`cache/`、`crypto/`、`config/`、`pki/`、`httpsvc/`、`adminui/`、`config.yaml`、`README.md`。
2. 修改 `server/go.mod` 的 module 行：`rbac` → `github.com/aid297/rbac/server`。
3. 重写服务端所有内部 import：`rbac/...` → `github.com/aid297/rbac/server/...`（含 `cmd/rbac/main.go` 与各包测试）。
4. `docs/`、`LICENSE`、`.gitignore` 保持在根。`.gitignore` 中 `stats/`、`secret/` 这类无前导斜杠的模式天然递归匹配，迁移后仍能忽略 `server/stats/`、`server/secret/`；`/rbac` 等根级模式按需调整。
5. 验证：在 `server/` 下 `go build ./...`、`go vet ./...`、`go test -race ./...` 全绿。

两个 module 相互独立，SDK 不 import 服务端，故无需 `go.work`（且 `.gitignore` 已忽略 `go.work`）。

## 3. SDK 架构

- 包名：`rbac`，import 路径 `github.com/aid297/rbac/sdk-go`。
- 仅依赖标准库（`net/http`、`encoding/json`、`crypto/tls`、`crypto/x509`、`time`、`context`、`net/url`、`os`、`fmt`、`errors`、`io`、`bytes`）。
- 单一并发安全的 `Client`，内部持有 `*http.Client` 与解析后的 base URL。所有方法接收 `context.Context`，用于超时与取消。
- 与线协议（JSON DTO）一一对应，但用更符合 Go 习惯的类型暴露（时间为 `*time.Time` 而非字符串）。

### 3.1 Client 与构造

```go
type Client struct { /* baseURL *url.URL, hc *http.Client, ua string */ }

func NewClient(baseURL string, opts ...Option) (*Client, error)
```

`NewClient` 校验 baseURL 可解析且 scheme 为 `http` 或 `https`，否则返回错误。

### 3.2 配置选项（functional options）

```go
type Option func(*clientConfig) error

WithHTTPClient(*http.Client)   // 自带 client；设置后 TLS 相关选项被忽略
WithCACert(pem []byte)         // 信任自签 CA（PEM 字节）
WithCACertFile(path string)    // 同上，从文件读取
WithInsecureSkipVerify(bool)   // 跳过证书校验（仅建议测试用）
WithUserAgent(string)          // 覆盖默认 User-Agent
WithTimeout(time.Duration)     // 设置 http.Client.Timeout
```

TLS 构建规则：

- 若设置了 `WithHTTPClient`，直接使用该 client，**忽略** `WithCACert*` / `WithInsecureSkipVerify` / `WithTimeout`（由调用方自负）。
- 否则新建 `*http.Client`：当 baseURL 为 https 且提供了 CA 或 insecure 选项时，构造 `*tls.Config`——把 CA 追加进 `x509.CertPool`（`RootCAs`），并按需设置 `InsecureSkipVerify`，挂到 `Transport.TLSClientConfig`。
- 纯 http baseURL 不配置 TLS。
- `WithCACert` / `WithCACertFile` 的 PEM 无法解析为证书时，`NewClient` 返回错误（快速失败）。
- 默认 `User-Agent`：`rbac-sdk-go/<version>`（version 为包级常量）。

### 3.3 调用选项（per-call options）

Enforce / Reachable 的可选查询参数通过 CallOption 传入，保持常见调用简洁：

```go
type CallOption func(*callParams)

WithScenarios([]string)  // 场景列表
WithNow(time.Time)       // 判定时间点（仅 Enforce 使用；缺省为服务端当前时间）
```

### 3.4 方法集（全部接收 ctx）

读路径：

```go
Health(ctx context.Context) error
Enforce(ctx context.Context, subject, target string, opts ...CallOption) (bool, error)
Reachable(ctx context.Context, subject string, opts ...CallOption) ([]string, error)
```

- `Health`：`GET /healthz`，200 返回 nil；503（暂停）返回 `*APIError`（可经 `IsPaused` 判断）。
- `Enforce`：`POST /v1/enforce`，body `{subject,target,scenarios?,now?}`，解析 `{"allow":bool}`。
- `Reachable`：`GET /v1/reachable?subject=...&scenario=...`（scenario 可重复），解析 `{"reachable":[]string}`。结果**包含 subject 自身**（与服务端语义一致，展示层自行过滤）。

绑定 CRUD：

```go
ListBindings(ctx context.Context) ([]Binding, error)
GetBinding(ctx context.Context, src, dst, scenario string) (Binding, error)
AddBinding(ctx context.Context, b Binding) (Binding, error)
UpdateBinding(ctx context.Context, b Binding) (Binding, error)
SetEnabled(ctx context.Context, src, dst, scenario string, enabled bool) error
RemoveBinding(ctx context.Context, src, dst, scenario string) error
```

- `ListBindings`：`GET /v1/bindings`（无 src/dst），解析 `{"bindings":[...]}`。
- `GetBinding`：`GET /v1/bindings?src=&dst=&scenario=`，404 → `ErrNotFound`（`IsNotFound`）。
- `AddBinding`：`POST /v1/bindings`，成功 201 返回服务端回显的 Binding；409（重复）→ `IsConflict`。
- `UpdateBinding`：`PUT /v1/bindings`，成功 200；404（不存在，不会新增）→ `IsNotFound`。
- `SetEnabled`：`PATCH /v1/bindings/enabled`，body `{src,dst,scenario,enabled}`，成功 200 `{"ok":true}`。
- `RemoveBinding`：`DELETE /v1/bindings?src=&dst=&scenario=`，成功 204（无 body）。

## 4. 数据类型

```go
type Binding struct {
    Src        string      `json:"src"`
    Dst        string      `json:"dst"`
    Scenario   string      `json:"scenario"`
    Enabled    bool        `json:"enabled"`
    Conditions []Condition `json:"conditions"`
}

type ConditionKind string
const (
    KindAll  ConditionKind = "ALL"
    KindTime ConditionKind = "TIME"
)

type Condition struct {
    Kind  ConditionKind
    Start *time.Time // 仅 TIME 有意义；nil 表示无下界
    End   *time.Time // 仅 TIME 有意义；nil 表示无上界
}
```

构造器：

```go
func AllCondition() Condition
func TimeRange(start, end *time.Time) Condition
```

线格式（与服务端 DTO 对齐）：`condition` 序列化为 `{"kind":"ALL"}` 或 `{"kind":"TIME","start":"<RFC3339>","end":"<RFC3339>"}`，start/end 为空时省略。`Condition` 实现自定义 `MarshalJSON` / `UnmarshalJSON`：

- 序列化：`Kind=="TIME"` 且 Start/End 非 nil 时，以 `time.RFC3339`（UTC）写成字符串字段；否则省略。
- 反序列化：读取 `kind`、可选 `start`/`end` 字符串，按 RFC3339 解析为 `*time.Time`；解析失败返回错误。

注意：服务端时间是**秒级精度**（RFC3339），亚秒在往返中会丢失——SDK 不承诺保留亚秒。

`Binding` 在新增时若 `Conditions` 为空，SDK 不自动填充（与服务端 `readBinding` 一致：服务端把空 conditions 视作 `ALL`）。SDK 提供 `AllCondition()` 供调用方显式表达。

## 5. 错误模型

```go
type APIError struct {
    StatusCode int
    Method     string
    Path       string
    Message    string // 来自服务端 {"error":"..."} 的 message
    Body       []byte // 原始响应体，便于排查
}
func (e *APIError) Error() string
```

判定辅助（基于 `*APIError.StatusCode`，配合 `errors.As`）：

```go
func IsNotFound(err error) bool   // 404
func IsConflict(err error) bool   // 409
func IsPaused(err error) bool     // 503
func IsBadRequest(err error) bool // 400
```

约定：

- 任何非 2xx 响应都转成 `*APIError`（尽力解析 `{"error":...}`，解析不出则 Message 留空、保留 Body）。
- 网络层 / TLS / 超时错误按原样返回（可被 `errors.Is(err, context.DeadlineExceeded)` 等识别），不包装成 `*APIError`。
- 请求构造错误（如 baseURL 非法）由 `NewClient` 或方法直接返回普通 error。
- 响应体大小设上限（如 4 MiB）以防异常服务端导致内存放大。

## 6. HTTP 传输细节

- 统一内部方法 `do(ctx, method, path string, query url.Values, body any, out any) error`：
  - 拼接 `baseURL + path`，附加 query。
  - body 非 nil 时 JSON 编码，`Content-Type: application/json`。
  - 带上 `User-Agent`、`Accept: application/json`。
  - 用 `http.NewRequestWithContext` 绑定 ctx。
  - 发送、读取（限长）、判断状态码：2xx → 若 `out` 非 nil 则解码；非 2xx → 构造 `*APIError`。
  - 204 / 空 body 时不解码。
- 请求体大小：服务端限制 1 MiB；SDK 不主动限制请求体，但响应读取限长 4 MiB。
- 幂等性：SDK 不做自动重试，调用方自行决定。

## 7. 测试策略

仅用标准库 `testing`，表驱动，风格与 `server/policy` 一致。

- HTTP 路径：`httptest.NewServer` 起一个假服务端，按端点返回固定 JSON，断言 SDK 解析正确、请求方法/路径/query/body 符合预期。
- 错误映射：分别让假服务端返回 400/404/409/503，断言对应 `Is*` 谓词为真、`*APIError` 字段正确。
- HTTPS / TLS：`httptest.NewTLSServer`：
  - `WithCACert(srv.Certificate() 的 PEM)` → 请求成功（证明 CA 信任生效）。
  - `WithInsecureSkipVerify(true)` → 请求成功。
  - 不带任何 TLS 选项 → 请求失败（证书不被信任）。
  - `WithHTTPClient(srv.Client())` → 请求成功（自带 client 路径）。
- 类型往返：`Condition` 的 `MarshalJSON` / `UnmarshalJSON` 对 ALL 与 TIME（含单边无界、双边有界）正确；时间为秒级。
- 边界：`NewClient` 对非法 baseURL、无法解析的 CA PEM 返回错误。
- 覆盖率目标：核心 client / 传输 / 类型 ≥ 90%。

验收：`sdk-go` 下 `go build ./...`、`go vet ./...`、`go test -race ./...` 全绿。

## 8. 交付物

1. 重构后的 `server/`（module 改名 + import 重写，构建与测试通过）。
2. `sdk-go/`：`go.mod`、`client.go`、`options.go`、`types.go`、`errors.go`、对应 `_test.go`、`README.md`。
3. 根 `README.md`（monorepo 概览）。
4. 本设计文档。

## 9. 决策与未决项

已决：

- 目录名 `server`；module 全路径化（`github.com/aid297/rbac/server`、`github.com/aid297/rbac/sdk-go`）。
- SDK 独立、零第三方依赖、不 import 服务端。
- v1 覆盖 `/v1` 全部端点（读 + 绑定 CRUD）。
- TLS：CA 证书 / 自带 client / insecure 三种方式。

未决（留待后续版本）：

- 是否需要管理页（Basic Auth）接口的 SDK 支持。
- 是否引入可选重试 / 退避。
- 服务端新增协议（如 gRPC）后的 SDK 扩展。
