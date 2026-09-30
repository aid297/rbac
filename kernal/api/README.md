# Public API contracts

Machine-readable definitions for integrating with the RBAC service **without** using an official SDK.

| Artifact | Path | Use for |
| --- | --- | --- |
| **OpenAPI 3** | [`openapi.yaml`](openapi.yaml) | HTTP/HTTPS JSON clients, REST code generators, Swagger UI |
| **Protobuf** | [`proto/rbac/v1/rbac.proto`](proto/rbac/v1/rbac.proto) | gRPC / gRPC+TLS clients (`RbacService`) |

Both describe the same **public** surface: health, CA export, enforce/reachable, and binding CRUD. They do **not** cover the Basic Auth admin preview (`/api/*`).

## Stable URLs (GitHub raw)

Replace `main` with a release tag (e.g. `kernal/v1.2.0`) to pin a contract version.

```
https://raw.githubusercontent.com/aid297/rbac/main/kernal/api/openapi.yaml
https://raw.githubusercontent.com/aid297/rbac/main/kernal/api/proto/rbac/v1/rbac.proto
```

## HTTP (OpenAPI)

- **Auth**: none on `/v1/*` (secure the network or put a gateway in front).
- **Pause**: except `GET /healthz`, paused storage → **503** JSON `{"error":"service paused","reason":"..."}`.
- **Bodies**: max **1 MiB**; unknown JSON fields → **400**.

### Generate a REST client (examples)

[OpenAPI Generator](https://openapi-generator.tech/) (Java, Python, PHP, etc.):

```bash
openapi-generator-cli generate \
  -i openapi.yaml \
  -g python \
  -o /tmp/rbac-python-client \
  --package-name rbac_client
```

[Speakeasy](https://www.speakeasy.com/), [Kiota](https://learn.microsoft.com/en-us/openapi/kiota/), and other OpenAPI tools accept the same file.

### Browse interactively

```bash
npx --yes @redocly/cli preview-docs kernal/api/openapi.yaml
# or: docker run -p 8080:8080 -e SWAGGER_JSON=/openapi.yaml -v "$PWD/kernal/api:/openapi" swaggerapi/swagger-ui
```

## gRPC (Protobuf)

Service: **`rbac.v1.RbacService`**. RPC names and HTTP routes are documented in comments inside the `.proto` file.

### Generate stubs

Requires `protoc` plus language plugins. Import path for well-known types:

```bash
cd kernal/api
protoc -I proto \
  --python_out=/tmp/rbac_pb --grpc_python_out=/tmp/rbac_pb \
  proto/rbac/v1/rbac.proto
```

Go (already generated in-repo under `kernal/api/gen`):

```bash
cd kernal && make proto
```

### Error semantics (gRPC)

| Application condition | gRPC code | HTTP equivalent |
| --- | --- | --- |
| Service paused (health / calls) | `FAILED_PRECONDITION` | 503 |
| Binding missing | `NOT_FOUND` | 404 |
| Duplicate binding | `ALREADY_EXISTS` | 409 |
| Invalid argument / policy | `INVALID_ARGUMENT` | 400 |
| Network / server down | `UNAVAILABLE` | — (not “paused”) |

Official SDKs map these the same way; generated clients should treat **`FAILED_PRECONDITION` on Health** as pause, not **`UNAVAILABLE`**.

## Official SDKs vs generated clients

Maintained in this monorepo (HTTP + gRPC, shared semantics):

- [`sdk-go`](../../sdk-go/)
- [`sdk-ts`](../../sdk-ts/)
- [`sdk-rust`](../../sdk-rust/)
- [`sdk-csharp`](../../sdk-csharp/)

For other languages, prefer **OpenAPI → REST** and/or **proto → gRPC** from this directory. If generated behavior diverges (timeouts, pause detection, timestamp truncation to seconds), align with the official SDKs or the kernal implementation in `kernal/http-server` and `kernal/grpc-server`.

Monorepo change checklist (openapi ↔ handlers ↔ proto ↔ SDKs): [`docs/INTEGRATION.md`](../../docs/INTEGRATION.md).

## Versioning

- **OpenAPI** `info.version` tracks the **document** revision (bump when the HTTP contract changes).
- **Proto** uses protobuf wire compatibility; breaking RPC or field changes should be rare and documented in release notes.
- Pin integration tests to a **git tag** or commit SHA of these files.
