# sdk-ts

Official TypeScript/JavaScript client SDK for the rbac authorization microservice `/v1` API. Mirrors the Go SDK ([`sdk-go`](../sdk-go/)), Rust SDK ([`sdk-rust`](../sdk-rust/)), and C# SDK ([`sdk-csharp`](../sdk-csharp/)).

```typescript
import { Client } from 'rbac-sdk-ts';

const client = new Client('http://localhost:8080');
const allow = await client.enforce('alice', 'doc:42');
```

Requirements: Node.js 18+.

## Installation

```bash
npm install rbac-sdk-ts
# or
yarn add rbac-sdk-ts
# or
pnpm add rbac-sdk-ts
```

## Quick Start

```typescript
import { Client, CallOptions } from '@rbac/sdk';

const client = new Client('http://localhost:8080');

// Permission check
const allow = await client.enforce('alice', 'doc:42');

// With scenarios and timestamp
const allowWithOptions = await client.enforce(
  'alice',
  'doc:42',
  {
    scenarios: ['VIP'],
    now: new Date().toISOString(),
  }
);

// Reachable nodes (includes subject itself)
const nodes = await client.reachable('alice', {
  scenarios: ['VIP'],
});
```

The `Client` is safe to reuse across your application. Default request timeout is **30 seconds**.

## API

### Read Operations

```typescript
await client.health();
await client.enforce(subject, target, options?);
await client.reachable(subject, options?);
```

### Binding Management

```typescript
await client.listBindings();
await client.getBinding(src, dst, scenario);
await client.addBinding(binding);
await client.updateBinding(binding);
await client.setEnabled(src, dst, scenario, enabled);
await client.removeBinding(src, dst, scenario);
```

## Data Types

```typescript
interface Binding {
  src: string;
  dst: string;
  scenario: string;
  enabled?: boolean;      // omitted when null/undefined, server defaults to true
  conditions?: Condition[];
}

interface Condition {
  kind: ConditionKind;    // 'ALL' or 'TIME'
  start?: string;         // ISO 8601 datetime, half-open interval [start, end)
  end?: string;           // ISO 8601 datetime
}

enum ConditionKind {
  All = 'ALL',
  Time = 'TIME',
}
```

Creating conditions:

```typescript
import { ConditionKind } from 'rbac-sdk-ts';

const allCondition = { kind: ConditionKind.All };
const timeRange = {
  kind: ConditionKind.Time,
  start: '2026-06-01T00:00:00Z',
  end: '2026-07-01T00:00:00Z',
};
```

Adding a binding with time window:

```typescript
await client.addBinding({
  src: 'alice',
  dst: 'role:editor',
  scenario: '',
  enabled: true,
  conditions: [timeRange],
});
```

> Server time uses RFC3339 **second precision**.
>
> `enabled` is optional: when omitted from JSON, the server defaults to `true`.

## Error Handling

Non-2xx responses throw `ApiError`, with status code predicates:

```typescript
import { ApiErrors } from 'rbac-sdk-ts';

try {
  await client.getBinding('alice', 'role:editor', '');
} catch (error) {
  if (ApiErrors.isNotFound(error)) {
    // 404: binding not found
  } else if (ApiErrors.isConflict(error)) {
    // 409: duplicate binding
  } else if (ApiErrors.isBadRequest(error)) {
    // 400: validation error
  } else if (ApiErrors.isPaused(error)) {
    // 503: service paused
  } else {
    // Network/TLS/timeout errors
  }
}
```

`ApiError` properties: `statusCode`, `method`, `path`, `message`, `body`. Transport-layer errors are thrown as-is, not wrapped in `ApiError`.

## Client Options

| Option | Description |
| --- | --- |
| `fetch?: typeof fetch` | Custom fetch implementation. Defaults to global fetch. |
| `timeout?: number` | Request timeout in milliseconds (default 30000). |
| `userAgent?: string` | Custom User-Agent header (default `rbac-sdk-ts/<version>`). |

Call-level options (for `enforce` / `reachable`):

| Option | Description |
| --- | --- |
| `scenarios?: string[]` | Scenario list |
| `now?: string` | Evaluation timestamp (ISO 8601, Enforce only; defaults to server time) |

## Development

```bash
cd sdk-ts
npm install
npm run build
npm test
```

## License

[MIT](../LICENSE)
