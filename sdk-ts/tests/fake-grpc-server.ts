/**
 * In-process gRPC server for testing — mirrors the fake servers in Go/C#/Rust SDKs.
 */

import { resolve } from 'node:path';
import {
  Server,
  ServerCredentials,
  loadPackageDefinition,
  status as GrpcStatus,
  type UntypedServiceImplementation,
} from '@grpc/grpc-js';
import { loadSync } from '@grpc/proto-loader';

const PROTO_PATH = resolve(__dirname, '..', 'proto', 'rbac', 'v1', 'rbac.proto');

interface FakeState {
  bindings: Map<string, Record<string, unknown>>;
  paused: boolean;
  lastEnforce: Record<string, unknown> | null;
}

function key(src: string, dst: string, scenario: string): string {
  return `${src}\0${dst}\0${scenario}`;
}

export class FakeGrpcServer {
  private server: Server;
  public port: number;
  public state: FakeState;
  public hang = false;

  constructor(port = 0, paused = false) {
    this.state = {
      bindings: new Map(),
      paused,
      lastEnforce: null,
    };
    this.port = port;

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
    const serviceDef = v1Ns.RbacService as unknown as {
      service: unknown;
    };

    const impl: UntypedServiceImplementation = {
      Health: (call: any, cb: any) => {
        if (this.hang) return; // never call cb — simulates a hung server
        if (this.state.paused) {
          cb({
            code: GrpcStatus.FAILED_PRECONDITION,
            details: 'service paused: test',
          });
          return;
        }
        cb(null, { status: 'ok', reason: '' });
      },
      GetCACert: (_call: any, cb: any) => {
        cb({ code: GrpcStatus.UNIMPLEMENTED, details: 'not used' });
      },
      Enforce: (call: any, cb: any) => {
        const req = call.request;
        this.state.lastEnforce = req;
        if (this.state.paused) {
          cb({
            code: GrpcStatus.FAILED_PRECONDITION,
            details: 'service paused: test',
          });
          return;
        }
        const k = key(req.subject, req.target, '');
        const b = this.state.bindings.get(k);
        const allow = b
          ? b.enabled === undefined || b.enabled === true
          : false;
        cb(null, { allow });
      },
      Reachable: (call: any, cb: any) => {
        const req = call.request;
        const out = [req.subject];
        for (const b of this.state.bindings.values()) {
          if (b.src === req.subject && (b.enabled === undefined || b.enabled === true)) {
            out.push(b.dst);
          }
        }
        cb(null, { subject: req.subject, reachable: out });
      },
      ListBindings: (_call: any, cb: any) => {
        cb(null, {
          bindings: Array.from(this.state.bindings.values()),
        });
      },
      GetBinding: (call: any, cb: any) => {
        const req = call.request;
        const b = this.state.bindings.get(
          key(req.src, req.dst, req.scenario)
        );
        if (!b) {
          cb({ code: GrpcStatus.NOT_FOUND, details: 'binding not found' });
          return;
        }
        cb(null, b);
      },
      AddBinding: (call: any, cb: any) => {
        const b = { ...call.request };
        const k = key(b.src, b.dst, b.scenario);
        if (this.state.bindings.has(k)) {
          cb({ code: GrpcStatus.ALREADY_EXISTS, details: 'duplicate' });
          return;
        }
        if (b.enabled === undefined || b.enabled === null) {
          b.enabled = true;
        }
        if (!b.conditions || b.conditions.length === 0) {
          b.conditions = [{ kind: 'ALL' }];
        }
        this.state.bindings.set(k, b);
        cb(null, b);
      },
      UpdateBinding: (call: any, cb: any) => {
        const b = { ...call.request };
        const k = key(b.src, b.dst, b.scenario);
        if (!this.state.bindings.has(k)) {
          cb({ code: GrpcStatus.NOT_FOUND, details: 'binding not found' });
          return;
        }
        this.state.bindings.set(k, b);
        cb(null, b);
      },
      SetEnabled: (call: any, cb: any) => {
        const req = call.request;
        const b = this.state.bindings.get(
          key(req.src, req.dst, req.scenario)
        );
        if (!b) {
          cb({ code: GrpcStatus.NOT_FOUND, details: 'binding not found' });
          return;
        }
        b.enabled = req.enabled;
        cb(null, { ok: true });
      },
      RemoveBinding: (call: any, cb: any) => {
        const req = call.request;
        if (
          !this.state.bindings.delete(key(req.src, req.dst, req.scenario))
        ) {
          cb({ code: GrpcStatus.NOT_FOUND, details: 'binding not found' });
          return;
        }
        cb(null, {});
      },
    };

    this.server = new Server();
    this.server.addService(
      serviceDef.service as Parameters<Server['addService']>[0],
      impl
    );
  }

  start(): Promise<void> {
    return new Promise((resolve, reject) => {
      this.server.bindAsync(
        `127.0.0.1:${this.port}`,
        ServerCredentials.createInsecure(),
        (err, boundPort) => {
          if (err) {
            reject(err);
            return;
          }
          this.port = boundPort;
          resolve();
        }
      );
    });
  }

  target(): string {
    return `127.0.0.1:${this.port}`;
  }

  forceShutdown(): void {
    this.server.forceShutdown();
  }
}
