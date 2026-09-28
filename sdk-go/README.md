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

### 基础用法

```go
ctx := context.Background()

// 创建客户端
client, err := rbac.NewClient("http://localhost:8080")
if err != nil {
    log.Fatal(err)
}

// 权限判定：检查 alice 是否可以访问 doc:42
allow, err := client.Enforce(ctx, "alice", "doc:42")
if err != nil {
    log.Fatal(err)
}
fmt.Printf("允许访问: %v\n", allow)

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

### 参数说明

#### Enforce 方法

```go
func (c *Client) Enforce(ctx context.Context, subject, target string, opts ...CallOption) (bool, error)
```

- **`subject`**: 主体标识符，通常是用户 ID、角色名或服务名。例如：`"alice"`、`"user:123"`、`"service:payment"`
- **`target`**: 目标资源标识符，可以是文档、API 端点、功能模块等。例如：`"doc:42"`、`"/api/users"`、`"feature:export"`
- **`opts`**: 可选的调用级选项
  - `WithScenarios([]string)`: 场景列表，用于多租户或多环境隔离。例如：`[]string{"VIP"}`、`[]string{"prod"}`
  - `WithNow(time.Time)`: 判定时间点，用于时间窗口权限控制。缺省时使用服务端当前时间

返回值：
- **`allow bool`**: `true` 表示允许访问，`false` 表示拒绝
- **`error`**: 网络错误、服务错误或 API 错误（如 4xx/5xx）

#### Reachable 方法

```go
func (c *Client) Reachable(ctx context.Context, subject string, opts ...CallOption) ([]string, error)
```

- **`subject`**: 主体标识符（同上）
- **`opts`**: 可选的调用级选项（同上）

返回值：
- **`[]string`**: 从 subject 出发可达的所有节点列表（包含 subject 自身）。例如：`["alice", "role:editor", "role:viewer"]`
- **`error`**: 错误信息

## HTTPS 与自签 CA

服务端 HTTPS 使用启动时自动生成的**自签 CA**。SDK 提供五种信任方式：

> **注意**：`baseURL` 的主机名必须与证书 SAN（Subject Alternative Name）一致。例如证书签发的是 `localhost`，则不能用 `https://127.0.0.1:8443` 访问，反之亦然。

### 方式一：信任 CA 证书（PEM 字节）

适用于从环境变量、配置中心或其他渠道获取 CA 证书的场景。

```go
// caPEM 是 PEM 编码的 CA 证书字节数组
// 可以从文件读取、环境变量获取，或从服务端下载
caPEM, err := os.ReadFile("secret/ca.crt")
if err != nil {
    log.Fatal(err)
}

client, err := rbac.NewClient("https://localhost:8443",
    rbac.WithCACert(caPEM),
)
```

**CA 证书来源**：
- 从服务端启动目录的 `secret/ca.crt` 文件获取
- 通过服务端 `/v1/ca-cert` 端点下载（见方式三）
- 从配置管理系统或密钥管理服务获取

### 方式二：从文件读取 CA 证书

适用于 CA 证书已存储在本地文件的场景。

```go
// 直接指定 CA 证书文件路径
client, err := rbac.NewClient("https://localhost:8443",
    rbac.WithCACertFile("secret/ca.crt"),
)
```

### 方式三：自动管理 CA 证书（推荐生产环境）

SDK 会自动检查本地路径是否存在 CA 证书；如果缺失或为空，会从服务端 `/v1/ca-cert` 端点下载并缓存到本地。下载失败会重试一次，两次都失败则返回错误。

```go
// 首次运行时会从服务端下载 CA 证书并缓存到指定路径
// 后续运行直接从本地读取，无需网络连接
client, err := rbac.NewClient("https://localhost:8443",
    rbac.WithCACertPath("/path/to/cache/ca.pem"),
)
```

**工作流程**：
1. 检查 `/path/to/cache/ca.pem` 是否存在且非空
2. 如果存在，直接加载并使用
3. 如果不存在或为空，向 `https://localhost:8443/v1/ca-cert` 发起 GET 请求
4. 将下载的 PEM 数据写入缓存文件
5. 如果下载失败，重试一次；再次失败则返回错误

**优势**：
- 首次部署时无需手动分发 CA 证书
- 证书轮换后自动更新
- 本地缓存减少网络依赖

### 方式四：跳过证书校验（仅建议测试使用）

⚠️ **警告**：此方式会禁用 TLS 证书验证，存在中间人攻击风险，仅用于开发或测试环境。

```go
client, err := rbac.NewClient("https://localhost:8443",
    rbac.WithInsecureSkipVerify(true),
)
```

### 方式五：自带 *http.Client

适用于需要自定义 TLS 配置、代理、连接池等高级场景。设置后 TLS 相关选项和 `WithTimeout` 会被忽略。

```go
// 完全自定义 HTTP 客户端
myHTTPClient := &http.Client{
    Timeout: 60 * time.Second,
    Transport: &http.Transport{
        // 自定义传输层配置
    },
}

client, err := rbac.NewClient("https://localhost:8443",
    rbac.WithHTTPClient(myHTTPClient),
)
```

## 构造选项

| 选项 | 说明 | 适用场景 |
| --- | --- | --- |
| `WithHTTPClient(*http.Client)` | 自带 HTTP client；设置后 TLS 与 `WithTimeout` 选项被忽略 | 需要自定义代理、连接池、TLS 配置等 |
| `WithCACert(pem []byte)` | 信任自签 CA（PEM 字节）；仅对 `https://` 生效，`http://` 下返回错误 | CA 证书已从其他渠道获取 |
| `WithCACertFile(path string)` | 从文件读取 CA 证书；仅对 `https://` 生效 | CA 证书存储在本地文件 |
| `WithCACertPath(path string)` | **自动管理 CA 证书**：检查本地路径是否存在，缺失时从服务端 `/v1/ca-cert` 下载并缓存；下载失败重试一次 | 生产环境推荐，自动化证书管理 |
| `WithInsecureSkipVerify(bool)` | 跳过 TLS 证书校验（仅测试用）；仅对 `https://` 生效 | 开发/测试环境快速调试 |
| `WithUserAgent(string)` | 覆盖默认 User-Agent（默认 `rbac-sdk-go/<version>`） | 需要自定义请求头标识 |
| `WithTimeout(time.Duration)` | 设置请求超时（默认 30s） | 根据业务需求调整超时时间 |

调用级选项（用于 `Enforce` / `Reachable`）：

| 选项 | 说明 |
| --- | --- |
| `WithScenarios([]string)` | 场景列表，用于多租户或多环境隔离 |
| `WithNow(time.Time)` | 判定时间点（仅 `Enforce`；缺省为服务端当前时间） |

## API

### 读路径

```go
// Health 检查服务存活状态
Health(ctx context.Context) error

// Enforce 判定 subject 是否有权访问 target
// 参数：
//   - subject: 主体标识符（用户 ID、角色名、服务名等）
//   - target: 目标资源标识符（文档、API、功能模块等）
//   - opts: 可选的调用级选项（场景列表、判定时间点）
// 返回：allow=true 表示允许，allow=false 表示拒绝
Enforce(ctx context.Context, subject, target string, opts ...CallOption) (bool, error)

// Reachable 列出从 subject 出发可达的所有节点（包含 subject 自身）
// 参数：
//   - subject: 主体标识符
//   - opts: 可选的调用级选项（场景列表）
// 返回：可达节点列表，如 ["alice", "role:editor", "role:viewer"]
Reachable(ctx context.Context, subject string, opts ...CallOption) ([]string, error)
```

### 绑定管理

```go
// ListBindings 列出所有绑定关系
ListBindings(ctx context.Context) ([]Binding, error)

// GetBinding 获取单个绑定关系
// 参数：src=源节点, dst=目标节点, scenario=场景（空字符串表示默认场景）
GetBinding(ctx context.Context, src, dst, scenario string) (Binding, error)

// AddBinding 创建新的绑定关系
// 如果绑定已存在，返回 IsConflict 错误
AddBinding(ctx context.Context, b Binding) (Binding, error)

// UpdateBinding 更新现有绑定关系
// 如果绑定不存在，返回 IsNotFound 错误
UpdateBinding(ctx context.Context, b Binding) (Binding, error)

// SetEnabled 启用或禁用绑定关系
SetEnabled(ctx context.Context, src, dst, scenario string, enabled bool) error

// RemoveBinding 删除绑定关系
// 如果绑定不存在，返回 IsNotFound 错误
RemoveBinding(ctx context.Context, src, dst, scenario string) error
```

## 数据类型

```go
type Binding struct {
    Src        string      // 源节点（主体）
    Dst        string      // 目标节点（资源或角色）
    Scenario   string      // 场景标识（空字符串表示默认场景）
    Enabled    *bool       // 是否启用；nil 时 JSON 省略该字段，服务端默认为 true；用 Bool(v) 设置
    Conditions []Condition // 条件列表（无条件时为空切片）
}

type Condition struct {
    Kind  ConditionKind // KindAll ("ALL") 或 KindTime ("TIME")
    Start *time.Time    // TIME 半开区间 [Start, End)；nil 表示无界
    End   *time.Time
}

func Bool(v bool) *bool  // 辅助函数，用于构造 Enabled 指针
```

### 构造条件

```go
// 无条件恒真（任何情况下都生效）
rbac.AllCondition()

// 时间窗口：半开区间 [start, end)
// start 和 end 可以为 nil，表示单侧无界
rbac.TimeRange(start, end)                // 固定时间窗口
rbac.TimeRange(start, nil)                // 仅有下界（从 start 开始永久有效）
rbac.TimeRange(nil, end)                  // 仅有上界（在 end 之前有效）
```

### 使用示例

新增带时间窗口的边：

```go
start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
end := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

_, err := client.AddBinding(ctx, rbac.Binding{
    Src: "alice", 
    Dst: "role:editor", 
    Enabled: rbac.Bool(true),
    Conditions: []rbac.Condition{rbac.TimeRange(&start, &end)},
})
```

创建无条件绑定：

```go
_, err := client.AddBinding(ctx, rbac.Binding{
    Src: "bob", 
    Dst: "role:viewer",
    Conditions: []rbac.Condition{rbac.AllCondition()},
})
```

> **时间精度**：服务端时间为 RFC3339 **秒级精度**，亚秒值在往返中会丢失。
>
> **Enabled 字段**：`*bool` 指针类型。`nil`（零值）时 JSON 不发送该字段，由服务端默认为 `true`。需显式启用/禁用时用 `rbac.Bool(true)` / `rbac.Bool(false)`。

## 错误处理

非 2xx 响应统一转成 `*rbac.APIError`，并提供状态码谓词：

```go
b, err := client.GetBinding(ctx, "alice", "role:editor", "")
switch {
case rbac.IsNotFound(err):   // 404：绑定不存在
    fmt.Println("绑定不存在")
case rbac.IsConflict(err):   // 409：新增时重复
    fmt.Println("绑定已存在")
case rbac.IsBadRequest(err): // 400：参数/校验失败
    fmt.Println("参数错误:", err)
case rbac.IsPaused(err):     // 503：服务暂停（如对账失败）
    fmt.Println("服务暂停")
case err != nil:             // 其他：网络/TLS/超时等传输错误
    fmt.Println("其他错误:", err)
default:
    fmt.Println("绑定存在:", b)
}
```

`*APIError` 字段：
- **`StatusCode`**: HTTP 状态码（400、404、409、503 等）
- **`Method`**: HTTP 方法（GET、POST、PUT、DELETE 等）
- **`Path`**: 请求路径（如 `/v1/bindings`）
- **`Message`**: 错误消息（来自服务端 `{"error":...}` 或 `{"reason":...}`）
- **`Body`**: 原始响应体字节数组

网络层 / TLS / 超时错误按原样返回，不包装为 `*APIError`，可用 `errors.Is(err, context.DeadlineExceeded)` 等判断。

### 完整示例

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"
    
    rbac "github.com/aid297/rbac/sdk-go"
)

func main() {
    ctx := context.Background()
    
    // 创建客户端（自动管理 CA 证书）
    client, err := rbac.NewClient("https://localhost:8443",
        rbac.WithCACertPath("/tmp/rbac-ca.pem"),
        rbac.WithTimeout(10*time.Second),
    )
    if err != nil {
        log.Fatal(err)
    }
    
    // 检查健康状态
    if err := client.Health(ctx); err != nil {
        log.Fatal("服务不可用:", err)
    }
    
    // 权限判定
    allow, err := client.Enforce(ctx, "alice", "doc:42",
        rbac.WithScenarios([]string{"VIP"}),
    )
    if err != nil {
        if rbac.IsNotFound(err) {
            fmt.Println("资源不存在")
        } else {
            log.Fatal("判定失败:", err)
        }
    }
    fmt.Printf("允许访问: %v\n", allow)
    
    // 创建带时间窗口的绑定
    start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
    end := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
    
    _, err = client.AddBinding(ctx, rbac.Binding{
        Src: "alice",
        Dst: "role:editor",
        Enabled: rbac.Bool(true),
        Conditions: []rbac.Condition{rbac.TimeRange(&start, &end)},
    })
    if err != nil {
        if rbac.IsConflict(err) {
            fmt.Println("绑定已存在")
        } else {
            log.Fatal("创建绑定失败:", err)
        }
    }
    
    // 查询可达节点
    nodes, err := client.Reachable(ctx, "alice", rbac.WithScenarios([]string{"VIP"}))
    if err != nil {
        log.Fatal("查询失败:", err)
    }
    fmt.Printf("可达节点: %v\n", nodes)
}
```

## 许可

[MIT](../LICENSE)
