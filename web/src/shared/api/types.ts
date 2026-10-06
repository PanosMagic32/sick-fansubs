/**
 * Shared transport types — the RFC 9457 problem shape and validation
 * violations. These belong to the HTTP transport (shared/api), not to
 * any feature: every endpoint error shares them.
 *
 * Feature request/response DTOs live in each feature's data-access/ —
 * e.g. the auth DTOs are in features/auth/data-access/types.ts.
 */

/**
 * RFC 9457 Problem Details.
 *
 * The Go server (internal/problem/problem.go) always emits `type`, `title`,
 * and `status`, so these are required fields here even though the RFC makes
 * `type` optional (defaults to "about:blank" when absent).
 */
export interface ProblemDetail {
  type: string;
  title: string;
  status: number;
  detail?: string;
  requestId?: string;
}

/** A single field-level validation error (matches Go's handler.Violation). */
export interface Violation {
  field: string;
  code: string;
}

/** 422 response from the API — extends ProblemDetail with violations. */
export interface ValidationProblem extends ProblemDetail {
  violations: Violation[];
}
