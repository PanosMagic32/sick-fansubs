/**
 * Search endpoint function — the GET /api/v1/search wire contract, built on
 * the shared transport (shared/api/client.ts: request + problem parsing).
 * The transport stays feature-free.
 */

import { request, type RequestOptions } from "@shared/api/client.js";
import { API_REQUEST_TIMEOUT_MS } from "@shared/api/request-signal.js";

import type { SearchResultList, SearchSort, SearchType } from "./types.js";

export interface SearchContentParams {
  /** The search text (trimmed by the page before sending); required — the
   * server answers 422 for a missing or blank q. */
  q: string;
  /** Content-type scope; omitted (or "all") means both types. */
  type?: SearchType;
  /** Result ordering; omitted (or "date") means newest first. */
  sort?: SearchSort;
  /** Lower publication-date bound as YYYY-MM-DD (UTC day, inclusive). */
  from?: string;
  /** Upper publication-date bound as YYYY-MM-DD (UTC day, inclusive). */
  to?: string;
  /** Page size 1–100. */
  limit?: number;
  /** Opaque cursor from a previous pageInfo.endCursor — only meaningful
   * with the same q+type+sort+window that produced it, and each sort's
   * cursor is rejected under the other sort. */
  after?: string;
}

/**
 * GET /api/v1/search — public merged search, newest first by default.
 * No CSRF (public GET), default 401 auto-transition applies.
 */
export function searchContent(
  params: SearchContentParams,
  opts?: RequestOptions,
): Promise<SearchResultList> {
  const query = new URLSearchParams();
  query.set("q", params.q);
  if (params.type !== undefined && params.type !== "all") {
    query.set("type", params.type);
  }
  if (params.sort !== undefined && params.sort !== "date") {
    query.set("sort", params.sort);
  }
  if (params.from) {
    query.set("from", params.from);
  }
  if (params.to) {
    query.set("to", params.to);
  }
  if (params.limit !== undefined) {
    query.set("limit", String(params.limit));
  }
  if (params.after) {
    query.set("after", params.after);
  }

  return request<SearchResultList>(
    "GET",
    `/search?${query.toString()}`,
    undefined,
    {
      signal: opts?.signal ?? AbortSignal.timeout(API_REQUEST_TIMEOUT_MS),
    },
  );
}
