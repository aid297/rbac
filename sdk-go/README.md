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

## HTTPS 与自签 CA

服务端 HTTPS 使用启动时自动生成的**自签 CA**。SDK 提供三种信任方式：

```go
// 方式一：信任 CA 证书（PEM 字节）
client, err := rbac.NewClient("https://localhost:8443",
    rbac.WithCACert(caPEM),
)

// 方式二：从文件读取 CA 证书（通常是 server 的 secret/ca.crt）
client, err := rbac.NewClient("https://localhost:8443",
    rbac.WithCACertFile("secret/ca.crt"),
)

// 方式三：跳过证书校验（仅建议测试使用）
client, err := rbac.NewClient("https://localhost:8443",
    rbac.WithInsecureSkipVerify(true),
)

// 方式四：自带 *http.Client（TLS 由你自行配置；此时 TLS 相关选项被忽略）
client, err := rbac.NewClient("https://localhost:8443",
    rbac.WithHTTPClient(myHTTPClient),
)
```

## 构造选项

| 选项 | 说明 |
| --- | --- |
| `WithHTTPClient(*http.Client)` | 自带 HTTP client；设置后 TLS 与 `WithTimeout` 选项被忽略 |
| `WithCACert(pem []byte)` | 信任自签 CA（PEM 字节） |
| `WithCACertFile(path string)` | 从文件读取 CA 证书 |
| `WithInsecureSkipVerify(bool)` | 跳过 TLS 证书校验（仅测试用） |
| `WithUserAgent(string)` | 覆盖默认 User-Agent（默认 `rbac-sdk-go/<version>`） |
| `WithTimeout(time.Duration)` | 设置请求超时 |

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
    Enabled    bool
    Conditions []Condition
}

type Condition struct {
    Kind  ConditionKind // KindAll ("ALL") 或 KindTime ("TIME")
    Start *time.Time    // TIME 半开区间 [Start, End)；nil 表示无界
    End   *time.Time
}
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
    Src: "alice", Dst: "role:editor", Enabled: true,
    Conditions: []rbac.Condition{rbac.TimeRange(&start, &end)},
})
```

> 服务端时间为 RFC3339 **秒级精度**，亚秒值在往返中会丢失。

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
