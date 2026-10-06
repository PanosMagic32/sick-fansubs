/**
 * Current-user profile endpoint functions — built on the shared transport
 * like auth-api.ts.
 *
 * GET /api/v1/users/me is a safe authenticated read: no CSRF token, no 401
 * suppression. The 401 here is a genuine dead session, so the global
 * 401→anonymous transition in shared/api/client.ts stays active — a
 * profile fetch racing a cross-tab sign-out heals through the session
 * provider's revalidate.
 *
 * PUT /api/v1/users/me/avatar is the self-service upload (multipart,
 * CSRF'd like every mutation). It returns the refreshed profile — the
 * caller swaps its avatar state without a second round trip.
 *
 * Cancellation/timeout: the profile read and the avatar upload follow the
 * page-owned cancellation contract (the page's AbortController composed
 * with requestSignal); the avatar upload is the recorded no-client-timeout
 * exception (a large body can legitimately outlive the action deadline).
 * changeEmail is an auth action and defaults to AUTH_ACTION_TIMEOUT_MS
 * like the auth-api endpoints.
 */

import {
  request,
  requestForm,
  type RequestOptions,
} from "@shared/api/client.js";

import { AUTH_ACTION_TIMEOUT_MS } from "./auth-api.js";
import type {
  EmailChangeRequest,
  EmailChangeResponse,
  UserProfile,
} from "./types.js";

/** GET /api/v1/users/me — the current user's full profile. */
export function fetchCurrentUserProfile(
  opts?: RequestOptions,
): Promise<UserProfile> {
  return request<UserProfile>("GET", "/users/me", undefined, {
    signal: opts?.signal,
  });
}

/**
 * PUT /api/v1/users/me/email — change the account email.
 * Current-password re-authentication; 200 returns the refreshed profile
 * (the new address starts unverified). CSRF'd like every mutation, with
 * the auth-action deadline as its default signal.
 */
export function changeEmail(
  body: EmailChangeRequest,
  opts?: RequestOptions,
): Promise<EmailChangeResponse> {
  return request<EmailChangeResponse>("PUT", "/users/me/email", body, {
    needsCSRF: true,
    signal: opts?.signal ?? AbortSignal.timeout(AUTH_ACTION_TIMEOUT_MS),
  });
}

/**
 * PUT /api/v1/users/me/avatar — self-service avatar upload.
 * One step: the server processes the file (200×200 square crop PNG) and
 * updates the reference; the 200 body is the refreshed profile with the
 * absolute served avatarUrl.
 */
export function uploadAvatar(
  file: File,
  opts?: RequestOptions,
): Promise<UserProfile> {
  const form = new FormData();
  form.append("file", file, file.name);
  return requestForm<UserProfile>("/users/me/avatar", form, {
    method: "PUT",
    signal: opts?.signal,
  });
}
