# sdk-rust

rbac 授权微服务的官方 Rust 客户端 SDK。封装服务端 `/v1` REST API（HTTP / HTTPS）与 gRPC，对应 Go 侧的 [`sdk-go`](../sdk-go/)。

```rust
use rbac::{CallOpts, Client};

let client = Client::new("http://localhost:8080")?;
let allow = client.enforce("alice", "doc:42", CallOpts::default())?;
```

要求：Rust **1.75+**（edition 2021）。crate 名 `rbac-sdk-rs`，库名 `rbac`。HTTP 与 gRPC 共用同一套方法；gRPC 依赖 `tonic` / `prost`（proto 已 vendored 在 `proto/`，`build.rs` 生成桩，无需兄弟目录 `kernal/`）。

## 安装

```toml
[dependencies]
rbac-sdk-rs = { path = "../sdk-rust" }   # 本地 monorepo
# 或 crates.io：rbac-sdk-rs = "0.1"
```

## 快速开始

### 基础用法

```rust
use rbac::{CallOpts, Client, Error};

fn main() -> Result<(), Error> {
    // 创建客户端
    let client = Client::new("http://localhost:8080")?;

    // 权限判定：检查 alice 是否可以访问 doc:42
    let allow = client.enforce("alice", "doc:42", CallOpts::default())?;
    println!("允许访问: {}", allow);

    // 带场景与时间点
    let allow = client.enforce(
        "alice",
        "doc:42",
        CallOpts::new()
            .with_scenarios(["VIP"])
            .with_now(chrono::Utc::now()),
    )?;

    // 可达节点（结果包含 subject 自身）
    let nodes = client.reachable("alice", CallOpts::new().with_scenarios(["VIP"]))?;
    println!("可达节点: {:?}", nodes);

    Ok(())
}
```

`Client` 内部使用 `Arc`，可在多线程间克隆复用。默认请求超时 **30 秒**。gRPC 客户端用完后可调用 `close()`（HTTP 为 no-op）。

### gRPC

与 HTTP 共用同一套方法；用 `Client::new_grpc` 连接服务端 `server.grpc` / `server.grpc_tls` 端口：

```rust
// 明文 gRPC（server.grpc.port，默认 9080）
let client = Client::new_grpc("localhost:9080")?;

// gRPC + TLS（server.grpc_tls.port，默认 9443），信任自签 CA
let client = Client::grpc_builder("localhost:9443")?
    .ca_cert_file("secret/ca.crt")?
    .build()?;

// 或跳过校验（仅测试）
let client = Client::grpc_builder("localhost:9443")?
    .insecure_skip_verify(true)
    .build()?;
```

- `target` 为 `host:port`，不要写 `http://` / `grpc://`。
- 未配置 CA / `insecure_skip_verify` 时使用**明文**传输（对应服务端 `grpc.enable`）。
- 配置了 `ca_cert*` 或 `insecure_skip_verify(true)` 时走 **TLS**（对应 `grpc_tls`）。
- `http_client` / `ca_cert_path` 仅适用于 HTTP 构造。
- **拨号时机**：`build()` 立即建立连接，失败报 `Error::Config`（与 Go 的首次调用惰性拨号略有不同）。

gRPC 业务错误映射为与 HTTP 相同的 `Error::Api` / `is_not_found` 等谓词。服务暂停为 `FailedPrecondition` → 503（`is_paused`）；传输层 `Unavailable` 等保留为 `Error::Grpc`，不会被 `is_paused` 命中。可用 `Error::is_grpc` 区分。

### 参数说明

#### enforce 方法

```rust
pub fn enforce(&self, subject: &str, target: &str, opts: CallOpts) -> Result<bool, Error>
```

- **`subject`**: 主体标识符，通常是用户 ID、角色名或服务名。例如：`"alice"`、`"user:123"`、`"service:payment"`
- **`target`**: 目标资源标识符，可以是文档、API 端点、功能模块等。例如：`"doc:42"`、`"/api/users"`、`"feature:export"`
- **`opts`**: 调用级选项
  - `with_scenarios([...])`: 场景列表，用于多租户或多环境隔离。例如：`["VIP"]`、`["prod"]`
  - `with_now(DateTime<Utc>)`: 判定时间点，用于时间窗口权限控制。缺省时使用服务端当前时间

返回值：
- **`Result<bool, Error>`**: `Ok(true)` 表示允许访问，`Ok(false)` 表示拒绝；错误包含网络错误、服务错误或 API 错误

#### reachable 方法

```rust
pub fn reachable(&self, subject: &str, opts: CallOpts) -> Result<Vec<String>, Error>
```

- **`subject`**: 主体标识符（同上）
- **`opts`**: 调用级选项（同上）

返回值：
- **`Result<Vec<String>, Error>`**: 从 subject 出发可达的所有节点列表（包含 subject 自身）。例如：`["alice", "role:editor", "role:viewer"]`

## HTTPS 与自签 CA

服务端 HTTPS 使用启动时自动生成的**自签 CA**。SDK 提供五种信任方式：

> **注意**：`base_url` 的主机名必须与证书 SAN（Subject Alternative Name）一致。例如证书签发的是 `localhost`，则不能用 `https://127.0.0.1:8443` 访问，反之亦然。

### 方式一：信任 CA 证书（PEM 字节）

适用于从环境变量、配置中心或其他渠道获取 CA 证书的场景。

```rust
// ca_pem 是 PEM 编码的 CA 证书字节数组
// 可以从文件读取、环境变量获取，或从服务端下载
let ca_pem = std::fs::read("secret/ca.crt")?;

let client = Client::builder("https://localhost:8443")?
    .ca_cert(ca_pem)?
    .build()?;
```

**CA 证书来源**：
- 从服务端启动目录的 `secret/ca.crt` 文件获取
- 通过服务端 `/v1/ca-cert` 端点下载（见方式三）
- 从配置管理系统或密钥管理服务获取

### 方式二：从文件读取 CA 证书

适用于 CA 证书已存储在本地文件的场景。

```rust
// 直接指定 CA 证书文件路径
let client = Client::builder("https://localhost:8443")?
    .ca_cert_file("secret/ca.crt")?
    .build()?;
```

### 方式三：自动管理 CA 证书（推荐生产环境）

SDK 会自动检查本地路径是否存在 CA 证书；如果缺失或为空，会从服务端 `/v1/ca-cert` 端点下载并缓存到本地。下载失败会重试一次，两次都失败则返回错误。

```rust
// 首次运行时会从服务端下载 CA 证书并缓存到指定路径
// 后续运行直接从本地读取，无需网络连接
let client = Client::builder("https://localhost:8443")?
    .ca_cert_path("/path/to/cache/ca.pem")?
    .build()?;
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

```rust
let client = Client::builder("https://localhost:8443")?
    .insecure_skip_verify(true)
    .build()?;
```

### 方式五：自带 reqwest::blocking::Client

适用于需要自定义 TLS 配置、代理、连接池等高级场景。设置后 TLS 相关选项和 `timeout` 会被忽略。

```rust
// 完全自定义 HTTP 客户端
let my_http_client = reqwest::blocking::Client::builder()
    .timeout(std::time::Duration::from_secs(60))
    .build()?;

let client = Client::builder("https://localhost:8443")?
    .http_client(my_http_client)
    .build()?;
```

## 构造选项

| 方法 | 说明 | 适用场景 |
| --- | --- | --- |
| `http_client(Client)` | 自带 blocking HTTP client；设置后 TLS 与 `timeout` 被忽略 | 需要自定义代理、连接池、TLS 配置等 |
| `ca_cert(pem)` | 信任自签 CA（PEM）；仅对 `https://` 生效，`http://` 下返回错误 | CA 证书已从其他渠道获取 |
| `ca_cert_file(path)` | 从文件读取 CA；仅对 `https://` 生效 | CA 证书存储在本地文件 |
| `ca_cert_path(path)` | **自动管理 CA 证书**：检查本地路径是否存在，缺失时从服务端 `/v1/ca-cert` 下载并缓存；下载失败重试一次 | 生产环境推荐，自动化证书管理 |
| `insecure_skip_verify(bool)` | 跳过 TLS 校验（仅测试）；仅对 `https://` 生效 | 开发/测试环境快速调试 |
| `user_agent(string)` | 覆盖默认 User-Agent（默认 `rbac-sdk-rust/<version>`） | 需要自定义请求头标识 |
| `timeout(Duration)` | 请求超时（默认 30s）。`Duration::ZERO` 表示不设超时 | 根据业务需求调整超时时间 |

调用级选项（用于 `enforce` / `reachable`）：

| 方法 | 说明 |
| --- | --- |
| `with_scenarios([...])` | 场景列表，用于多租户或多环境隔离 |
| `with_now(DateTime<Utc>)` | 判定时间点（仅 `enforce`；缺省为服务端当前时间） |

## API

### 读路径

```rust
// health 检查服务存活状态
client.health()?;

// enforce 判定 subject 是否有权访问 target
// 参数：
//   - subject: 主体标识符（用户 ID、角色名、服务名等）
//   - target: 目标资源标识符（文档、API、功能模块等）
//   - opts: 调用级选项（场景列表、判定时间点）
// 返回：Ok(true) 表示允许，Ok(false) 表示拒绝
client.enforce(subject: &str, target: &str, opts: CallOpts) -> Result<bool, Error>;

// reachable 列出从 subject 出发可达的所有节点（包含 subject 自身）
// 参数：
//   - subject: 主体标识符
//   - opts: 调用级选项（场景列表）
// 返回：可达节点列表，如 ["alice", "role:editor", "role:viewer"]
client.reachable(subject: &str, opts: CallOpts) -> Result<Vec<String>, Error>;
```

### 绑定管理

```rust
// list_bindings 列出所有绑定关系
client.list_bindings()? -> Result<Vec<Binding>, Error>;

// get_binding 获取单个绑定关系
// 参数：src=源节点, dst=目标节点, scenario=场景（空字符串表示默认场景）
client.get_binding(src: &str, dst: &str, scenario: &str)? -> Result<Binding, Error>;

// add_binding 创建新的绑定关系
// 如果绑定已存在，返回 is_conflict 错误
client.add_binding(binding: &Binding)? -> Result<Binding, Error>;

// update_binding 更新现有绑定关系
// 如果绑定不存在，返回 is_not_found 错误
client.update_binding(binding: &Binding)? -> Result<Binding, Error>;

// set_enabled 启用或禁用绑定关系
client.set_enabled(src: &str, dst: &str, scenario: &str, enabled: bool)? -> Result<(), Error>;

// remove_binding 删除绑定关系
// 如果绑定不存在，返回 is_not_found 错误
client.remove_binding(src: &str, dst: &str, scenario: &str)? -> Result<(), Error>;
```

## 数据类型

```rust
struct Binding {
    src: String,        // 源节点（主体）
    dst: String,        // 目标节点（资源或角色）
    scenario: String,   // 场景标识（空字符串表示默认场景）
    enabled: Option<bool>,  // 是否启用；None 时 JSON 省略该字段，服务端默认为 true
    conditions: Vec<Condition>,  // 条件列表（无条件时为空向量）
}

struct Condition {
    kind: ConditionKind,    // ConditionKind::All ("ALL") 或 ConditionKind::Time ("TIME")
    start: Option<DateTime<Utc>>,  // TIME 半开区间 [start, end)；None 表示无界
    end: Option<DateTime<Utc>>,
}

enum ConditionKind {
    All,   // 无条件恒真
    Time,  // 时间窗口
}
```

### 构造条件

```rust
// 无条件恒真（任何情况下都生效）
rbac::all_condition()

// 时间窗口：半开区间 [start, end)
// start 和 end 可以为 None，表示单侧无界
rbac::time_range(Some(start), Some(end))  // 固定时间窗口
rbac::time_range(Some(start), None)       // 仅有下界（从 start 开始永久有效）
rbac::time_range(None, Some(end))         // 仅有上界（在 end 之前有效）
```

### 使用示例

新增带时间窗口的边：

```rust
use rbac::{all_condition, time_range, Binding};

let start = chrono::Utc.with_ymd_and_hms(2026, 6, 1, 0, 0, 0).unwrap();
let end = chrono::Utc.with_ymd_and_hms(2026, 7, 1, 0, 0, 0).unwrap();

client.add_binding(
    &Binding::new("alice", "role:editor")
        .with_enabled(true)
        .with_conditions(vec![time_range(Some(start), Some(end))]),
)?;
```

创建无条件绑定：

```rust
client.add_binding(
    &Binding::new("bob", "role:viewer")
        .with_conditions(vec![all_condition()]),
)?;
```

> **时间精度**：服务端时间为 RFC3339 **秒级精度**。
>
> **enabled 字段**：`Option<bool>` 类型。`None` 时 JSON 不发送该字段，由服务端默认为 `true`。

## 错误处理

非 2xx 响应转为 `Error::Api(ApiError)`，并提供状态码谓词：

```rust
match client.get_binding("alice", "role:editor", "") {
    Err(e) if e.is_not_found() => {
        println!("绑定不存在");
    }
    Err(e) if e.is_conflict() => {
        println!("绑定已存在");
    }
    Err(e) if e.is_bad_request() => {
        println!("参数错误: {}", e);
    }
    Err(e) if e.is_paused() => {
        println!("服务暂停");
    }
    Err(e) => {
        println!("其他错误: {}", e);  // 网络 / TLS / 超时等
    }
    Ok(b) => {
        println!("绑定存在: {:?}", b);
    }
}
```

`ApiError` 字段：
- **`status_code`**: HTTP 状态码（400、404、409、503 等）
- **`method`**: HTTP 方法（GET、POST、PUT、DELETE 等）
- **`path`**: 请求路径（如 `/v1/bindings`）
- **`message`**: 错误消息（来自服务端 `{"error":...}` 或 `{"reason":...}`）
- **`body`**: 原始响应体字节数组

传输层错误不会包装为 `ApiError`，可直接匹配 `Error::Network` 等变体。

### 完整示例

```rust
use rbac::{time_range, Binding, CallOpts, Client, Error};
use std::time::Duration;

fn main() -> Result<(), Error> {
    // 创建客户端（自动管理 CA 证书）
    let client = Client::builder("https://localhost:8443")?
        .ca_cert_path("/tmp/rbac-ca.pem")?
        .timeout(Duration::from_secs(10))
        .build()?;

    // 检查健康状态
    client.health()?;

    // 权限判定
    let allow = client.enforce(
        "alice",
        "doc:42",
        CallOpts::new().with_scenarios(["VIP"]),
    )?;
    println!("允许访问: {}", allow);

    // 创建带时间窗口的绑定
    let start = chrono::Utc.with_ymd_and_hms(2026, 6, 1, 0, 0, 0).unwrap();
    let end = chrono::Utc.with_ymd_and_hms(2026, 7, 1, 0, 0, 0).unwrap();

    match client.add_binding(
        &Binding::new("alice", "role:editor")
            .with_enabled(true)
            .with_conditions(vec![time_range(Some(start), Some(end))]),
    ) {
        Ok(_) => println!("绑定创建成功"),
        Err(e) if e.is_conflict() => println!("绑定已存在"),
        Err(e) => return Err(e),
    }

    // 查询可达节点
    let nodes = client.reachable("alice", CallOpts::new().with_scenarios(["VIP"]))?;
    println!("可达节点: {:?}", nodes);

    Ok(())
}
```

## 开发

```bash
cd sdk-rust
cargo test
cargo clippy -- -D warnings
```

gRPC 桩已提交在 `src/pb/rbac.v1.rs`（对应 `proto/rbac/v1/rbac.proto`），工程无需 `protoc` 即可构建。在 monorepo 内同步服务端 proto 并重新生成：

```bash
cp ../kernal/api/proto/rbac/v1/rbac.proto proto/rbac/v1/rbac.proto
make proto   # 需要 protoc
```

## 许可

[MIT](../LICENSE)
