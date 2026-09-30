import { GrpcClient, ApiError, ConfigError, ClosedError, ApiErrors, isGrpcError } from '../src';
import { FakeGrpcServer } from './fake-grpc-server';
import { ConditionKind } from '../src/types';

describe('GrpcClient', () => {
  let server: FakeGrpcServer;

  beforeAll(async () => {
    server = new FakeGrpcServer(0, false);
    await server.start();
  });

  afterAll(() => {
    server.forceShutdown();
  });

  describe('constructor', () => {
    test('accepts host:port', () => {
      const client = new GrpcClient('localhost:9080');
      expect(client).toBeInstanceOf(GrpcClient);
      client.close();
    });

    test('rejects empty target', () => {
      expect(() => new GrpcClient('')).toThrow(ConfigError);
    });

    test('rejects URL with scheme', () => {
      expect(() => new GrpcClient('http://localhost:9080')).toThrow(ConfigError);
    });

    test('accepts custom timeout', () => {
      const client = new GrpcClient('localhost:9080', { timeout: 5000 });
      expect(client).toBeInstanceOf(GrpcClient);
      client.close();
    });

    test('accepts custom user agent', () => {
      const client = new GrpcClient('localhost:9080', {
        userAgent: 'my-agent/1.0',
      });
      expect(client).toBeInstanceOf(GrpcClient);
      client.close();
    });
  });

  describe('health', () => {
    test('returns ok when healthy', async () => {
      const client = new GrpcClient(server.target());
      const resp = await client.health();
      expect(resp.status).toBe('ok');
      client.close();
    });
  });

  describe('enforce', () => {
    test('returns true for allowed binding', async () => {
      const client = new GrpcClient(server.target());
      await client.addBinding({
        src: 'alice',
        dst: 'doc:42',
        scenario: '',
        enabled: true,
        conditions: [{ kind: ConditionKind.All }],
      });
      const allow = await client.enforce('alice', 'doc:42');
      expect(allow).toBe(true);
      client.close();
    });

    test('returns false for no binding', async () => {
      const client = new GrpcClient(server.target());
      const allow = await client.enforce('nobody', 'nothing');
      expect(allow).toBe(false);
      client.close();
    });

    test('passes scenarios and now', async () => {
      const client = new GrpcClient(server.target());
      await client.enforce('a', 'b', {
        scenarios: ['VIP'],
        now: '2026-06-15T00:00:00Z',
      });
      const last = server.state.lastEnforce as Record<string, unknown>;
      expect(last.subject).toBe('a');
      expect(last.scenarios).toEqual(['VIP']);
      expect(last.now).toEqual({ seconds: 1781481600, nanos: 0 });
      client.close();
    });
  });

  describe('reachable', () => {
    test('returns list including subject', async () => {
      const client = new GrpcClient(server.target());
      const nodes = await client.reachable('alice', { scenarios: ['VIP'] });
      expect(nodes).toContain('alice');
      client.close();
    });
  });

  describe('binding CRUD', () => {
    test('full lifecycle', async () => {
      const client = new GrpcClient(server.target());

      // Create
      const created = await client.addBinding({
        src: 'bob',
        dst: 'role:editor',
        scenario: '',
        enabled: true,
        conditions: [{ kind: ConditionKind.All }],
      });
      expect(created.src).toBe('bob');

      // Read
      const got = await client.getBinding('bob', 'role:editor', '');
      expect(got.dst).toBe('role:editor');

      // List
      const list = await client.listBindings();
      expect(list.length).toBeGreaterThan(0);

      // Update with time range
      await client.updateBinding({
        src: 'bob',
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

      // Disable
      await client.setEnabled('bob', 'role:editor', '', false);
      expect(await client.enforce('bob', 'role:editor')).toBe(false);

      // Conflict on duplicate
      const err = await client
        .addBinding({
          src: 'bob',
          dst: 'role:editor',
          scenario: '',
        })
        .catch((e) => e);
      expect(ApiErrors.isConflict(err)).toBe(true);

      // Delete
      await client.removeBinding('bob', 'role:editor', '');

      // Not found
      const nf = await client
        .getBinding('bob', 'role:editor', '')
        .catch((e) => e);
      expect(ApiErrors.isNotFound(nf)).toBe(true);

      client.close();
    });
  });

  describe('paused', () => {
    test('health throws IsPaused', async () => {
      const pausedServer = new FakeGrpcServer(0, true);
      await pausedServer.start();
      const client = new GrpcClient(pausedServer.target());
      const err = await client.health().catch((e) => e);
      expect(ApiErrors.isPaused(err)).toBe(true);
      expect(isGrpcError(err)).toBe(true);
      client.close();
      pausedServer.forceShutdown();
    });
  });

  describe('close', () => {
    test('methods throw ClosedError after close', async () => {
      const client = new GrpcClient(server.target());
      client.close();
      await expect(client.health()).rejects.toThrow(ClosedError);
      client.close(); // idempotent
    });
  });

  describe('timeout', () => {
    test('deadline exceeded when server hangs', async () => {
      const hungServer = new FakeGrpcServer(0, false);
      hungServer.hang = true;
      await hungServer.start();

      const client = new GrpcClient(hungServer.target(), { timeout: 200 });
      const err = await client.health().catch((e) => e);

      // DEADLINE_EXCEEDED (code 4) must NOT be IsPaused.
      expect(ApiErrors.isPaused(err)).toBe(false);
      expect(isGrpcError(err)).toBe(true);
      expect((err as { code?: number }).code).toBe(4); // GrpcStatus.DEADLINE_EXCEEDED

      client.close();
      hungServer.forceShutdown();
    }, 10000);
  });

  describe('error mapping', () => {
    test('not found is grpc', async () => {
      const client = new GrpcClient(server.target());
      const err = await client
        .getBinding('no', 'such', '')
        .catch((e) => e);
      expect(ApiErrors.isNotFound(err)).toBe(true);
      expect(isGrpcError(err)).toBe(true);
      client.close();
    });
  });
});
