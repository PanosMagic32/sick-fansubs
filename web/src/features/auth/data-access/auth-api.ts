/**
 * Auth endpoint functions — the wire contract for the six authentication
 * endpoints, built on the shared transport
 * (shared/api/client.ts: request + problem parsing).
 *
 * Endpoint functions live in the
 * feature's data-access/, next to the state machine and wire types — the
 * transport itself stays feature-free in shared/api/.
 *
 * The CSRF/401 knobs per endpoint are wire-contract facts:
 *  - sign-in/register/bootstrap send no CSRF (no token exists yet);
 *  - sign-out/sign-out-all/changePassword require CSRF;
 *  - changePassword suppresses the 401 auto-transition (its 401 means
 *    "wrong current password").
 */

import { request, type RequestOptions } from "@shared/api/client.js";

import type {
  ForgotPasswordRequest,
  ForgotPasswordResponse,
  PasswordChangeRequest,
  PasswordChangeResponse,
  RegisterRequest,
  RegisterResponse,
  ResendVerificationResponse,
  ResetPasswordRequest,
  ResetPasswordResponse,
  SessionBootstrapResponse,
  SignInRequest,
  SignInResponse,
  SignOutResponse,
  VerifyEmailRequest,
  VerifyEmailResponse,
} from "./types.js";

/**
 * Client-side timeout for auth actions (cancellation/timeout is explicit
 * per client operation). 15s sits under the server's 30s
 * WriteTimeout, so a stalled connection fails here — unfreezing the form
 * with a generic network banner — instead of hanging until the server
 * gives up. Applied as the default for every endpoint below; a caller
 * that passes its own signal (the session bootstrap does — its abort
 * controller covers disconnect) keeps it instead.
 */
export const AUTH_ACTION_TIMEOUT_MS = 15_000;

/** Caller signal, or the auth-action timeout when none is passed. */
function actionSignal(opts?: RequestOptions): AbortSignal | undefined {
  return opts?.signal ?? AbortSignal.timeout(AUTH_ACTION_TIMEOUT_MS);
}

/**
 * POST /api/v1/auth/sign-in — authenticate with username/email + password.
 * Does not send CSRF (the client has no token before authentication).
 */
export function signIn(
  body: SignInRequest,
  opts?: RequestOptions,
): Promise<SignInResponse> {
  return request<SignInResponse>("POST", "/auth/sign-in", body, {
    signal: actionSignal(opts),
  });
}

/** POST /api/v1/auth/register — create a new account and auto sign-in. */
export function register(
  body: RegisterRequest,
  opts?: RequestOptions,
): Promise<RegisterResponse> {
  return request<RegisterResponse>("POST", "/auth/register", body, {
    signal: actionSignal(opts),
  });
}

/** GET /api/v1/auth/session — check current session state. */
export function bootstrapSession(
  opts?: RequestOptions,
): Promise<SessionBootstrapResponse> {
  return request<SessionBootstrapResponse>("GET", "/auth/session", undefined, {
    signal: opts?.signal,
  });
}

/** POST /api/v1/auth/sign-out — end the current session. Requires CSRF. */
export function signOut(opts?: RequestOptions): Promise<SignOutResponse> {
  return request<SignOutResponse>("POST", "/auth/sign-out", undefined, {
    needsCSRF: true,
    signal: actionSignal(opts),
  });
}

/**
 * POST /api/v1/auth/sign-out-all — end all sessions for the current user.
 * Requires CSRF. Takes no request body.
 */
export function signOutAll(opts?: RequestOptions): Promise<SignOutResponse> {
  return request<SignOutResponse>("POST", "/auth/sign-out-all", undefined, {
    needsCSRF: true,
    signal: actionSignal(opts),
  });
}

/**
 * PUT /api/v1/auth/password — change password.
 * Atomically revokes all sessions and clears the cookie.
 * The user must sign in again after this call.
 * Requires CSRF. suppress401Callback prevents the 401 auto-transition
 * on wrong current password (the user stays authenticated to retry).
 */
export function changePassword(
  body: PasswordChangeRequest,
  opts?: RequestOptions,
): Promise<PasswordChangeResponse> {
  return request<PasswordChangeResponse>("PUT", "/auth/password", body, {
    needsCSRF: true,
    signal: actionSignal(opts),
    suppress401Callback: true,
  });
}

/**
 * POST /api/v1/auth/forgot-password — email a reset link.
 * Always 200 with an empty object: unknown identifiers and suspended
 * accounts are indistinguishable from known ones. No CSRF (the caller may
 * be anonymous; the server rejects authenticated requests with 403).
 */
export function forgotPassword(
  body: ForgotPasswordRequest,
  opts?: RequestOptions,
): Promise<ForgotPasswordResponse> {
  return request<ForgotPasswordResponse>(
    "POST",
    "/auth/forgot-password",
    body,
    {
      signal: actionSignal(opts),
    },
  );
}

/**
 * POST /api/v1/auth/reset-password — consume a reset token.
 * 204 on success; the session cookie is cleared when the current session
 * belongs to the reset account. needsCSRF attaches the token only when a
 * session exists — anonymous callers post without one (the middleware
 * requires CSRF only for authenticated requests).
 */
export function resetPassword(
  body: ResetPasswordRequest,
  opts?: RequestOptions,
): Promise<ResetPasswordResponse> {
  return request<ResetPasswordResponse>("POST", "/auth/reset-password", body, {
    needsCSRF: true,
    signal: actionSignal(opts),
  });
}

/**
 * POST /api/v1/auth/verify-email — consume a verification token.
 * 204 on success (idempotent for an already-verified user). Allowed
 * regardless of session state — needsCSRF attaches the token only when a
 * session exists (the reset-page precedent).
 */
export function verifyEmail(
  body: VerifyEmailRequest,
  opts?: RequestOptions,
): Promise<VerifyEmailResponse> {
  return request<VerifyEmailResponse>("POST", "/auth/verify-email", body, {
    needsCSRF: true,
    signal: actionSignal(opts),
  });
}

/**
 * POST /api/v1/auth/resend-verification — re-send the verification email.
 * Authenticated + CSRF; 204 always (an
 * already-verified user is a no-op). 429 carries Retry-After from the
 * shared 1/30min bucket. The 401 is a genuinely dead session — no
 * suppression, the global transition stays active.
 */
export function resendVerification(
  opts?: RequestOptions,
): Promise<ResendVerificationResponse> {
  return request<ResendVerificationResponse>(
    "POST",
    "/auth/resend-verification",
    undefined,
    {
      needsCSRF: true,
      signal: actionSignal(opts),
    },
  );
}
