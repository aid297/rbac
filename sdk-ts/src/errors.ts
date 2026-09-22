import { ApiErrorResponse } from './types';

/**
 * Custom error class for API errors.
 */
export class ApiError extends Error {
  public readonly statusCode: number;
  public readonly method: string;
  public readonly path: string;
  public readonly body?: string;

  constructor(
    message: string,
    statusCode: number,
    method: string,
    path: string,
    body?: string
  ) {
    super(message);
    this.name = 'ApiError';
    this.statusCode = statusCode;
    this.method = method;
    this.path = path;
    this.body = body;
  }

  /**
   * Check if this is a "not found" error (404).
   */
  isNotFound(): boolean {
    return this.statusCode === 404;
  }

  /**
   * Check if this is a "conflict" error (409).
   */
  isConflict(): boolean {
    return this.statusCode === 409;
  }

  /**
   * Check if this is a "bad request" error (400).
   */
  isBadRequest(): boolean {
    return this.statusCode === 400;
  }

  /**
   * Check if this is a "service paused" error (503).
   */
  isPaused(): boolean {
    return this.statusCode === 503;
  }
}

/**
 * Custom error class for configuration errors.
 */
export class ConfigError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'ConfigError';
  }
}

/**
 * Helper functions to check error types.
 */
export const ApiErrors = {
  isNotFound: (error: unknown): boolean =>
    error instanceof ApiError && error.isNotFound(),

  isConflict: (error: unknown): boolean =>
    error instanceof ApiError && error.isConflict(),

  isBadRequest: (error: unknown): boolean =>
    error instanceof ApiError && error.isBadRequest(),

  isPaused: (error: unknown): boolean =>
    error instanceof ApiError && error.isPaused(),
};
