/**
 * Favorites endpoint functions — built on the
 * shared transport like profile-api.ts and auth-api.ts.
 *
 * Wire facts:
 *  - GETs are safe reads: no CSRF, no 401 suppression (a favorites 401 is
 *    a genuinely dead session — the global 401→anonymous transition heals
 *    it, and the account page's guard redirects).
 *  - PUT/DELETE require CSRF (the client attaches X-CSRF-Token
 *    automatically via needsCSRF).
 *  - Every mutation is idempotent on the server (204 on repeat), so the
 *    client needs no retry/de-dup logic.
 *
 * Cancellation/timeout: callers own their AbortController (composed with
 * the shared requestSignal timeout for page-level reads); the toggle
 * element passes its own.
 */

import { request, type RequestOptions } from "@shared/api/client.js";

import type {
  FavoriteKind,
  FavoriteListResponse,
  FavoriteStatusResponse,
} from "./types.js";

/** GET /api/v1/users/me/favorites/{kind} — keyset page of favorites. */
export function listFavorites(
  kind: FavoriteKind,
  params: { limit: number; after?: string },
  opts?: RequestOptions,
): Promise<FavoriteListResponse> {
  const query = new URLSearchParams({ limit: String(params.limit) });
  if (params.after) query.set("after", params.after);
  return request<FavoriteListResponse>(
    "GET",
    `/users/me/favorites/${kind}?${query.toString()}`,
    undefined,
    { signal: opts?.signal },
  );
}

/** GET /api/v1/users/me/favorites/{kind}/{id} — is it favorited? */
export function favoriteStatus(
  kind: FavoriteKind,
  contentId: string,
  opts?: RequestOptions,
): Promise<FavoriteStatusResponse> {
  return request<FavoriteStatusResponse>(
    "GET",
    `/users/me/favorites/${kind}/${encodeURIComponent(contentId)}`,
    undefined,
    { signal: opts?.signal },
  );
}

/** PUT /api/v1/users/me/favorites/{kind}/{id} — add (idempotent, 204). */
export function addFavorite(
  kind: FavoriteKind,
  contentId: string,
  opts?: RequestOptions,
): Promise<void> {
  return request<void>(
    "PUT",
    `/users/me/favorites/${kind}/${encodeURIComponent(contentId)}`,
    undefined,
    { needsCSRF: true, signal: opts?.signal },
  );
}

/** DELETE /api/v1/users/me/favorites/{kind}/{id} — remove (idempotent, 204). */
export function removeFavorite(
  kind: FavoriteKind,
  contentId: string,
  opts?: RequestOptions,
): Promise<void> {
  return request<void>(
    "DELETE",
    `/users/me/favorites/${kind}/${encodeURIComponent(contentId)}`,
    undefined,
    { needsCSRF: true, signal: opts?.signal },
  );
}
