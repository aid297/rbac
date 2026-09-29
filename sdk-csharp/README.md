# sdk-csharp

rbac 授权微服务的官方 .NET / C# 客户端 SDK。封装服务端 `/v1` REST API（HTTP / HTTPS）与 gRPC，对应 Go 侧的 [`sdk-go`](../sdk-go/)。

```csharp
using Rbac;

using var client = Client.Create("http://localhost:8080");
var allow = await client.EnforceAsync("alice", "doc:42");
```

要求：.NET 10（`net10.0`）。包名 `Rbac.Sdk.Cs`，命名空间 `Rbac`。HTTP 与 gRPC 共用同一套 API；gRPC 依赖 `Grpc.Net.Client` 与 `Google.Protobuf`（桩代码已提交在 `src/Rbac.Sdk/Generated/`，工程可独立构建，无需 `server/` 或 `protoc`）。

## 安装

```bash
dotnet add package Rbac.Sdk.Cs --version 0.1.1
# 或本地 monorepo：
dotnet add reference ../sdk-csharp/src/Rbac.Sdk/Rbac.Sdk.csproj
```

## 快速开始

### 基础用法

```csharp
using Rbac;

using var client = Client.Create("http://localhost:8080");

// 权限判定：检查 alice 是否可以访问 doc:42
var allow = await client.EnforceAsync("alice", "doc:42");
Console.WriteLine($"允许访问: {allow}");

// 带场景与时间点
allow = await client.EnforceAsync(
    "alice",
    "doc:42",
    new CallOptions()
        .WithScenarios("VIP")
        .WithNow(DateTimeOffset.UtcNow));

// 可达节点（结果包含 subject 自身）
var nodes = await client.ReachableAsync("alice", new CallOptions().WithScenarios("VIP"));
Console.WriteLine($"可达节点: {string.Join(", ", nodes)}");
```

`Client` 可在多线程间复用。默认请求超时 **30 秒**。方法均支持 `CancellationToken`。用完后应 `Dispose`（HTTP 释放自建 `HttpClient`；gRPC 关闭 channel）。

### gRPC

与 HTTP 共用同一套方法；用 `CreateGrpc` 连接服务端 `server.grpc` / `server.grpc_tls` 端口：

```csharp
// 明文 gRPC（server.grpc.port，默认 9080）
using var client = Client.CreateGrpc("localhost:9080");

// gRPC + TLS（server.grpc_tls.port，默认 9443），信任自签 CA
using var tlsClient = Client.CreateGrpc("localhost:9443",
    new ClientOptions().WithCACertFile("secret/ca.crt"));

// 或跳过校验（仅测试）
using var insecure = Client.CreateGrpc("localhost:9443",
    new ClientOptions().WithInsecureSkipVerify(true));
```

- `target` 为 `host:port`，不要写 `http://` / `grpc://`。
- 未配置 CA / `WithInsecureSkipVerify` 时使用**明文**传输（对应服务端 `grpc.enable`）。
- 配置了 `WithCACert*` 或 `WithInsecureSkipVerify(true)` 时走 **TLS**（对应 `grpc_tls`）。
- `WithHttpClient` / `WithCACertPath` 不支持 gRPC 构造（与 Go SDK 一致）。

gRPC 错误映射为与 HTTP 相同的 `ApiException` / `ApiErrors` 谓词。服务暂停为 `FailedPrecondition` → 503（`IsPaused`）；传输层 `Unavailable` **不会**包装为 `ApiException`，也不会被 `IsPaused` 命中。可用 `ApiErrors.IsGrpc` 区分 gRPC 路径错误。

### 参数说明

#### EnforceAsync 方法

```csharp
Task<bool> EnforceAsync(string subject, string target, CallOptions? options = null, CancellationToken cancellationToken = default)
```

- **`subject`**: 主体标识符，通常是用户 ID、角色名或服务名。例如：`"alice"`、`"user:123"`、`"service:payment"`
- **`target`**: 目标资源标识符，可以是文档、API 端点、功能模块等。例如：`"doc:42"`、`"/api/users"`、`"feature:export"`
- **`options`**: 可选的调用级选项
  - `WithScenarios(...)`: 场景列表，用于多租户或多环境隔离。例如：`"VIP"`、`"prod"`
  - `WithNow(DateTimeOffset)`: 判定时间点，用于时间窗口权限控制。缺省时使用服务端当前时间
- **`cancellationToken`**: 取消令牌，用于异步操作取消

返回值：
- **`Task<bool>`**: `true` 表示允许访问，`false` 表示拒绝
- 异常：网络错误、服务错误或 `ApiException`（如 4xx/5xx）

#### ReachableAsync 方法

```csharp
Task<IReadOnlyList<string>> ReachableAsync(string subject, CallOptions? options = null, CancellationToken cancellationToken = default)
```

- **`subject`**: 主体标识符（同上）
- **`options`**: 可选的调用级选项（同上）
- **`cancellationToken`**: 取消令牌

返回值：
- **`Task<IReadOnlyList<string>>`**: 从 subject 出发可达的所有节点列表（包含 subject 自身）。例如：`["alice", "role:editor", "role:viewer"]`

## HTTPS 与自签 CA

服务端 HTTPS 使用启动时自动生成的**自签 CA**。SDK 提供五种信任方式：

> **注意**：`baseUrl` 的主机名必须与证书 SAN（Subject Alternative Name）一致。例如证书签发的是 `localhost`，则不能用 `https://127.0.0.1:8443` 访问，反之亦然。

### 方式一：信任 CA 证书（PEM）

适用于从环境变量、配置中心或其他渠道获取 CA 证书的场景。

```csharp
// caPem 是 PEM 编码的 CA 证书字符串
// 可以从文件读取、环境变量获取，或从服务端下载
var caPem = File.ReadAllText("secret/ca.crt");

using var client = new Client("https://localhost:8443",
    new ClientOptions().WithCACert(caPem));
```

**CA 证书来源**：
- 从服务端启动目录的 `secret/ca.crt` 文件获取
- 通过服务端 `/v1/ca-cert` 端点下载（见方式三）
- 从配置管理系统或密钥管理服务获取

### 方式二：从文件读取 CA 证书

适用于 CA 证书已存储在本地文件的场景。

```csharp
// 直接指定 CA 证书文件路径
using var client = new Client("https://localhost:8443",
    new ClientOptions().WithCACertFile("secret/ca.crt"));
```

### 方式三：自动管理 CA 证书（推荐生产环境）

SDK 会自动检查本地路径是否存在 CA 证书；如果缺失或为空，会从服务端 `/v1/ca-cert` 端点下载并缓存到本地。下载失败会重试一次，两次都失败则返回错误。

```csharp
// 首次运行时会从服务端下载 CA 证书并缓存到指定路径
// 后续运行直接从本地读取，无需网络连接
using var client = new Client("https://localhost:8443",
    new ClientOptions().WithCACertPath("/path/to/cache/ca.pem"));
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

```csharp
using var client = new Client("https://localhost:8443",
    new ClientOptions().WithInsecureSkipVerify(true));
```

### 方式五：自带 HttpClient

适用于需要自定义 TLS 配置、代理、连接池等高级场景。设置后 TLS 相关选项和 `WithTimeout` 会被忽略。SDK 不会 Dispose 该实例。

```csharp
// 完全自定义 HTTP 客户端
var myHttpClient = new HttpClient
{
    Timeout = TimeSpan.FromSeconds(60),
    // 其他自定义配置
};

using var client = new Client("https://localhost:8443",
    new ClientOptions().WithHttpClient(myHttpClient));
```

## 构造选项

| 方法 | 说明 | 适用场景 |
| --- | --- | --- |
| `WithHttpClient(HttpClient)` | 自带 HttpClient；设置后 TLS 与 `WithTimeout` 被忽略；SDK 不 Dispose 该实例 | 需要自定义代理、连接池、TLS 配置等 |
| `WithCACert(pem)` | 信任自签 CA（PEM）；仅对 `https://` 生效，`http://` 下抛错 | CA 证书已从其他渠道获取 |
| `WithCACertFile(path)` | 从文件读取 CA；仅对 `https://` 生效 | CA 证书存储在本地文件 |
| `WithCACertPath(path)` | **自动管理 CA 证书**：检查本地路径是否存在，缺失时从服务端 `/v1/ca-cert` 下载并缓存；下载失败重试一次 | 生产环境推荐，自动化证书管理 |
| `WithInsecureSkipVerify(bool)` | 跳过 TLS 校验（仅测试）；仅对 `https://` 生效 | 开发/测试环境快速调试 |
| `WithUserAgent(string)` | 覆盖默认 User-Agent（默认 `rbac-sdk-csharp/<version>`） | 需要自定义请求头标识 |
| `WithTimeout(TimeSpan)` | 请求超时（默认 30s）。`TimeSpan.Zero` / 负值（含 `Timeout.InfiniteTimeSpan`）表示不设超时 | 根据业务需求调整超时时间 |

调用级选项（用于 `EnforceAsync` / `ReachableAsync`）：

| 方法 | 说明 |
| --- | --- |
| `WithScenarios(...)` | 场景列表，用于多租户或多环境隔离 |
| `WithNow(DateTimeOffset)` | 判定时间点（仅 Enforce；缺省为服务端当前时间） |

## API

### 读路径

```csharp
// HealthAsync 检查服务存活状态
await client.HealthAsync(cancellationToken);

// EnforceAsync 判定 subject 是否有权访问 target
// 参数：
//   - subject: 主体标识符（用户 ID、角色名、服务名等）
//   - target: 目标资源标识符（文档、API、功能模块等）
//   - options: 可选的调用级选项（场景列表、判定时间点）
//   - cancellationToken: 取消令牌
// 返回：true 表示允许，false 表示拒绝
Task<bool> EnforceAsync(string subject, string target, CallOptions? options = null, CancellationToken cancellationToken = default);

// ReachableAsync 列出从 subject 出发可达的所有节点（包含 subject 自身）
// 参数：
//   - subject: 主体标识符
//   - options: 可选的调用级选项（场景列表）
//   - cancellationToken: 取消令牌
// 返回：可达节点列表，如 ["alice", "role:editor", "role:viewer"]
Task<IReadOnlyList<string>> ReachableAsync(string subject, CallOptions? options = null, CancellationToken cancellationToken = default);
```

### 绑定管理

```csharp
// ListBindingsAsync 列出所有绑定关系
await client.ListBindingsAsync(cancellationToken);

// GetBindingAsync 获取单个绑定关系
// 参数：src=源节点, dst=目标节点, scenario=场景（空字符串表示默认场景）
await client.GetBindingAsync(src, dst, scenario, cancellationToken);

// AddBindingAsync 创建新的绑定关系
// 如果绑定已存在，抛出 IsConflict 异常
await client.AddBindingAsync(binding, cancellationToken);

// UpdateBindingAsync 更新现有绑定关系
// 如果绑定不存在，抛出 IsNotFound 异常
await client.UpdateBindingAsync(binding, cancellationToken);

// SetEnabledAsync 启用或禁用绑定关系
await client.SetEnabledAsync(src, dst, scenario, enabled, cancellationToken);

// RemoveBindingAsync 删除绑定关系
// 如果绑定不存在，抛出 IsNotFound 异常
await client.RemoveBindingAsync(src, dst, scenario, cancellationToken);
```

## 数据类型

```csharp
class Binding {
    string Src;               // 源节点（主体）
    string Dst;               // 目标节点（资源或角色）
    string Scenario;          // 场景标识（空字符串表示默认场景）
    bool? Enabled;            // 是否启用；null 时 JSON 省略，服务端默认为 true
    List<Condition> Conditions;  // 条件列表（无条件时为空列表）
}

class Condition {
    ConditionKind Kind;       // All ("ALL") 或 Time ("TIME")
    DateTimeOffset? Start;    // TIME 半开区间 [Start, End)；null 表示无界
    DateTimeOffset? End;
}

enum ConditionKind {
    All,   // 无条件恒真
    Time   // 时间窗口
}
```

### 构造条件

```csharp
// 无条件恒真（任何情况下都生效）
Condition.All()

// 时间窗口：半开区间 [start, end)
// start 和 end 可以为 null，表示单侧无界
Condition.TimeRange(start, end)      // 固定时间窗口
Condition.TimeRange(start, null)     // 仅有下界（从 start 开始永久有效）
Condition.TimeRange(null, end)       // 仅有上界（在 end 之前有效）
```

### 使用示例

新增带时间窗口的边：

```csharp
var start = new DateTimeOffset(2026, 6, 1, 0, 0, 0, TimeSpan.Zero);
var end = new DateTimeOffset(2026, 7, 1, 0, 0, 0, TimeSpan.Zero);

await client.AddBindingAsync(new Binding
{
    Src = "alice",
    Dst = "role:editor",
    Enabled = true,
    Conditions = [Condition.TimeRange(start, end)],
});
```

创建无条件绑定：

```csharp
await client.AddBindingAsync(new Binding
{
    Src = "bob",
    Dst = "role:viewer",
    Conditions = [Condition.All()],
});
```

> **时间精度**：服务端时间为 RFC3339 **秒级精度**。
>
> **Enabled 字段**：`bool?` 类型。`null` 时 JSON 不发送该字段，由服务端默认为 `true`。

## 错误处理

非 2xx 响应抛出 `ApiException`，并用 `ApiErrors` 谓词判断：

```csharp
try
{
    var binding = await client.GetBindingAsync("alice", "role:editor", "");
    Console.WriteLine($"绑定存在: {binding}");
}
catch (Exception ex) when (ApiErrors.IsNotFound(ex))
{
    Console.WriteLine("绑定不存在");
}
catch (Exception ex) when (ApiErrors.IsConflict(ex))
{
    Console.WriteLine("绑定已存在");
}
catch (Exception ex) when (ApiErrors.IsBadRequest(ex))
{
    Console.WriteLine($"参数错误: {ex.Message}");
}
catch (Exception ex) when (ApiErrors.IsPaused(ex))
{
    Console.WriteLine("服务暂停");
}
catch (Exception ex)
{
    Console.WriteLine($"其他错误: {ex.Message}");  // 网络 / TLS / 超时等
}
```

`ApiException` 属性：
- **`StatusCode`**: HTTP 状态码（400、404、409、503 等）
- **`Method`**: HTTP 方法（GET、POST、PUT、DELETE 等）
- **`Path`**: 请求路径（如 `/v1/bindings`）
- **`ApiMessage`**: 错误消息（来自服务端 `{"error":...}` 或 `{"reason":...}`）
- **`Body`**: 原始响应体字符串

传输层错误不会包装为 `ApiException`，可直接捕获 `HttpRequestException` 等。

### 完整示例

```csharp
using Rbac;

var cts = new CancellationTokenSource(TimeSpan.FromSeconds(10));

// 创建客户端（自动管理 CA 证书）
using var client = new Client("https://localhost:8443",
    new ClientOptions()
        .WithCACertPath("/tmp/rbac-ca.pem")
        .WithTimeout(TimeSpan.FromSeconds(10)));

// 检查健康状态
await client.HealthAsync(cts.Token);

// 权限判定
var allow = await client.EnforceAsync(
    "alice",
    "doc:42",
    new CallOptions().WithScenarios("VIP"),
    cts.Token);
Console.WriteLine($"允许访问: {allow}");

// 创建带时间窗口的绑定
var start = new DateTimeOffset(2026, 6, 1, 0, 0, 0, TimeSpan.Zero);
var end = new DateTimeOffset(2026, 7, 1, 0, 0, 0, TimeSpan.Zero);

try
{
    await client.AddBindingAsync(new Binding
    {
        Src = "alice",
        Dst = "role:editor",
        Enabled = true,
        Conditions = [Condition.TimeRange(start, end)],
    }, cts.Token);
    Console.WriteLine("绑定创建成功");
}
catch (Exception ex) when (ApiErrors.IsConflict(ex))
{
    Console.WriteLine("绑定已存在");
}

// 查询可达节点
var nodes = await client.ReachableAsync(
    "alice",
    new CallOptions().WithScenarios("VIP"),
    cts.Token);
Console.WriteLine($"可达节点: {string.Join(", ", nodes)}");
```

## 开发

```bash
cd sdk-csharp
dotnet test
```

gRPC 桩自包含于 `src/Rbac.Sdk/Generated/`（对应 `proto/rbac/v1/rbac.proto`）。在 monorepo 内同步服务端 proto 并重新生成：

```bash
cp ../server/api/proto/rbac/v1/rbac.proto proto/rbac/v1/rbac.proto
make proto
```

## 许可

[MIT](../LICENSE)
