# sdk-rust

rbac 授权微服务的官方 Rust 客户端 SDK。封装服务端 `/v1` REST API（HTTP / HTTPS），对应 Go 侧的 [`sdk-go`](../sdk-go/)。

```rust
use rbac::{CallOpts, Client};

let client = Client::new("http://localhost:8080")?;
let allow = client.enforce("alice", "doc:42", CallOpts::default())?;
```

要求：Rust 1.70+（edition 2021）。crate 名 `rbac-sdk`，库名 `rbac`。

## 安装

```toml
[dependencies]
rbac-sdk = { path = "../sdk-rust" }   # 本地 monorepo
# 或将来：rbac-sdk = "0.1"
```

## 快速开始

```rust
use rbac::{CallOpts, Client, Error};

fn main() -> Result<(), Error> {
    let client = Client::new("http://localhost:8080")?;

    // 权限判定
    let allow = client.enforce("alice", "doc:42", CallOpts::default())?;

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
    let _ = (allow, nodes);
    Ok(())
}
```

`Client` 内部使用 `Arc`，可在多线程间克隆复用。默认请求超时 **30 秒**。

## HTTPS 与自签 CA

服务端 HTTPS 使用启动时自动生成的**自签 CA**。SDK 提供四种信任方式：

> **注意**：`base_url` 的主机名必须与证书 SAN 一致。例如证书签发的是 `localhost`，则不能用 `https://127.0.0.1:8443` 访问，反之亦然。

```rust
// 方式一：信任 CA 证书（PEM 字节）
let client = Client::builder("https://localhost:8443")?
    .ca_cert(ca_pem)?
    .build()?;

// 方式二：从文件读取 CA 证书（通常是 server 的 secret/ca.crt）
let client = Client::builder("https://localhost:8443")?
    .ca_cert_file("secret/ca.crt")?
    .build()?;

// 方式三：自动管理 CA 证书（推荐生产环境）
// SDK 会检查本地路径是否存在 CA 证书；如果缺失或为空，自动从服务端 /v1/ca-cert 下载并缓存
// 下载失败会重试一次，两次都失败则返回错误
let client = Client::builder("https://localhost:8443")?
    .ca_cert_path("/path/to/cache/ca.pem")?
    .build()?;

// 方式四：跳过证书校验（仅建议测试使用）
let client = Client::builder("https://localhost:8443")?
    .insecure_skip_verify(true)
    .build()?;

// 方式五：自带 reqwest::blocking::Client（TLS 由你自行配置；此时 TLS / timeout 选项被忽略）
let client = Client::builder("https://localhost:8443")?
    .http_client(my_http_client)
    .build()?;
```

## 构造选项

| 方法 | 说明 |
| --- | --- |
| `http_client(Client)` | 自带 blocking HTTP client；设置后 TLS 与 `timeout` 被忽略 |
| `ca_cert(pem)` | 信任自签 CA（PEM）；仅对 `https://` 生效，`http://` 下返回错误 |
| `ca_cert_file(path)` | 从文件读取 CA；仅对 `https://` 生效 |
| `ca_cert_path(path)` | **自动管理 CA 证书**：检查本地路径是否存在，缺失时从服务端 `/v1/ca-cert` 下载并缓存；下载失败重试一次 |
| `insecure_skip_verify(bool)` | 跳过 TLS 校验（仅测试）；仅对 `https://` 生效 |
| `user_agent(string)` | 覆盖默认 User-Agent（默认 `rbac-sdk-rust/<version>`） |
| `timeout(Duration)` | 请求超时（默认 30s） |

调用级选项（用于 `enforce` / `reachable`）：

| 方法 | 说明 |
| --- | --- |
| `with_scenarios([...])` | 场景列表 |
| `with_now(DateTime<Utc>)` | 判定时间点（仅 `enforce`；缺省为服务端当前时间） |

## API

### 读路径

```rust
client.health()?;
client.enforce(subject, target, CallOpts)?;
client.reachable(subject, CallOpts)?;
```

### 绑定管理

```rust
client.list_bindings()?;
client.get_binding(src, dst, scenario)?;
client.add_binding(&binding)?;
client.update_binding(&binding)?;
client.set_enabled(src, dst, scenario, enabled)?;
client.remove_binding(src, dst, scenario)?;
```

## 数据类型

```rust
Binding {
    src, dst, scenario,
    enabled: Option<bool>,  // None 时 JSON 省略，服务端默认为 true
    conditions: Vec<Condition>,
}

Condition {
    kind: ConditionKind::All | ConditionKind::Time,
    start: Option<DateTime<Utc>>,  // TIME 半开区间 [start, end)
    end: Option<DateTime<Utc>>,
}
```

构造条件：

```rust
rbac::all_condition();
rbac::time_range(Some(start), Some(end));
rbac::time_range(Some(start), None);
```

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

> 服务端时间为 RFC3339 **秒级精度**。
>
> `enabled` 为 `Option<bool>`：`None` 时 JSON 不发送该字段，由服务端默认为 `true`。

## 错误处理

非 2xx 响应转为 `Error::Api(ApiError)`，并提供状态码谓词：

```rust
match client.get_binding("alice", "role:editor", "") {
    Err(e) if e.is_not_found() => { /* 404 */ }
    Err(e) if e.is_conflict() => { /* 409 */ }
    Err(e) if e.is_bad_request() => { /* 400 */ }
    Err(e) if e.is_paused() => { /* 503 */ }
    Err(e) => { /* 网络 / TLS / 超时等 */ }
    Ok(b) => { /* ... */ }
}
```

`ApiError` 字段：`status_code`、`method`、`path`、`message`、`body`。传输层错误不会包装为 `ApiError`。

## 开发

```bash
cd sdk-rust
cargo test
cargo clippy -- -D warnings
```

## 许可

[MIT](../LICENSE)
