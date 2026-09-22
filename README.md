# rbac

一个用 Go 编写的轻量级 RBAC（基于关系的访问控制）授权系统，以**有向图**建模权限：每条边表示一个主体到目标的授权关系，支持时间窗口、场景等条件，权限判定即图的可达性查询。

本仓库是一个 monorepo，包含相互独立的服务端与客户端 SDK：

| 目录 | 说明 |
| --- | --- |
| [`server/`](server/) | Go module `github.com/aid297/rbac/server`：HTTP / HTTPS REST API、管理预览页、文件持久化、可选 Redis 缓存、可选静态加密、自签 CA |
| [`sdk-go/`](sdk-go/) | Go module `github.com/aid297/rbac/sdk-go`：对接 `/v1` API 的官方 Go 客户端（仅依赖标准库） |
| [`sdk-rust/`](sdk-rust/) | Rust crate `rbac-sdk`（库名 `rbac`）：对接同一 `/v1` API 的官方 Rust 客户端 |
| [`sdk-csharp/`](sdk-csharp/) | .NET 包 `Rbac.Sdk`（命名空间 `Rbac`）：对接同一 `/v1` API 的官方 C# 客户端 |

设计文档见 [`docs/superpowers/specs/`](docs/superpowers/specs/)。

## 快速开始

启动服务端（详见 [`server/README.md`](server/README.md)）：

```bash
cd server
go build -o rbac ./cmd/rbac
./rbac --config config.yaml   # 默认所有服务关闭，需在配置中开启 http/https/admin
```

在 Go 程序中用 SDK 接入（详见 [`sdk-go/README.md`](sdk-go/README.md)）：

```go
import rbac "github.com/aid297/rbac/sdk-go"

client, err := rbac.NewClient("http://localhost:8080")
if err != nil {
    log.Fatal(err)
}
allow, err := client.Enforce(ctx, "alice", "doc:42")
```

在 Rust 程序中用 SDK 接入（详见 [`sdk-rust/README.md`](sdk-rust/README.md)）：

```rust
use rbac::{CallOpts, Client};

let client = Client::new("http://localhost:8080")?;
let allow = client.enforce("alice", "doc:42", CallOpts::default())?;
```

在 C# 程序中用 SDK 接入（详见 [`sdk-csharp/README.md`](sdk-csharp/README.md)）：

```csharp
using Rbac;

using var client = Client.Create("http://localhost:8080");
var allow = await client.EnforceAsync("alice", "doc:42");
```

## 开发

```bash
# 服务端
cd server && go test -race ./...

# Go SDK
cd sdk-go && go test -race ./...

# Rust SDK
cd sdk-rust && cargo test

# C# SDK
cd sdk-csharp && dotnet test
```

## 许可证

[MIT](LICENSE)
