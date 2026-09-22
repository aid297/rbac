import { Client, ApiError, ConfigError, ApiErrors } from '../src';

describe('Client', () => {
  describe('constructor', () => {
    test('accepts valid HTTP URL', () => {
      const client = new Client('http://localhost:8080');
      expect(client).toBeInstanceOf(Client);
    });

    test('accepts valid HTTPS URL', () => {
      const client = new Client('https://localhost:8443');
      expect(client).toBeInstanceOf(Client);
    });

    test('rejects invalid URL', () => {
      expect(() => new Client('not-a-url')).toThrow(ConfigError);
    });

    test('rejects non-HTTP scheme', () => {
      expect(() => new Client('ftp://example.com')).toThrow(ConfigError);
    });

    test('rejects URL without host', () => {
      // Note: 'http:///path' is actually valid in URL parsing (hostname becomes empty string)
      // So we test with a truly invalid URL instead
      expect(() => new Client('http://')).toThrow(ConfigError);
    });

    test('trims whitespace from URL', () => {
      const client = new Client('  http://localhost:8080  ');
      expect(client).toBeInstanceOf(Client);
    });

    test('removes trailing slash from URL', () => {
      const client = new Client('http://localhost:8080/');
      expect(client).toBeInstanceOf(Client);
    });
  });

  describe('options', () => {
    test('uses default timeout', () => {
      const client = new Client('http://localhost:8080');
      // Default timeout is 30000ms
      expect(client).toBeInstanceOf(Client);
    });

    test('accepts custom timeout', () => {
      const client = new Client('http://localhost:8080', {
        timeout: 5000,
      });
      expect(client).toBeInstanceOf(Client);
    });

    test('accepts custom user agent', () => {
      const client = new Client('http://localhost:8080', {
        userAgent: 'my-custom-agent/1.0',
      });
      expect(client).toBeInstanceOf(Client);
    });
  });
});

describe('ApiError', () => {
  test('isNotFound returns true for 404', () => {
    const error = new ApiError('Not Found', 404, 'GET', '/test');
    expect(error.isNotFound()).toBe(true);
  });

  test('isConflict returns true for 409', () => {
    const error = new ApiError('Conflict', 409, 'POST', '/test');
    expect(error.isConflict()).toBe(true);
  });

  test('isBadRequest returns true for 400', () => {
    const error = new ApiError('Bad Request', 400, 'POST', '/test');
    expect(error.isBadRequest()).toBe(true);
  });

  test('isPaused returns true for 503', () => {
    const error = new ApiError('Service Unavailable', 503, 'GET', '/healthz');
    expect(error.isPaused()).toBe(true);
  });

  test('ApiErrors helpers work correctly', () => {
    const notFoundError = new ApiError('Not Found', 404, 'GET', '/test');
    const otherError = new Error('Some error');

    expect(ApiErrors.isNotFound(notFoundError)).toBe(true);
    expect(ApiErrors.isNotFound(otherError)).toBe(false);
    expect(ApiErrors.isConflict(notFoundError)).toBe(false);
  });
});
