/**
 * Projects endpoint functions — the public wire contract (GET
 * /api/v1/projects + GET /api/v1/projects/{id}), built on
 * the shared transport (shared/api/client.ts: request + problem parsing).
 * The transport stays feature-free.
 */

import { request, type RequestOptions } from "@shared/api/client.js";
import { API_REQUEST_TIMEOUT_MS } from "@shared/api/request-signal.js";

import type { ProjectDetail, ProjectList } from "./types.js";

export interface ListProjectsParams {
  /** Page size 1–100. Defaults to the API default when omitted. */
  limit?: number;
  /** Opaque cursor from a previous pageInfo.endCursor (pv1. namespace). */
  after?: string;
}

/**
 * GET /api/v1/projects — public keyset-paginated list of published
 * projects, newest first. No CSRF (public GET), default 401
 * auto-transition applies.
 */
export function getProjects(
  params: ListProjectsParams = {},
  opts?: RequestOptions,
): Promise<ProjectList> {
  const search = new URLSearchParams();
  if (params.limit !== undefined) {
    search.set("limit", String(params.limit));
  }
  if (params.after) {
    search.set("after", params.after);
  }
  const query = search.size > 0 ? `?${search.toString()}` : "";

  return request<ProjectList>("GET", `/projects${query}`, undefined, {
    signal: opts?.signal ?? AbortSignal.timeout(API_REQUEST_TIMEOUT_MS),
  });
}

// ── Detail ─────────────────────────────────────────────────────────

/**
 * GET /api/v1/projects/{id} — public detail of one published project.
 *
 * Unknown ids and masked (unpublished) projects both answer a 404
 * problem; callers distinguish the deliberate not-found state via the
 * ApiError type "/problems/not-found". No CSRF (public GET).
 */
export function getProject(
  id: string,
  opts?: RequestOptions,
): Promise<ProjectDetail> {
  // The id is a path segment; encoding keeps any reserved character from
  // changing the request URL's meaning. RFC 3986 unreserved ids (the
  // accepted set) pass through unchanged.
  return request<ProjectDetail>(
    "GET",
    `/projects/${encodeURIComponent(id)}`,
    undefined,
    {
      signal: opts?.signal ?? AbortSignal.timeout(API_REQUEST_TIMEOUT_MS),
    },
  );
}
