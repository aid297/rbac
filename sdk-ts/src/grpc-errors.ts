/**
 * gRPC error mapping — mirrors the HTTP error predicates.
 *
 * Transport codes (Unavailable, Cancelled, DeadlineExceeded) are returned
 * as-is so callers can distinguish them from application-level ApiErrors.
 * Service pause uses FailedPrecondition on the wire → HTTP 503.
 */

import { status as GrpcStatus } from '@grpc/grpc-js';
import { ApiError } from './errors';

/** gRPC status code → HTTP status code (matches Go/C#/Rust SDKs). */
export function grpcCodeToHttp(code: number): number {
  switch (code) {
    case GrpcStatus.OK:
      return 200;
    case GrpcStatus.INVALID_ARGUMENT:
    case GrpcStatus.OUT_OF_RANGE:
      return 400;
    case GrpcStatus.FAILED_PRECONDITION:
      return 503;
    case GrpcStatus.NOT_FOUND:
      return 404;
    case GrpcStatus.ALREADY_EXISTS:
    case GrpcStatus.ABORTED:
      return 409;
    case GrpcStatus.PERMISSION_DENIED:
      return 403;
    case GrpcStatus.UNAUTHENTICATED:
      return 401;
    case GrpcStatus.RESOURCE_EXHAUSTED:
      return 429;
    case GrpcStatus.UNIMPLEMENTED:
      return 501;
    default:
      return 500;
  }
}

/** Transport-level codes that should NOT be wrapped as ApiError. */
const TRANSPORT_CODES = new Set<number>([
  GrpcStatus.UNAVAILABLE,
  GrpcStatus.CANCELLED,
  GrpcStatus.DEADLINE_EXCEEDED,
]);

/**
 * Maps a gRPC error to an `ApiError` (for application codes) or returns
 * the original error (for transport codes). Mirrors `mapGRPCError` in Go.
 */
export function mapGrpcError(method: string, err: unknown): unknown {
  if (!err || typeof err !== 'object' || !('code' in err)) {
    return err;
  }
  const code = (err as { code: number }).code;
  if (TRANSPORT_CODES.has(code)) {
    return err; // raw transport error — not IsPaused
  }
  const message =
    (err as { details?: string; message?: string }).details ||
    (err as { message?: string }).message ||
    'gRPC error';
  return new ApiError(message, grpcCodeToHttp(code), 'RPC', method);
}

/** True when the error came from the gRPC path (mapped ApiError or raw transport). */
export function isGrpcError(err: unknown): boolean {
  if (err instanceof ApiError && err.method === 'RPC') {
    return true;
  }
  // grpc-js transport errors are Error instances with a numeric `code` property.
  return (
    err instanceof Error &&
    'code' in err &&
    typeof (err as { code: unknown }).code === 'number'
  );
}
