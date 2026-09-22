import { ApiError, ConfigError } from './errors';
import {
  Binding,
  CallOptions,
  EnforceResponse,
  ReachableResponse,
  BindingsResponse,
  HealthResponse,
} from './types';

/**
 * SDK version for User-Agent header.
 */
const VERSION = '0.1.0';
const DEFAULT_USER_AGENT = `rbac-sdk-ts/${VERSION}`;
const DEFAULT_TIMEOUT = 30000; // 30 seconds
const MAX_RESPONSE_BYTES = 4 * 1024 * 1024; // 4MB

/**
 * Options for creating a Client.
 */
export interface ClientOptions {
  /** Custom fetch implementation. If not provided, uses global fetch. */
  fetch?: typeof fetch;
  /** Timeout in milliseconds for requests. Defaults to 30000ms. */
  timeout?: number;
  /** Custom User-Agent header. */
  userAgent?: string;
}

/**
 * RBAC SDK Client for interacting with the rbac microservice /v1 API.
 */
export class Client {
  private readonly baseUrl: string;
  private readonly fetchFn: typeof fetch;
  private readonly timeout: number;
  private readonly userAgent: string;

  /**
   * Creates a new RBAC client.
   * @param baseUrl - The base URL of the rbac service (e.g., "http://localhost:8080")
   * @param options - Optional configuration
   */
  constructor(baseUrl: string, options?: ClientOptions) {
    const trimmed = baseUrl.trim();

    // Validate URL
    try {
      const url = new URL(trimmed);
      if (url.protocol !== 'http:' && url.protocol !== 'https:') {
        throw new ConfigError(
          `Base URL scheme must be http or https, got "${url.protocol}"`
        );
      }
      if (!url.hostname) {
        throw new ConfigError('Base URL must include a host');
      }
    } catch (error) {
      if (error instanceof ConfigError) {
        throw error;
      }
      throw new ConfigError(`Invalid base URL: ${baseUrl}`);
    }

    this.baseUrl = trimmed.endsWith('/') ? trimmed.slice(0, -1) : trimmed;
    this.fetchFn = options?.fetch || globalThis.fetch;
    this.timeout = options?.timeout ?? DEFAULT_TIMEOUT;
    this.userAgent = options?.userAgent || DEFAULT_USER_AGENT;
  }

  /**
   * Checks service liveness.
   * @returns Health status information
   * @throws {ApiError} If service is paused (503) or other errors
   */
  async health(): Promise<HealthResponse> {
    return this.doRequest<HealthResponse>('GET', '/healthz');
  }

  /**
   * Reports whether subject can reach target under the given options.
   * @param subject - The subject identifier
   * @param target - The target identifier
   * @param options - Optional call options (scenarios, now)
   * @returns true if allowed, false otherwise
   */
  async enforce(
    subject: string,
    target: string,
    options?: CallOptions
  ): Promise<boolean> {
    const body = {
      subject,
      target,
      scenarios: options?.scenarios,
      now: options?.now,
    };

    const response = await this.doJson<EnforceResponse>(
      'POST',
      '/v1/enforce',
      body
    );
    return response.allow;
  }

  /**
   * Lists every node subject can reach, including subject itself.
   * @param subject - The subject identifier
   * @param options - Optional call options (scenarios)
   * @returns Array of reachable node identifiers
   */
  async reachable(
    subject: string,
    options?: CallOptions
  ): Promise<string[]> {
    const params = new URLSearchParams();
    params.set('subject', subject);
    if (options?.scenarios) {
      for (const scenario of options.scenarios) {
        params.append('scenario', scenario);
      }
    }

    const response = await this.doJson<ReachableResponse>(
      'GET',
      '/v1/reachable',
      undefined,
      params
    );
    return response.reachable;
  }

  /**
   * Returns all bindings.
   * @returns Array of bindings
   */
  async listBindings(): Promise<Binding[]> {
    const response = await this.doJson<BindingsResponse>(
      'GET',
      '/v1/bindings'
    );
    return response.bindings;
  }

  /**
   * Fetches a single binding.
   * @param src - Source identifier
   * @param dst - Destination identifier
   * @param scenario - Scenario name
   * @returns The binding
   * @throws {ApiError} If binding not found (404)
   */
  async getBinding(
    src: string,
    dst: string,
    scenario: string
  ): Promise<Binding> {
    const params = new URLSearchParams();
    params.set('src', src);
    params.set('dst', dst);
    if (scenario) {
      params.set('scenario', scenario);
    }

    return this.doJson<Binding>('GET', '/v1/bindings', undefined, params);
  }

  /**
   * Creates a binding.
   * @param binding - The binding to create
   * @returns The created binding
   * @throws {ApiError} If binding already exists (409)
   */
  async addBinding(binding: Binding): Promise<Binding> {
    return this.doJson<Binding>('POST', '/v1/bindings', binding);
  }

  /**
   * Replaces an existing binding.
   * @param binding - The binding to update
   * @returns The updated binding
   * @throws {ApiError} If binding not found (404)
   */
  async updateBinding(binding: Binding): Promise<Binding> {
    return this.doJson<Binding>('PUT', '/v1/bindings', binding);
  }

  /**
   * Toggles a binding's enabled flag.
   * @param src - Source identifier
   * @param dst - Destination identifier
   * @param scenario - Scenario name
   * @param enabled - Whether the binding should be enabled
   */
  async setEnabled(
    src: string,
    dst: string,
    scenario: string,
    enabled: boolean
  ): Promise<void> {
    const body = { src, dst, scenario, enabled };
    await this.doRequest<void>('PATCH', '/v1/bindings/enabled', body);
  }

  /**
   * Deletes a binding.
   * @param src - Source identifier
   * @param dst - Destination identifier
   * @param scenario - Scenario name
   * @throws {ApiError} If binding not found (404)
   */
  async removeBinding(
    src: string,
    dst: string,
    scenario: string
  ): Promise<void> {
    const params = new URLSearchParams();
    params.set('src', src);
    params.set('dst', dst);
    if (scenario) {
      params.set('scenario', scenario);
    }

    await this.doRequest<void>('DELETE', '/v1/bindings', undefined, params);
  }

  /**
   * Performs a JSON request and parses the response.
   */
  private async doJson<T>(
    method: string,
    path: string,
    body?: unknown,
    params?: URLSearchParams
  ): Promise<T> {
    const data = await this.doRequest<unknown>(method, path, body, params);
    return data as T;
  }

  /**
   * Performs an HTTP request.
   */
  private async doRequest<T>(
    method: string,
    path: string,
    body?: unknown,
    params?: URLSearchParams
  ): Promise<T> {
    // Build URL
    let url = `${this.baseUrl}${path}`;
    if (params && params.toString()) {
      url += `?${params.toString()}`;
    }

    // Build request options
    const headers: Record<string, string> = {
      Accept: 'application/json',
      'User-Agent': this.userAgent,
    };

    const options: RequestInit = {
      method,
      headers,
      signal: AbortSignal.timeout(this.timeout),
    };

    // Add body if present
    if (body !== undefined) {
      options.body = JSON.stringify(body);
      headers['Content-Type'] = 'application/json';
    }

    // Execute request
    let response: Response;
    try {
      response = await this.fetchFn(url, options);
    } catch (error) {
      if (error instanceof Error && error.name === 'TimeoutError') {
        throw new Error(`Request timeout after ${this.timeout}ms`);
      }
      throw error;
    }

    // Read response body
    const arrayBuffer = await response.arrayBuffer();
    const bytes = new Uint8Array(arrayBuffer);

    // Limit response size
    if (bytes.length > MAX_RESPONSE_BYTES) {
      throw new Error('Response body exceeds maximum size limit');
    }

    const text = new TextDecoder().decode(bytes);

    // Handle non-2xx responses
    if (response.status < 200 || response.status > 299) {
      let message = `HTTP ${response.status}`;
      try {
        const errorBody: { error?: string; reason?: string } =
          JSON.parse(text);
        message = errorBody.error || errorBody.reason || message;
      } catch {
        // Use default message if JSON parsing fails
      }

      throw new ApiError(
        message,
        response.status,
        method,
        path,
        text
      );
    }

    // Parse JSON response if present
    if (text.trim()) {
      try {
        return JSON.parse(text) as T;
      } catch {
        throw new Error(`Failed to parse JSON response: ${text}`);
      }
    }

    return {} as T;
  }
}
