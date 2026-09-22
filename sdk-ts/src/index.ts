/**
 * Official TypeScript/JavaScript client SDK for the rbac authorization microservice /v1 API.
 *
 * @example
 * ```typescript
 * import { Client } from '@rbac/sdk';
 *
 * const client = new Client('http://localhost:8080');
 * const allow = await client.enforce('alice', 'doc:42');
 * ```
 */

export { Client, type ClientOptions } from './client';
export { ApiError, ConfigError, ApiErrors } from './errors';
export * from './types';

/**
 * SDK version.
 */
export const VERSION = '0.1.0';
