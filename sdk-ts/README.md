# sdk-ts

rbac 授权微服务的官方 TypeScript/JavaScript 客户端 SDK。封装服务端 `/v1` REST API（HTTP / HTTPS），对应 Go 侧的 [`sdk-go`](../sdk-go/)、Rust 侧的 [`sdk-rust`](../sdk-rust/) 和 C# 侧的 [`sdk-csharp`](../sdk-csharp/)。

```typescript
import { Client } from 'rbac-sdk-ts';

const client = new Client('http://localhost:8080');
const allow = await client.enforce('alice', 'doc:42');
```

要求：Node.js 18+。

## 安装

```bash
npm install rbac-sdk-ts
# 或
yarn add rbac-sdk-ts
# 或
pnpm add rbac-sdk-ts
```

## 快速开始

### 基础用法

```typescript
import { Client, CallOptions } from 'rbac-sdk-ts';

const client = new Client('http://localhost:8080');

// 权限判定：检查 alice 是否可以访问 doc:42
const allow = await client.enforce('alice', 'doc:42');
console.log(`允许访问: ${allow}`);

// 带场景与时间点
const allowWithOptions = await client.enforce(
  'alice',
  'doc:42',
  {
    scenarios: ['VIP'],
    now: new Date().toISOString(),
  }
);

// 可达节点（结果包含 subject 自身）
const nodes = await client.reachable('alice', {
  scenarios: ['VIP'],
});
console.log(`可达节点: ${nodes.join(', ')}`);
```

`Client` 可在整个应用中安全复用。默认请求超时 **30 秒**。

### 参数说明

#### enforce 方法

```typescript
async enforce(subject: string, target: string, options?: CallOptions): Promise<boolean>
```

- **`subject`**: 主体标识符，通常是用户 ID、角色名或服务名。例如：`"alice"`、`"user:123"`、`"service:payment"`
- **`target`**: 目标资源标识符，可以是文档、API 端点、功能模块等。例如：`"doc:42"`、`"/api/users"`、`"feature:export"`
- **`options`**: 可选的调用级选项
  - `scenarios`: 场景列表，用于多租户或多环境隔离。例如：`["VIP"]`、`["prod"]`
  - `now`: 判定时间点（ISO 8601 格式），用于时间窗口权限控制。缺省时使用服务端当前时间

返回值：
- **`Promise<boolean>`**: `true` 表示允许访问，`false` 表示拒绝
- 异常：网络错误、服务错误或 `ApiError`（如 4xx/5xx）

#### reachable 方法

```typescript
async reachable(subject: string, options?: CallOptions): Promise<string[]>
```

- **`subject`**: 主体标识符（同上）
- **`options`**: 可选的调用级选项（同上）

返回值：
- **`Promise<string[]>`**: 从 subject 出发可达的所有节点列表（包含 subject 自身）。例如：`["alice", "role:editor", "role:viewer"]`

## HTTPS 与自签 CA

服务端 HTTPS 使用启动时自动生成的**自签 CA**。SDK 提供五种信任方式：

> **注意**：`baseUrl` 的主机名必须与证书 SAN（Subject Alternative Name）一致。例如证书签发的是 `localhost`，则不能用 `https://127.0.0.1:8443` 访问，反之亦然。

### 方式一：信任 CA 证书（PEM 字符串）

适用于从环境变量、配置中心或其他渠道获取 CA 证书的场景。

```typescript
// caPem 是 PEM 编码的 CA 证书字符串
// 可以从文件读取、环境变量获取，或从服务端下载
const caPem = fs.readFileSync('secret/ca.crt', 'utf-8');

const client = new Client('https://localhost:8443', {
  caCert: caPem,
});
```

**CA 证书来源**：
- 从服务端启动目录的 `secret/ca.crt` 文件获取
- 通过服务端 `/v1/ca-cert` 端点下载（见方式三）
- 从配置管理系统或密钥管理服务获取

### 方式二：从文件读取 CA 证书

适用于 CA 证书已存储在本地文件的场景。

```typescript
// 直接指定 CA 证书文件路径
const client = new Client('https://localhost:8443', {
  caCertFile: 'secret/ca.crt',
});
```

### 方式三：自动管理 CA 证书（推荐生产环境）

SDK 会自动检查本地路径是否存在 CA 证书；如果缺失或为空，会从服务端 `/v1/ca-cert` 端点下载并缓存到本地。下载失败会重试一次，两次都失败则返回错误。

```typescript
// 首次运行时会从服务端下载 CA 证书并缓存到指定路径
// 后续运行直接从本地读取，无需网络连接
const client = new Client('https://localhost:8443', {
  caCertPath: '/path/to/cache/ca.pem',
});
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

```typescript
const client = new Client('https://localhost:8443', {
  insecureSkipVerify: true,
});
```

### 方式五：自带 fetch 实现

适用于需要自定义 TLS 配置、代理、连接池等高级场景。设置后 TLS 相关选项会被忽略。

```typescript
// 自定义 fetch 实现
const customFetch = (url: string, init?: RequestInit) => {
  // 你的自定义 fetch 逻辑
  return fetch(url, init);
};

const client = new Client('https://localhost:8443', {
  fetch: customFetch,
});
```

## 构造选项

| 选项 | 说明 | 适用场景 |
| --- | --- | --- |
| `fetch?: typeof fetch` | 自定义 fetch 实现。默认为全局 fetch。 | 需要自定义代理、TLS 配置等 |
| `timeout?: number` | 请求超时毫秒数（默认 30000）。 | 根据业务需求调整超时时间 |
| `userAgent?: string` | 覆盖默认 User-Agent（默认 `rbac-sdk-ts/<version>`）。 | 需要自定义请求头标识 |
| `caCert?: string` | 信任自签 CA（PEM 字符串）；仅对 `https://` 生效，`http://` 下抛错。 | CA 证书已从其他渠道获取 |
| `caCertFile?: string` | 从文件读取 CA；仅对 `https://` 生效。 | CA 证书存储在本地文件 |
| `caCertPath?: string` | **自动管理 CA 证书**：检查本地路径是否存在，缺失时从服务端 `/v1/ca-cert` 下载并缓存；下载失败重试一次。 | 生产环境推荐，自动化证书管理 |
| `insecureSkipVerify?: boolean` | 跳过 TLS 校验（仅测试）；仅对 `https://` 生效。 | 开发/测试环境快速调试 |

调用级选项（用于 `enforce` / `reachable`）：

| 选项 | 说明 |
| --- | --- |
| `scenarios?: string[]` | 场景列表，用于多租户或多环境隔离 |
| `now?: string` | 判定时间点（ISO 8601，仅 Enforce；缺省为服务端当前时间） |

## API

### 读路径

```typescript
// health 检查服务存活状态
await client.health();

// enforce 判定 subject 是否有权访问 target
// 参数：
//   - subject: 主体标识符（用户 ID、角色名、服务名等）
//   - target: 目标资源标识符（文档、API、功能模块等）
//   - options: 可选的调用级选项（场景列表、判定时间点）
// 返回：true 表示允许，false 表示拒绝
async enforce(subject: string, target: string, options?: CallOptions): Promise<boolean>;

// reachable 列出从 subject 出发可达的所有节点（包含 subject 自身）
// 参数：
//   - subject: 主体标识符
//   - options: 可选的调用级选项（场景列表）
// 返回：可达节点列表，如 ["alice", "role:editor", "role:viewer"]
async reachable(subject: string, options?: CallOptions): Promise<string[]>;
```

### 绑定管理

```typescript
// listBindings 列出所有绑定关系
await client.listBindings();

// getBinding 获取单个绑定关系
// 参数：src=源节点, dst=目标节点, scenario=场景（空字符串表示默认场景）
await client.getBinding(src, dst, scenario);

// addBinding 创建新的绑定关系
// 如果绑定已存在，抛出 IsConflict 错误
await client.addBinding(binding);

// updateBinding 更新现有绑定关系
// 如果绑定不存在，抛出 IsNotFound 错误
await client.updateBinding(binding);

// setEnabled 启用或禁用绑定关系
await client.setEnabled(src, dst, scenario, enabled);

// removeBinding 删除绑定关系
// 如果绑定不存在，抛出 IsNotFound 错误
await client.removeBinding(src, dst, scenario);
```

## 数据类型

```typescript
interface Binding {
  src: string;          // 源节点（主体）
  dst: string;          // 目标节点（资源或角色）
  scenario: string;     // 场景标识（空字符串表示默认场景）
  enabled?: boolean;    // 是否启用；null/undefined 时 JSON 省略，服务端默认为 true
  conditions?: Condition[];  // 条件列表（无条件时为空数组）
}

interface Condition {
  kind: ConditionKind;  // 'ALL' 或 'TIME'
  start?: string;       // ISO 8601 时间，半开区间 [start, end)；undefined 表示无界
  end?: string;         // ISO 8601 时间
}

enum ConditionKind {
  All = 'ALL',   // 无条件恒真
  Time = 'TIME', // 时间窗口
}
```

### 构造条件

```typescript
import { ConditionKind } from 'rbac-sdk-ts';

// 无条件恒真（任何情况下都生效）
const allCondition = { kind: ConditionKind.All };

// 时间窗口：半开区间 [start, end)
// start 和 end 可以为 undefined，表示单侧无界
const timeRange = {
  kind: ConditionKind.Time,
  start: '2026-06-01T00:00:00Z',
  end: '2026-07-01T00:00:00Z',
};

const startOnly = {
  kind: ConditionKind.Time,
  start: '2026-06-01T00:00:00Z',
  // end undefined: 从 start 开始永久有效
};

const endOnly = {
  kind: ConditionKind.Time,
  // start undefined: 在 end 之前有效
  end: '2026-07-01T00:00:00Z',
};
```

### 使用示例

新增带时间窗口的边：

```typescript
await client.addBinding({
  src: 'alice',
  dst: 'role:editor',
  scenario: '',
  enabled: true,
  conditions: [
    {
      kind: ConditionKind.Time,
      start: '2026-06-01T00:00:00Z',
      end: '2026-07-01T00:00:00Z',
    },
  ],
});
```

创建无条件绑定：

```typescript
await client.addBinding({
  src: 'bob',
  dst: 'role:viewer',
  scenario: '',
  conditions: [{ kind: ConditionKind.All }],
});
```

> **时间精度**：服务端时间为 RFC3339 **秒级精度**。
>
> **enabled 字段**：可选字段。JSON 中省略时，服务端默认为 `true`。

## 错误处理

非 2xx 响应抛出 `ApiError`，并提供状态码谓词：

```typescript
import { ApiErrors } from 'rbac-sdk-ts';

try {
  const binding = await client.getBinding('alice', 'role:editor', '');
  console.log(`绑定存在: ${binding}`);
} catch (error) {
  if (ApiErrors.isNotFound(error)) {
    console.log('绑定不存在');
  } else if (ApiErrors.isConflict(error)) {
    console.log('绑定已存在');
  } else if (ApiErrors.isBadRequest(error)) {
    console.log(`参数错误: ${(error as any).message}`);
  } else if (ApiErrors.isPaused(error)) {
    console.log('服务暂停');
  } else {
    console.log(`其他错误: ${(error as any).message}`);  // 网络/TLS/超时等
  }
}
```

`ApiError` 属性：
- **`statusCode`**: HTTP 状态码（400、404、409、503 等）
- **`method`**: HTTP 方法（GET、POST、PUT、DELETE 等）
- **`path`**: 请求路径（如 `/v1/bindings`）
- **`message`**: 错误消息（来自服务端 `{"error":...}` 或 `{"reason":...}`）
- **`body`**: 原始响应体字符串

传输层错误不会包装为 `ApiError`，直接抛出原始错误。

### 完整示例

```typescript
import { Client, ConditionKind } from 'rbac-sdk-ts';
import { ApiErrors } from 'rbac-sdk-ts';

async function main() {
  // 创建客户端（自动管理 CA 证书）
  const client = new Client('https://localhost:8443', {
    caCertPath: '/tmp/rbac-ca.pem',
    timeout: 10000, // 10 秒
  });

  // 检查健康状态
  await client.health();

  // 权限判定
  const allow = await client.enforce('alice', 'doc:42', {
    scenarios: ['VIP'],
  });
  console.log(`允许访问: ${allow}`);

  // 创建带时间窗口的绑定
  try {
    await client.addBinding({
      src: 'alice',
      dst: 'role:editor',
      scenario: '',
      enabled: true,
      conditions: [
        {
          kind: ConditionKind.Time,
          start: '2026-06-01T00:00:00Z',
          end: '2026-07-01T00:00:00Z',
        },
      ],
    });
    console.log('绑定创建成功');
  } catch (error) {
    if (ApiErrors.isConflict(error)) {
      console.log('绑定已存在');
    } else {
      throw error;
    }
  }

  // 查询可达节点
  const nodes = await client.reachable('alice', {
    scenarios: ['VIP'],
  });
  console.log(`可达节点: ${nodes.join(', ')}`);
}

main().catch(console.error);
```

## 开发

```bash
cd sdk-ts
npm install
npm run build
npm test
```

## 许可

[MIT](../LICENSE)
