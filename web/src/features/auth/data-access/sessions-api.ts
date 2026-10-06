/**
 * Self-service session endpoints — built on the shared
 * transport like favorites-api.ts and follows-api.ts.
 *
 * Wire facts:
 *  - GET is a safe read: no CSRF, no 401 suppression (a sessions 401 is a
 *    genuinely dead session — the global 401→anonymous transition heals it
 *    and the account page's guard redirects).
 *  - DELETE requires CSRF (the client attaches X-CSRF-Token automatically
 *    via needsCSRF).
 *  - DELETE is idempotent on the server: 204 for the caller's own session,
 *    for another user's id, and for a repeat call alike. The UI therefore
 *    never branches on the outcome — a successful revoke removes the row.
 *
 * Cancellation/timeout: callers own their AbortController.
 */

import { request, type RequestOptions } from "@shared/api/client.js";

import type { SessionListResponse } from "./types.js";

/**
 * The page size the account page asks for. Per-user session counts are tiny
 * (a handful of browsers); the server default is the same 20, and the load
 * more control only appears when a page actually has a successor.
 */
export const SESSION_PAGE_SIZE = 20;

/** GET /api/v1/users/me/sessions — keyset page of active sessions. */
export function listSessions(
  params: { limit: number; after?: string },
  opts?: RequestOptions,
): Promise<SessionListResponse> {
  const query = new URLSearchParams({ limit: String(params.limit) });
  if (params.after) query.set("after", params.after);
  return request<SessionListResponse>(
    "GET",
    `/users/me/sessions?${query.toString()}`,
    undefined,
    { signal: opts?.signal },
  );
}

/** DELETE /api/v1/users/me/sessions/{id} — revoke (idempotent, 204). */
export function revokeSession(
  id: string,
  opts?: RequestOptions,
): Promise<void> {
  return request<void>(
    "DELETE",
    `/users/me/sessions/${encodeURIComponent(id)}`,
    undefined,
    { needsCSRF: true, signal: opts?.signal },
  );
}
