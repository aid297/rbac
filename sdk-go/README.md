# sdk-go

rbac 授权微服务的官方 Go 客户端 SDK。封装服务端 `/v1` REST API（HTTP / HTTPS），仅依赖 Go 标准库。

```go
import rbac "github.com/aid297/rbac/sdk-go"
```

要求：Go 1.22+。

## 安装

```bash
go get github.com/aid297/rbac/sdk-go
```

## 快速开始

```go
ctx := context.Background()

client, err := rbac.NewClient("http://localhost:8080")
if err != nil {
    log.Fatal(err)
}

// 权限判定
allow, err := client.Enforce(ctx, "alice", "doc:42")
if err != nil {
    log.Fatal(err)
}

// 带场景与时间点
allow, err = client.Enforce(ctx, "alice", "doc:42",
    rbac.WithScenarios([]string{"VIP"}),
    rbac.WithNow(time.Now()),
)

// 可达节点（结果包含 subject 自身）
nodes, err := client.Reachable(ctx, "alice", rbac.WithScenarios([]string{"VIP"}))
```

`Client` 并发安全，可在整个程序中复用一个实例。所有方法都接收 `context.Context`，用于超时与取消。

> **默认超时**：未设置 `WithTimeout` 或 `WithHTTPClient` 时，请求超时默认为 **30 秒**。生产环境建议根据业务显式设置。

## HTTPS 与自签 CA

服务端 HTTPS 使用启动时自动生成的**自签 CA**。SDK 提供四种信任方式：

> **注意**：`baseURL` 的主机名必须与证书 SAN（Subject Alternative Name）一致。例如证书签发的是 `localhost`，则不能用 `https://127.0.0.1:8443` 访问，反之亦然。

```go
// 方式一：信任 CA 证书（PEM 字节）
client, err := rbac.NewClient("https://localhost:8443",
    rbac.WithCACert(caPEM),
)

// 方式二：从文件读取 CA 证书（通常是 server 的 secret/ca.crt）
client, err := rbac.NewClient("https://localhost:8443",
    rbac.WithCACertFile("secret/ca.crt"),
)

// 方式三：自动管理 CA 证书（推荐生产环境）
// SDK 会检查本地路径是否存在 CA 证书；如果缺失或为空，自动从服务端 /v1/ca-cert 下载并缓存
// 下载失败会重试一次，两次都失败则返回错误
client, err := rbac.NewClient("https://localhost:8443",
    rbac.WithCACertPath("/path/to/cache/ca.pem"),
)

// 方式四：跳过证书校验（仅建议测试使用）
client, err := rbac.NewClient("https://localhost:8443",
    rbac.WithInsecureSkipVerify(true),
)

// 方式五：自带 *http.Client（TLS 由你自行配置；此时 TLS 相关选项被忽略）
client, err := rbac.NewClient("https://localhost:8443",
    rbac.WithHTTPClient(myHTTPClient),
)
```

## 构造选项

| 选项 | 说明 |
| --- | --- |
| `WithHTTPClient(*http.Client)` | 自带 HTTP client；设置后 TLS 与 `WithTimeout` 选项被忽略 |
| `WithCACert(pem []byte)` | 信任自签 CA（PEM 字节）；仅对 `https://` 生效，`http://` 下返回错误 |
| `WithCACertFile(path string)` | 从文件读取 CA 证书；仅对 `https://` 生效 |
| `WithCACertPath(path string)` | **自动管理 CA 证书**：检查本地路径是否存在，缺失时从服务端 `/v1/ca-cert` 下载并缓存；下载失败重试一次 |
| `WithInsecureSkipVerify(bool)` | 跳过 TLS 证书校验（仅测试用）；仅对 `https://` 生效 |
| `WithUserAgent(string)` | 覆盖默认 User-Agent（默认 `rbac-sdk-go/<version>`） |
| `WithTimeout(time.Duration)` | 设置请求超时（默认 30s） |

调用级选项（用于 `Enforce` / `Reachable`）：

| 选项 | 说明 |
| --- | --- |
| `WithScenarios([]string)` | 场景列表 |
| `WithNow(time.Time)` | 判定时间点（仅 `Enforce`；缺省为服务端当前时间） |

## API

### 读路径

```go
Health(ctx context.Context) error
Enforce(ctx context.Context, subject, target string, opts ...CallOption) (bool, error)
Reachable(ctx context.Context, subject string, opts ...CallOption) ([]string, error)
```

### 绑定管理

```go
ListBindings(ctx context.Context) ([]Binding, error)
GetBinding(ctx context.Context, src, dst, scenario string) (Binding, error)
AddBinding(ctx context.Context, b Binding) (Binding, error)
UpdateBinding(ctx context.Context, b Binding) (Binding, error)
SetEnabled(ctx context.Context, src, dst, scenario string, enabled bool) error
RemoveBinding(ctx context.Context, src, dst, scenario string) error
```

## 数据类型

```go
type Binding struct {
    Src        string
    Dst        string
    Scenario   string
    Enabled    *bool       // nil 时 JSON 省略该字段，服务端默认为 true；用 Bool(v) 设置
    Conditions []Condition
}

type Condition struct {
    Kind  ConditionKind // KindAll ("ALL") 或 KindTime ("TIME")
    Start *time.Time    // TIME 半开区间 [Start, End)；nil 表示无界
    End   *time.Time
}

func Bool(v bool) *bool  // 辅助函数，用于构造 Enabled 指针
```

构造条件：

```go
rbac.AllCondition()                       // 无条件恒真
rbac.TimeRange(start, end)                // 半开区间 [start, end)，任一边可为 nil
rbac.TimeRange(start, nil)                // 仅有下界
```

新增带时间窗口的边：

```go
start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
end := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

_, err := client.AddBinding(ctx, rbac.Binding{
    Src: "alice", Dst: "role:editor", Enabled: rbac.Bool(true),
    Conditions: []rbac.Condition{rbac.TimeRange(&start, &end)},
})
```

> 服务端时间为 RFC3339 **秒级精度**，亚秒值在往返中会丢失。
>
> `Enabled` 为 `*bool` 指针类型：`nil`（零值）时 JSON 不发送该字段，由服务端默认为 `true`。需显式启用/禁用时用 `rbac.Bool(true)` / `rbac.Bool(false)`。

## 错误处理

非 2xx 响应统一转成 `*rbac.APIError`，并提供状态码谓词：

```go
b, err := client.GetBinding(ctx, "alice", "role:editor", "")
switch {
case rbac.IsNotFound(err):   // 404：绑定不存在
case rbac.IsConflict(err):   // 409：新增时重复
case rbac.IsBadRequest(err): // 400：参数/校验失败
case rbac.IsPaused(err):     // 503：服务暂停（如对账失败）
case err != nil:             // 其他：网络/TLS/超时等传输错误
}
```

`*APIError` 字段：`StatusCode`、`Method`、`Path`、`Message`（来自服务端 `{"error":...}`）、`Body`（原始响应体）。网络层 / TLS / 超时错误按原样返回，不包装为 `*APIError`，可用 `errors.Is(err, context.DeadlineExceeded)` 等判断。

## 许可

[MIT](../LICENSE)
