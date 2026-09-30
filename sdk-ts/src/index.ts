/**
 * Official TypeScript/JavaScript client SDK for the rbac authorization microservice /v1 API.
 *
 * Mirrors the Go SDK (`sdk-go`): HTTP/HTTPS + gRPC/gRPC+TLS, self-signed CA trust,
 * binding CRUD, Enforce / Reachable, and typed API errors.
 *
 * @example HTTP
 * ```typescript
 * import { Client } from 'rbac-sdk-ts';
 *
 * const client = new Client('http://localhost:8080');
 * const allow = await client.enforce('alice', 'doc:42');
 * ```
 *
 * @example gRPC
 * ```typescript
 * import { GrpcClient } from 'rbac-sdk-ts';
 *
 * const client = new GrpcClient('localhost:9080');
 * const allow = await client.enforce('alice', 'doc:42');
 * await client.close();
 * ```
 */

export { Client, type ClientOptions } from './client';
export { GrpcClient, type GrpcClientOptions } from './grpc-client';
export { ApiError, ConfigError, ClosedError, ApiErrors } from './errors';
export { mapGrpcError, isGrpcError, grpcCodeToHttp } from './grpc-errors';
export * from './types';
export { VERSION } from './version';
