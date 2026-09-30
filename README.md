# rbac

一个用 Go 编写的轻量级 RBAC（基于关系的访问控制）授权系统，以**有向图**建模权限：每条边表示一个主体到目标的授权关系，支持时间窗口、场景等条件，权限判定即图的可达性查询。

本仓库是一个 monorepo，包含相互独立的服务端与客户端 SDK：

| 目录 | 说明 |
| --- | --- |
| [`kernal/`](kernal/) | Go module `github.com/aid297/rbac/kernal`：RBAC 内核（`rbac/` 可库引用）+ HTTP/gRPC/Admin 服务、持久化、Redis、加密、自签 CA |
| [`sdk-go/`](sdk-go/) | Go module `github.com/aid297/rbac/sdk-go`：对接 `/v1` 的官方 Go 客户端（HTTP/HTTPS + gRPC/gRPC+TLS） |
| [`sdk-rust/`](sdk-rust/) | Rust crate `rbac-sdk-rs`（库名 `rbac`）：对接同一 `/v1` API 的官方 Rust 客户端（HTTP/HTTPS + gRPC/gRPC+TLS） |
| [`sdk-csharp/`](sdk-csharp/) | .NET 包 `Rbac.Sdk.Cs`（命名空间 `Rbac`）：对接同一 `/v1` API 的官方 C# 客户端（HTTP/HTTPS + gRPC/gRPC+TLS） |
| [`sdk-ts/`](sdk-ts/) | npm 包 `rbac-sdk-ts`：对接同一 `/v1` API 的官方 TypeScript/JavaScript 客户端（HTTP/HTTPS + gRPC/gRPC+TLS） |
| [`kernal/api/`](kernal/api/) | **公开契约**：[`openapi.yaml`](kernal/api/openapi.yaml)（HTTP JSON）与 [`proto/rbac/v1/rbac.proto`](kernal/api/proto/rbac/v1/rbac.proto)（gRPC），供第三方代码生成与其它语言集成 |

设计文档见 [`docs/superpowers/specs/`](docs/superpowers/specs/)。

未使用官方 SDK 时，见 [`kernal/api/README.md`](kernal/api/README.md)（OpenAPI Generator、protoc 示例与 raw URL）。**改 API 或给 AI/贡献者看整体约定**：[`docs/INTEGRATION.md`](docs/INTEGRATION.md)。

## 快速开始

启动服务端（详见 [`kernal/README.md`](kernal/README.md)）：

```bash
cd kernal
go build -o rbac-server ./cmd/rbac
./rbac-server --config config.yaml   # 默认所有服务关闭，需在配置中开启 http/https/admin
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

在 TypeScript/JavaScript 程序中用 SDK 接入（详见 [`sdk-ts/README.md`](sdk-ts/README.md)）：

```typescript
import { Client } from 'rbac-sdk-ts';

const client = new Client('http://localhost:8080');
const allow = await client.enforce('alice', 'doc:42');
```

## 开发

```bash
# 服务端
cd kernal && go test -race ./...

# Go SDK
cd sdk-go && go test -race ./...

# Rust SDK
cd sdk-rust && cargo test

# C# SDK
cd sdk-csharp && dotnet test

# TypeScript SDK
cd sdk-ts && npm test
```

## 许可证

[MIT](LICENSE)
