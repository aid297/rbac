# sdk-csharp

rbac 授权微服务的官方 .NET / C# 客户端 SDK。封装服务端 `/v1` REST API（HTTP / HTTPS），对应 Go 侧的 [`sdk-go`](../sdk-go/)。

```csharp
using Rbac;

using var client = Client.Create("http://localhost:8080");
var allow = await client.EnforceAsync("alice", "doc:42");
```

要求：.NET 10（`net10.0`）。包名 `Rbac.Sdk`，命名空间 `Rbac`。

## 安装

```bash
dotnet add package Rbac.Sdk --version 0.1.0
# 或本地 monorepo：
dotnet add reference ../sdk-csharp/src/Rbac.Sdk/Rbac.Sdk.csproj
```

## 快速开始

```csharp
using Rbac;

using var client = Client.Create("http://localhost:8080");

// 权限判定
var allow = await client.EnforceAsync("alice", "doc:42");

// 带场景与时间点
allow = await client.EnforceAsync(
    "alice",
    "doc:42",
    new CallOptions()
        .WithScenarios("VIP")
        .WithNow(DateTimeOffset.UtcNow));

// 可达节点（结果包含 subject 自身）
var nodes = await client.ReachableAsync("alice", new CallOptions().WithScenarios("VIP"));
```

`Client` 可在多线程间复用。默认请求超时 **30 秒**。方法均支持 `CancellationToken`。

## HTTPS 与自签 CA

服务端 HTTPS 使用启动时自动生成的**自签 CA**。SDK 提供四种信任方式：

> **注意**：`baseUrl` 的主机名必须与证书 SAN 一致。例如证书签发的是 `localhost`，则不能用 `https://127.0.0.1:8443` 访问，反之亦然。

```csharp
// 方式一：信任 CA 证书（PEM）
using var client = new Client("https://localhost:8443",
    new ClientOptions().WithCACert(caPem));

// 方式二：从文件读取 CA 证书（通常是 server 的 secret/ca.crt）
using var client = new Client("https://localhost:8443",
    new ClientOptions().WithCACertFile("secret/ca.crt"));

// 方式三：自动管理 CA 证书（推荐生产环境）
// SDK 会检查本地路径是否存在 CA 证书；如果缺失或为空，自动从服务端 /v1/ca-cert 下载并缓存
// 下载失败会重试一次，两次都失败则返回错误
using var client = new Client("https://localhost:8443",
    new ClientOptions().WithCACertPath("/path/to/cache/ca.pem"));

// 方式四：跳过证书校验（仅建议测试使用）
using var client = new Client("https://localhost:8443",
    new ClientOptions().WithInsecureSkipVerify(true));

// 方式五：自带 HttpClient（TLS 由你自行配置；此时 TLS / Timeout 选项被忽略）
using var client = new Client("https://localhost:8443",
    new ClientOptions().WithHttpClient(myHttpClient));
```

## 构造选项

| 方法 | 说明 |
| --- | --- |
| `WithHttpClient(HttpClient)` | 自带 HttpClient；设置后 TLS 与 `WithTimeout` 被忽略；SDK 不 Dispose 该实例 |
| `WithCACert(pem)` | 信任自签 CA（PEM）；仅对 `https://` 生效，`http://` 下抛错 |
| `WithCACertFile(path)` | 从文件读取 CA；仅对 `https://` 生效 |
| `WithCACertPath(path)` | **自动管理 CA 证书**：检查本地路径是否存在，缺失时从服务端 `/v1/ca-cert` 下载并缓存；下载失败重试一次 |
| `WithInsecureSkipVerify(bool)` | 跳过 TLS 校验（仅测试）；仅对 `https://` 生效 |
| `WithUserAgent(string)` | 覆盖默认 User-Agent（默认 `rbac-sdk-csharp/<version>`） |
| `WithTimeout(TimeSpan)` | 请求超时（默认 30s） |

调用级选项（用于 `EnforceAsync` / `ReachableAsync`）：

| 方法 | 说明 |
| --- | --- |
| `WithScenarios(...)` | 场景列表 |
| `WithNow(DateTimeOffset)` | 判定时间点（仅 Enforce；缺省为服务端当前时间） |

## API

### 读路径

```csharp
await client.HealthAsync();
await client.EnforceAsync(subject, target, options?);
await client.ReachableAsync(subject, options?);
```

### 绑定管理

```csharp
await client.ListBindingsAsync();
await client.GetBindingAsync(src, dst, scenario);
await client.AddBindingAsync(binding);
await client.UpdateBindingAsync(binding);
await client.SetEnabledAsync(src, dst, scenario, enabled);
await client.RemoveBindingAsync(src, dst, scenario);
```

## 数据类型

```csharp
class Binding {
    string Src, Dst, Scenario;
    bool? Enabled;              // null 时 JSON 省略，服务端默认为 true
    List<Condition> Conditions;
}

class Condition {
    ConditionKind Kind;         // All ("ALL") / Time ("TIME")
    DateTimeOffset? Start;      // TIME 半开区间 [Start, End)
    DateTimeOffset? End;
}
```

构造条件：

```csharp
Condition.All();
Condition.TimeRange(start, end);
Condition.TimeRange(start, null);
```

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

> 服务端时间为 RFC3339 **秒级精度**。
>
> `Enabled` 为 `bool?`：`null` 时 JSON 不发送该字段，由服务端默认为 `true`。

## 错误处理

非 2xx 响应抛出 `ApiException`，并用 `ApiErrors` 谓词判断：

```csharp
try
{
    await client.GetBindingAsync("alice", "role:editor", "");
}
catch (Exception ex) when (ApiErrors.IsNotFound(ex)) { /* 404 */ }
catch (Exception ex) when (ApiErrors.IsConflict(ex)) { /* 409 */ }
catch (Exception ex) when (ApiErrors.IsBadRequest(ex)) { /* 400 */ }
catch (Exception ex) when (ApiErrors.IsPaused(ex)) { /* 503 */ }
```

`ApiException` 属性：`StatusCode`、`Method`、`Path`、`ApiMessage`、`Body`。传输层错误不会包装为 `ApiException`。

## 开发

```bash
cd sdk-csharp
dotnet test
```

## 许可

[MIT](../LICENSE)
