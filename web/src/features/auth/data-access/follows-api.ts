/**
 * Content-follow endpoint functions — built
 * on the shared transport like favorites-api.ts.
 *
 * Wire facts:
 *  - Floor: AUTHENTICATED — the server rejects anonymous with the generic
 *    401.
 *  - GET is a safe read: no CSRF, no 401 suppression (a follow-status 401
 *    is a genuinely dead session — the global 401→anonymous transition
 *    heals it).
 *  - PUT/DELETE require CSRF (the client attaches X-CSRF-Token
 *    automatically via needsCSRF).
 *  - PUT is idempotent on the server (204 on repeat); DELETE is 204
 *    ALWAYS. Unknown/unpublished ids answer a MASKED 404 on GET/PUT (the
 *    toggle treats it as a load/action failure), never a data leak.
 *
 * Cancellation/timeout: callers own their AbortController; the toggle
 * element passes its own.
 */

import { request, type RequestOptions } from "@shared/api/client.js";

/** The two followable content kinds (the URL segment names). */
export type FollowKind = "blog-posts" | "projects";

/** GET /api/v1/users/me/follows/{kind}/{id} — is the content followed? */
export function followStatus(
  kind: FollowKind,
  contentId: string,
  opts?: RequestOptions,
): Promise<{ following: boolean }> {
  return request<{ following: boolean }>(
    "GET",
    `/users/me/follows/${kind}/${encodeURIComponent(contentId)}`,
    undefined,
    { signal: opts?.signal },
  );
}

/** PUT /api/v1/users/me/follows/{kind}/{id} — add (idempotent, 204). */
export function addFollow(
  kind: FollowKind,
  contentId: string,
  opts?: RequestOptions,
): Promise<void> {
  return request<void>(
    "PUT",
    `/users/me/follows/${kind}/${encodeURIComponent(contentId)}`,
    undefined,
    { needsCSRF: true, signal: opts?.signal },
  );
}

/** DELETE /api/v1/users/me/follows/{kind}/{id} — remove (always 204). */
export function removeFollow(
  kind: FollowKind,
  contentId: string,
  opts?: RequestOptions,
): Promise<void> {
  return request<void>(
    "DELETE",
    `/users/me/follows/${kind}/${encodeURIComponent(contentId)}`,
    undefined,
    { needsCSRF: true, signal: opts?.signal },
  );
}
