/**
 * Thin typed fetch wrapper for the Sick-Fansubs API.
 *
 * Responsibilities:
 *  - Same-origin fetch with JSON content negotiation.
 *  - Attaches X-CSRF-Token to authenticated unsafe requests.
 *  - Parses RFC 9457 problem responses into typed ApiError subclasses.
 *
 * The CSRF token is held in a module-level variable. The session state
 * machine (features/auth/data-access/) calls setCSRFToken / clearCSRFToken
 * after sign-in, bootstrap, and sign-out.
 */

import type { ProblemDetail, ValidationProblem, Violation } from "./types.js";

// ── CSRF token (in-memory only — never persisted) ───────────────────
//
// The token lives in module memory on purpose: it is re-issued by the
// session bootstrap on every load, so a dev reload (HMR or full) costs a
// re-bootstrap, never a persisted credential.

let _csrfToken: string | null = null;

/** Called by the session state machine after sign-in / bootstrap. */
export function setCSRFToken(token: string): void {
  _csrfToken = token;
}

/** Clear the CSRF token on sign-out or session expiry. */
export function clearCSRFToken(): void {
  _csrfToken = null;
}

/** Exposed for tests — components should not read this directly. */
export function getCSRFToken(): string | null {
  return _csrfToken;
}

// ── Unauthenticated callback ───────────────────────────────────────

/**
 * Optional callback fired on any 401 response.
 * The session state machine registers this to transition to `anonymous`
 * without requiring every call site to check `error.status === 401`.
 */
type OnUnauthenticated = () => void;
let _onUnauthenticated: OnUnauthenticated | null = null;

/** Register a callback that runs when the API returns 401. */
export function onUnauthenticated(fn: OnUnauthenticated): void {
  _onUnauthenticated = fn;
}

/**
 * Deregister the 401 callback. The session provider calls this on
 * disconnect so a removed element never receives a transition after
 * teardown (the module-level slot otherwise keeps the old closure alive).
 */
export function clearOnUnauthenticated(): void {
  _onUnauthenticated = null;
}

// ── Error types ─────────────────────────────────────────────────────

/**
 * Base error for all API failures.
 * Consumers can check `instanceof` to distinguish problem types,
 * network errors, and validation errors.
 */
export class ApiError extends Error {
  /** RFC 9457 problem type URI path (e.g. "/problems/auth/invalid-credentials"). */
  readonly type: string;
  readonly status: number;
  readonly title: string;
  readonly detail: string | undefined;
  readonly requestId: string | undefined;
  /** Seconds from the Retry-After header (429 responses). */
  readonly retryAfterSeconds: number | undefined;

  constructor(problem: ProblemDetail, retryAfterSeconds?: number) {
    super(problem.title);
    this.name = "ApiError";
    this.type = problem.type;
    this.status = problem.status;
    this.title = problem.title;
    this.detail = problem.detail;
    this.requestId = problem.requestId;
    this.retryAfterSeconds = retryAfterSeconds;
  }
}

/** 422 Validation Failed — carries per-field violations. */
export class ValidationError extends ApiError {
  readonly violations: ValidationProblem["violations"];

  constructor(problem: ValidationProblem) {
    super(problem);
    this.name = "ValidationError";
    this.violations = problem.violations;
  }
}

/** Fetch itself failed (network down, DNS, connection refused). */
export class NetworkError extends Error {
  /** The original error that caused the failure, if available. */
  readonly cause: unknown;

  constructor(message: string, cause?: unknown) {
    super(message);
    this.name = "NetworkError";
    this.cause = cause;
  }
}

/**
 * The composed client deadline fired (requestSignal's timeout) — distinct
 * from a teardown AbortError and from a connection failure, so a caller can
 * tell "too slow" apart from "cannot reach the server".
 */
export class RequestTimeoutError extends Error {
  /** The TimeoutError DOMException that aborted the fetch, if available. */
  readonly cause: unknown;

  constructor(message = "Request deadline exceeded", cause?: unknown) {
    super(message);
    this.name = "RequestTimeoutError";
    this.cause = cause;
  }
}

// ── Internal helpers ────────────────────────────────────────────────

const BASE = "/api/v1";

/**
 * Low-level request with problem parsing.
 *
 * This is the transport seam: feature data-access modules
 * (e.g. features/auth/data-access/auth-api.ts) call it for their
 * endpoints; UI code never does.
 *
 * - 2xx: parse and return JSON body.
 * - 4xx/5xx with application/problem+json: parse and throw ApiError/ValidationError.
 * - Other non-2xx: throw ApiError with status-derived info.
 * - Network failure: throw NetworkError.
 *
 * ⚠️ The return type uses `as T` casts on `response.json()` — these are
 * runtime-unsafe (the server could return a mismatched shape). This is an
 * accepted tradeoff: the team chose a thin manual wrapper
 * over OpenAPI code generation. Integration tests are the safety net.
 */
export async function request<T>(
  method: string,
  path: string,
  body: unknown = undefined,
  opts: {
    needsCSRF?: boolean;
    signal?: AbortSignal;
    suppress401Callback?: boolean;
    extraHeaders?: Record<string, string>;
    /** Called with the raw response after a SUCCESSFUL parse — the only
     * way endpoint functions can inspect response headers (e.g. the 201
     * Location on comment create/reply). Never called on failures. */
    onResponse?: (response: Response) => void;
  } = {},
): Promise<T> {
  const {
    needsCSRF = false,
    signal,
    suppress401Callback = false,
    extraHeaders,
    onResponse,
  } = opts;

  // Only include Content-Type when we have a body (avoids sending
  // Content-Type: application/json on body-less POST requests like
  // sign-out, which is harmless but semantically incorrect).
  const hasBody = body !== undefined && method !== "GET" && method !== "HEAD";
  const headers: Record<string, string> = {
    Accept: "application/json",
    ...extraHeaders,
  };
  if (hasBody) {
    headers["Content-Type"] = "application/json";
  }
  // Attach the CSRF token for unsafe methods when requested and available.
  if (needsCSRF && _csrfToken) {
    headers["X-CSRF-Token"] = _csrfToken;
  }

  const response = await doFetch(`${BASE}${path}`, {
    method,
    headers,
    body: hasBody ? JSON.stringify(body) : undefined,
    credentials: "same-origin",
    signal,
  });
  const result = await handleResponse<T>(response, suppress401Callback);
  // Success only — handleResponse throws on every failure path.
  onResponse?.(response);
  return result;
}

/**
 * Multipart variant of `request` for the media upload
 * and the avatar upload (PUT). The browser sets
 * Content-Type (with the multipart boundary) — the JSON transport above
 * must not. Shares doFetch/handleResponse so problem parsing and the 401
 * transition behave identically; CSRF is always attached (both endpoints
 * are unsafe methods).
 */
export async function requestForm<T>(
  path: string,
  form: FormData,
  opts: { signal?: AbortSignal; method?: string } = {},
): Promise<T> {
  const headers: Record<string, string> = {
    Accept: "application/json",
  };
  if (_csrfToken) {
    headers["X-CSRF-Token"] = _csrfToken;
  }

  const response = await doFetch(`${BASE}${path}`, {
    method: opts.method ?? "POST",
    headers,
    body: form,
    credentials: "same-origin",
    signal: opts.signal,
  });
  return handleResponse<T>(response, false);
}

/**
 * fetch with the transport's failure mapping: teardown aborts rethrow their
 * AbortError (callers test signal.aborted), the composed deadline surfaces
 * as RequestTimeoutError, and every other failure becomes a NetworkError.
 */
async function doFetch(input: string, init: RequestInit): Promise<Response> {
  try {
    return await fetch(input, init);
  } catch (err) {
    if (err instanceof DOMException && err.name === "AbortError") {
      throw err;
    }
    if (err instanceof DOMException && err.name === "TimeoutError") {
      throw new RequestTimeoutError("Request deadline exceeded", err);
    }
    throw new NetworkError(
      err instanceof Error ? err.message : "Network request failed",
      err,
    );
  }
}

/**
 * Shared post-fetch handling: 2xx JSON parsing, the 401 transition (unless
 * suppressed), and RFC 9457 problem parsing. Both request and requestForm
 * route through this so error behavior can never drift between them.
 */
async function handleResponse<T>(
  response: Response,
  suppress401Callback: boolean,
): Promise<T> {
  // Success — parse JSON body.
  if (response.ok) {
    // A bodyless success resolves undefined: 204 always, and the 201 the
    // comments create/reply endpoints return with only a Location header
    // Reading the text first is what makes both cases work —
    // response.json() would throw on an empty body and turn a valid 201
    // into an internal error.
    const text = await response.text();
    if (text === "") return undefined as T;
    try {
      return JSON.parse(text) as T;
    } catch {
      throw new ApiError({
        type: "/problems/internal-error",
        title: "Failed to parse response body",
        status: response.status,
      });
    }
  }

  // 401 — session expired, suspended, or revoked.
  // Automatically clear the CSRF token and notify the session state machine
  // so it can transition the UI to `anonymous`.
  //
  // suppress401Callback prevents this for endpoints that use 401 for
  // "invalid credentials" rather than session expiry (e.g., password
  // change wrong-password).
  if (response.status === 401 && !suppress401Callback) {
    clearCSRFToken();
    _onUnauthenticated?.();
  }

  // Try to parse as RFC 9457 problem.
  const contentType = response.headers.get("Content-Type") ?? "";
  const retryAfter = parseRetryAfter(response.headers.get("Retry-After"));
  if (contentType.includes("application/problem+json")) {
    let body: unknown;
    try {
      body = await response.json();
    } catch {
      throw new ApiError(internalErrorProblem(response.status), retryAfter);
    }

    // A non-object body is a malformed problem document — answer the
    // internal-error shape rather than dereferencing it.
    const raw =
      body !== null && typeof body === "object"
        ? (body as Record<string, unknown>)
        : null;
    const problem = raw
      ? normalizeProblem(raw, response.status)
      : internalErrorProblem(response.status);

    // 422 has a violations array — use the richer ValidationError. The
    // Array.isArray guard doubles as the malformed-response defense.
    if (response.status === 422 && Array.isArray(raw?.violations)) {
      throw new ValidationError({
        ...problem,
        violations: raw.violations as Violation[],
      });
    }

    throw new ApiError(problem, retryAfter);
  }

  // Non-JSON error response — build the unexpected-response problem. A
  // valid Retry-After still rides the error, so a 429 cooldown survives a
  // non-problem body.
  throw new ApiError(unexpectedProblem(response.status), retryAfter);
}

/** The internal-error problem shape used when a response body cannot be
 * trusted as a problem document. */
function internalErrorProblem(status: number): ProblemDetail {
  return {
    type: "/problems/internal-error",
    title: "Failed to parse error response",
    status,
  };
}

/** The fallback problem for an error response that is not a problem
 * document at all. */
function unexpectedProblem(status: number): ProblemDetail {
  return {
    type: "/problems/internal-error",
    title: `Unexpected ${status} response`,
    status,
  };
}

/** Coerce a parsed problem body to the wire shape: every RFC 9457 member is
 * optional on the wire, so a missing or wrongly-typed one falls back. The
 * HTTP response status is authoritative — a disagreeing body status cannot
 * misroute a consumer's status branch. */
function normalizeProblem(
  raw: Record<string, unknown>,
  status: number,
): ProblemDetail {
  return {
    type: typeof raw.type === "string" ? raw.type : "/problems/internal-error",
    title:
      typeof raw.title === "string"
        ? raw.title
        : `Unexpected ${status} response`,
    status,
    detail: typeof raw.detail === "string" ? raw.detail : undefined,
    requestId: typeof raw.requestId === "string" ? raw.requestId : undefined,
  };
}

/**
 * Parse a Retry-After header into seconds — whole integer strings ≥ 1 only,
 * per the OpenAPI `RateLimited` contract (`type: integer, minimum: 1`). The
 * RFC 9110 HTTP-date form and any junk are dropped, never misparsed.
 */
function parseRetryAfter(header: string | null): number | undefined {
  if (!header) return undefined;
  const raw = header.trim();
  if (!/^\d+$/.test(raw)) return undefined;
  const seconds = Number(raw);
  return Number.isSafeInteger(seconds) && seconds >= 1 ? seconds : undefined;
}

// ── Options type ────────────────────────────────────────────────────

/** Per-request options (AbortSignal for cancellation/timeout). */
export interface RequestOptions {
  /** AbortSignal for request cancellation (cancellation/timeout explicit per operation). */
  signal?: AbortSignal;
}
