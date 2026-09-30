/**
 * gRPC client for the rbac microservice — mirrors the HTTP `Client` API.
 *
 * Uses `@grpc/grpc-js` with `@grpc/proto-loader` for dynamic proto loading.
 * The proto file is vendored at `proto/rbac/v1/rbac.proto` (no `protoc` needed).
 *
 * @example
 * ```typescript
 * import { GrpcClient } from 'rbac-sdk-ts';
 *
 * // Plaintext (server.grpc, default 9080)
 * const client = new GrpcClient('localhost:9080');
 * const allow = await client.enforce('alice', 'doc:42');
 * await client.close();
 * ```
 */

import { join, resolve } from 'node:path';
import {
  ChannelCredentials,
  Client as GrpcClientCore,
  Metadata,
  loadPackageDefinition,
  status as GrpcStatus,
  type ClientUnaryCall,
  type ServiceClientConstructor,
} from '@grpc/grpc-js';
import { loadSync } from '@grpc/proto-loader';
import { ApiError, ConfigError, ClosedError } from './errors';
import { mapGrpcError } from './grpc-errors';
import {
  Binding,
  CallOptions,
  EnforceResponse,
  HealthResponse,
  ReachableResponse,
  BindingsResponse,
} from './types';
import { VERSION } from './version';

const PROTO_PATH = resolve(__dirname, '..', 'proto', 'rbac', 'v1', 'rbac.proto');
const DEFAULT_TIMEOUT = 30000; // 30 seconds, same as HTTP

/** Options for creating a `GrpcClient`. */
export interface GrpcClientOptions {
  /** Timeout in milliseconds for each RPC. Defaults to 30000. `0` means no timeout. */
  timeout?: number;
  /** Custom User-Agent metadata. */
  userAgent?: string;
  /** PEM-encoded CA certificate bytes (enables TLS / `grpc_tls`). */
  caCert?: Buffer | string;
  /** Path to a PEM CA certificate file. */
  caCertFile?: string;
  /** Disable TLS certificate verification (testing only; enables TLS). */
  insecureSkipVerify?: boolean;
}

interface RbacServiceClient extends GrpcClientCore {
  Health(
    req: Record<string, unknown>,
    metadata: Metadata,
    options: Record<string, unknown>,
    cb: (err: unknown, resp: HealthResponse) => void
  ): ClientUnaryCall;
  Enforce(
    req: Record<string, unknown>,
    metadata: Metadata,
    options: Record<string, unknown>,
    cb: (err: unknown, resp: EnforceResponse) => void
  ): ClientUnaryCall;
  Reachable(
    req: Record<string, unknown>,
    metadata: Metadata,
    options: Record<string, unknown>,
    cb: (err: unknown, resp: ReachableResponse) => void
  ): ClientUnaryCall;
  ListBindings(
    req: Record<string, unknown>,
    metadata: Metadata,
    options: Record<string, unknown>,
    cb: (err: unknown, resp: BindingsResponse) => void
  ): ClientUnaryCall;
  GetBinding(
    req: Record<string, unknown>,
    metadata: Metadata,
    options: Record<string, unknown>,
    cb: (err: unknown, resp: Binding) => void
  ): ClientUnaryCall;
  AddBinding(
    req: Record<string, unknown>,
    metadata: Metadata,
    options: Record<string, unknown>,
    cb: (err: unknown, resp: Binding) => void
  ): ClientUnaryCall;
  UpdateBinding(
    req: Record<string, unknown>,
    metadata: Metadata,
    options: Record<string, unknown>,
    cb: (err: unknown, resp: Binding) => void
  ): ClientUnaryCall;
  SetEnabled(
    req: Record<string, unknown>,
    metadata: Metadata,
    options: Record<string, unknown>,
    cb: (err: unknown, resp: Record<string, unknown>) => void
  ): ClientUnaryCall;
  RemoveBinding(
    req: Record<string, unknown>,
    metadata: Metadata,
    options: Record<string, unknown>,
    cb: (err: unknown, resp: Record<string, unknown>) => void
  ): ClientUnaryCall;
}

/**
 * gRPC client for the rbac microservice. Construct with `new GrpcClient(target)`.
 *
 * `target` is `host:port` (e.g. `"localhost:9080"`). Plaintext by default;
 * pass `caCert` / `caCertFile` / `insecureSkipVerify` to enable TLS.
 */
export class GrpcClient {
  private readonly client: RbacServiceClient;
  private readonly timeout: number;
  private readonly userAgent: string;
  private closed = false;

  /**
   * Creates a new gRPC RBAC client.
   * @param target - `host:port` (e.g. `"localhost:9080"`)
   * @param options - Optional configuration
   * @throws {ConfigError} If target is invalid or options conflict
   */
  constructor(target: string, options?: GrpcClientOptions) {
    const trimmed = target.trim();
    if (!trimmed) {
      throw new ConfigError('gRPC target must be non-empty');
    }
    if (trimmed.includes('://')) {
      throw new ConfigError(
        `gRPC target must be host:port (got "${trimmed}"); use Client for http(s):// URLs`
      );
    }

    this.timeout = options?.timeout ?? DEFAULT_TIMEOUT;

    // Load proto dynamically (no protoc needed at build/install time).
    const packageDef = loadSync(PROTO_PATH, {
      keepCase: false,
      longs: Number,
      enums: String,
      defaults: true,
      oneofs: true,
    });
    const grpcObj = loadPackageDefinition(packageDef);
    const rbacNs = grpcObj.rbac as unknown as Record<string, unknown>;
    const v1Ns = rbacNs.v1 as unknown as Record<string, unknown>;
    const ServiceCtor = v1Ns.RbacService as unknown as ServiceClientConstructor;
    const ua = options?.userAgent || `rbac-sdk-ts/${VERSION}`;
    this.userAgent = ua;
    this.client = new ServiceCtor(
      trimmed,
      this.buildCredentials(options),
      {
        'grpc.max_receive_message_length': 4 * 1024 * 1024,
        'grpc.primary_user_agent': ua,
      }
    ) as unknown as RbacServiceClient;
  }

  private buildCredentials(options?: GrpcClientOptions): ChannelCredentials {
    const useTls =
      !!options?.caCert || !!options?.caCertFile || !!options?.insecureSkipVerify;

    if (!useTls) {
      // Plaintext HTTP/2 (server.grpc).
      const insecureCreds = ChannelCredentials.createInsecure();
      return insecureCreds;
    }

    // TLS path
    let rootCert: Buffer | null = null;
    if (options?.caCertFile) {
      rootCert = require('node:fs').readFileSync(options.caCertFile) as Buffer;
    } else if (options?.caCert) {
      rootCert =
        typeof options.caCert === 'string'
          ? Buffer.from(options.caCert)
          : options.caCert;
    }

    // insecureSkipVerify: bypass server identity check
    const verifyOptions = options?.insecureSkipVerify
      ? { checkServerIdentity: () => undefined }
      : undefined;

    return ChannelCredentials.createSsl(
      rootCert,
      null,
      null,
      verifyOptions
    );
  }

  private ensureOpen(): void {
    if (this.closed) {
      throw new ClosedError('rbac: client is closed');
    }
  }

  private callMetadata(): Metadata {
    return new Metadata();
  }

  private callOptions(): { deadline: number } {
    // grpc-js honors `deadline` (ms timestamp) in the call options (3rd arg).
    // timeout <= 0 means no deadline (matches Go / C# / Rust).
    const deadline =
      this.timeout > 0 ? Date.now() + this.timeout : Infinity;
    return { deadline };
  }

  /** Checks service liveness. Throws `ApiError` (IsPaused) when paused. */
  async health(): Promise<HealthResponse> {
    this.ensureOpen();
    return this.unary<HealthResponse>('Health', {});
  }

  /** Reports whether subject can reach target under the given options. */
  async enforce(
    subject: string,
    target: string,
    options?: CallOptions
  ): Promise<boolean> {
    this.ensureOpen();
    const req: Record<string, unknown> = { subject, target };
    if (options?.scenarios?.length) {
      req.scenarios = options.scenarios;
    }
    if (options?.now) {
      req.now = this.toTimestamp(options.now);
    }
    const resp = await this.unary<EnforceResponse>('Enforce', req);
    return resp.allow;
  }

  /** Lists every node subject can reach, including subject itself. */
  async reachable(subject: string, options?: CallOptions): Promise<string[]> {
    this.ensureOpen();
    const req: Record<string, unknown> = { subject };
    if (options?.scenarios?.length) {
      req.scenarios = options.scenarios;
    }
    const resp = await this.unary<ReachableResponse>('Reachable', req);
    return resp.reachable || [];
  }

  /** Returns all bindings. */
  async listBindings(): Promise<Binding[]> {
    this.ensureOpen();
    const resp = await this.unary<BindingsResponse>('ListBindings', {});
    return resp.bindings || [];
  }

  /** Fetches a single binding, or throws not-found `ApiError`. */
  async getBinding(src: string, dst: string, scenario: string): Promise<Binding> {
    this.ensureOpen();
    return this.unary<Binding>('GetBinding', { src, dst, scenario });
  }

  /** Creates a binding. Duplicate → conflict `ApiError`. */
  async addBinding(binding: Binding): Promise<Binding> {
    this.ensureOpen();
    return this.unary<Binding>('AddBinding', this.bindingToProto(binding));
  }

  /** Replaces an existing binding. Missing → not-found (not created). */
  async updateBinding(binding: Binding): Promise<Binding> {
    this.ensureOpen();
    return this.unary<Binding>('UpdateBinding', this.bindingToProto(binding));
  }

  /** Toggles a binding's enabled flag. */
  async setEnabled(
    src: string,
    dst: string,
    scenario: string,
    enabled: boolean
  ): Promise<void> {
    this.ensureOpen();
    await this.unary('SetEnabled', { src, dst, scenario, enabled });
  }

  /** Deletes a binding, or throws not-found `ApiError`. */
  async removeBinding(src: string, dst: string, scenario: string): Promise<void> {
    this.ensureOpen();
    await this.unary('RemoveBinding', { src, dst, scenario });
  }

  /** Closes the underlying gRPC channel. Safe to call more than once. */
  close(): void {
    if (this.closed) return;
    this.closed = true;
    try {
      this.client.close();
    } catch {
      // ignore — channel may already be closed
    }
  }

  // --- internals ---

  private unary<T>(
    method: keyof RbacServiceClient,
    req: Record<string, unknown>
  ): Promise<T> {
    return new Promise((resolve, reject) => {
      const fn = this.client[method] as unknown as (
        r: Record<string, unknown>,
        m: Metadata,
        options: Record<string, unknown>,
        cb: (err: unknown, resp: T) => void
      ) => unknown;
      fn.call(
        this.client,
        req,
        this.callMetadata(),
        this.callOptions(),
        (err: unknown, resp: T) => {
          if (err) {
            reject(mapGrpcError(method as string, err));
            return;
          }
          resolve(resp);
        }
      );
    });
  }

  private bindingToProto(b: Binding): Record<string, unknown> {
    const out: Record<string, unknown> = {
      src: b.src,
      dst: b.dst,
      scenario: b.scenario || '',
    };
    if (b.enabled !== undefined) {
      out.enabled = b.enabled;
    }
    if (b.conditions?.length) {
      out.conditions = b.conditions.map((c) => {
        const pc: Record<string, unknown> = { kind: c.kind };
        if (c.kind === 'TIME') {
          if (c.start) pc.start = this.toTimestamp(c.start);
          if (c.end) pc.end = this.toTimestamp(c.end);
        }
        return pc;
      });
    }
    return out;
  }

  /** ISO 8601 string → protobuf Timestamp (seconds + nanos, truncated to seconds). */
  private toTimestamp(iso: string): { seconds: number; nanos: number } {
    const ms = Date.parse(iso);
    if (Number.isNaN(ms)) {
      throw new ConfigError(`invalid ISO 8601 timestamp: ${iso}`);
    }
    return { seconds: Math.floor(ms / 1000), nanos: 0 };
  }
}
