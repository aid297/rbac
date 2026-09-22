/**
 * Represents a binding between source and destination with optional conditions.
 */
export interface Binding {
  src: string;
  dst: string;
  scenario: string;
  enabled?: boolean;
  conditions?: Condition[];
}

/**
 * Represents a condition for a binding.
 */
export interface Condition {
  kind: ConditionKind;
  start?: string; // ISO 8601 datetime
  end?: string;   // ISO 8601 datetime
}

/**
 * Possible condition kinds.
 */
export enum ConditionKind {
  All = 'ALL',
  Time = 'TIME',
}

/**
 * Options for enforce and reachable calls.
 */
export interface CallOptions {
  scenarios?: string[];
  now?: string; // ISO 8601 datetime
}

/**
 * Response from health check endpoint.
 */
export interface HealthResponse {
  status: string;
  reason?: string;
}

/**
 * Response from enforce endpoint.
 */
export interface EnforceResponse {
  allow: boolean;
}

/**
 * Response from reachable endpoint.
 */
export interface ReachableResponse {
  reachable: string[];
}

/**
 * Response from bindings list endpoint.
 */
export interface BindingsResponse {
  bindings: Binding[];
}

/**
 * Error response from the API.
 */
export interface ApiErrorResponse {
  error?: string;
  reason?: string;
}
