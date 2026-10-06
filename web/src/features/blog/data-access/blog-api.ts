/**
 * Blog endpoint functions — the wire contract (GET /api/v1/blog-posts),
 * built on the shared transport (shared/api/client.ts: request + problem
 * parsing). The transport stays feature-free.
 */

import { request, type RequestOptions } from "@shared/api/client.js";
import { API_REQUEST_TIMEOUT_MS } from "@shared/api/request-signal.js";

import type { BlogPostDetail, BlogPostList } from "./types.js";

export interface ListBlogPostsParams {
  /** Page size 1–100. Defaults to the API default when omitted. */
  limit?: number;
  /** Opaque cursor from a previous pageInfo.endCursor. */
  after?: string;
}

/**
 * GET /api/v1/blog-posts — public keyset-paginated list, newest first.
 * No CSRF (public GET), default 401 auto-transition applies.
 */
export function listBlogPosts(
  params: ListBlogPostsParams = {},
  opts?: RequestOptions,
): Promise<BlogPostList> {
  const search = new URLSearchParams();
  if (params.limit !== undefined) {
    search.set("limit", String(params.limit));
  }
  if (params.after) {
    search.set("after", params.after);
  }
  const query = search.size > 0 ? `?${search.toString()}` : "";

  return request<BlogPostList>("GET", `/blog-posts${query}`, undefined, {
    signal: opts?.signal ?? AbortSignal.timeout(API_REQUEST_TIMEOUT_MS),
  });
}

// ── Detail ────────────────────────────────────────────────────────

/**
 * GET /api/v1/blog-posts/{id} — public detail of one published post.
 *
 * Unknown ids and masked (draft/archived/unstamped) posts both answer a
 * 404 problem; callers distinguish the deliberate not-found state via the
 * ApiError type "/problems/not-found". No CSRF (public GET).
 */
export function getBlogPost(
  id: string,
  opts?: RequestOptions,
): Promise<BlogPostDetail> {
  // The id is a path segment; encoding keeps any reserved character from
  // changing the request URL's meaning. RFC 3986 unreserved ids (the
  // accepted set) pass through unchanged.
  return request<BlogPostDetail>(
    "GET",
    `/blog-posts/${encodeURIComponent(id)}`,
    undefined,
    {
      signal: opts?.signal ?? AbortSignal.timeout(API_REQUEST_TIMEOUT_MS),
    },
  );
}
