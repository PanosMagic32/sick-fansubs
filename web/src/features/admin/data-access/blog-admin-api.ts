/**
 * Blog content-administration endpoints —
 * built on the shared transport like the other data-access modules.
 *
 * Wire facts:
 *  - Every mutation needs CSRF; update/delete additionally carry the
 *    revision-backed If-Match precondition. The client derives it from the
 *    staff detail body's `revision` field — the same value the ETag header
 *    carries (the transport drops response headers, so the body field is
 *    the client's source; the wire contract stays intact).
 *  - 428/412 problems surface as ApiError with the problem types
 *    /problems/precondition-required|precondition-failed.
 */

import { request, type RequestOptions } from "@shared/api/client.js";

export type BlogStatus = "draft" | "published" | "archived";

/** One row of the write DTO's downloads array. */
export interface BlogDownloadWrite {
  resolution: string;
  magnetUrl: string | null;
  torrentUrl: string | null;
}

/** One stored download row as the staff detail returns it (links verbatim). */
export interface StaffBlogDownload {
  resolution: string;
  magnetUrl: string | null;
  torrentUrl: string | null;
}

export interface BlogPostWrite {
  title: string;
  subtitle: string;
  description: string;
  thumbnailPath: string;
  status: BlogStatus;
  downloads: BlogDownloadWrite[];
}

export interface UserRef {
  id: string;
  username: string;
  avatarUrl: string | null;
}

export interface StaffBlogPost {
  id: string;
  title: string;
  subtitle: string;
  description: string;
  thumbnailPath: string;
  thumbnailUrl: string;
  status: BlogStatus;
  publishedAt: string | null;
  createdAt: string;
  updatedAt: string;
  revision: number;
  creator: UserRef | null;
  updater: UserRef | null;
  downloads: StaffBlogDownload[];
}

export interface StaffBlogPostSummary {
  id: string;
  title: string;
  subtitle: string;
  description: string;
  thumbnailUrl: string;
  status: BlogStatus;
  publishedAt: string | null;
  updatedAt: string;
  revision: number;
  creator: UserRef | null;
}

export interface StaffBlogPostList {
  items: StaffBlogPostSummary[];
  pageInfo: { hasNextPage: boolean; endCursor: string | null; total: number };
}

/** POST /api/v1/blog-posts — create (admin+, CSRF). */
export function createBlogPost(
  body: BlogPostWrite,
  opts?: RequestOptions,
): Promise<StaffBlogPost> {
  return request<StaffBlogPost>("POST", "/blog-posts", body, {
    needsCSRF: true,
    signal: opts?.signal,
  });
}

/** GET /api/v1/admin/blog-posts — staff keyset list (moderator+).
 * `status` and `q` are the optional narrowing filters: an exact status, and
 * a word-wise AND of folded title words (each query word must appear in the
 * folded title). Both are omitted when empty — the server reads a blank
 * value as "no filter" too. */
export function listStaffBlogPosts(
  params: { limit: number; after?: string; status?: BlogStatus; q?: string },
  opts?: RequestOptions,
): Promise<StaffBlogPostList> {
  const query = new URLSearchParams({ limit: String(params.limit) });
  if (params.after) query.set("after", params.after);
  if (params.status) query.set("status", params.status);
  if (params.q) query.set("q", params.q);
  return request<StaffBlogPostList>(
    "GET",
    `/admin/blog-posts?${query.toString()}`,
    undefined,
    { signal: opts?.signal },
  );
}

/** GET /api/v1/admin/blog-posts/{id} — staff detail (moderator+, §2 gate). */
export function getStaffBlogPost(
  id: string,
  opts?: RequestOptions,
): Promise<StaffBlogPost> {
  return request<StaffBlogPost>(
    "GET",
    `/admin/blog-posts/${encodeURIComponent(id)}`,
    undefined,
    { signal: opts?.signal },
  );
}

/** PUT /api/v1/blog-posts/{id} — update (moderator+, If-Match, CSRF). */
export function updateBlogPost(
  id: string,
  revision: number,
  body: BlogPostWrite,
  opts?: RequestOptions,
): Promise<StaffBlogPost> {
  return request<StaffBlogPost>(
    "PUT",
    `/blog-posts/${encodeURIComponent(id)}`,
    body,
    {
      needsCSRF: true,
      signal: opts?.signal,
      extraHeaders: { "If-Match": `"${revision}"` },
    },
  );
}

/** DELETE /api/v1/blog-posts/{id} — hard-delete (admin+, If-Match, CSRF). */
export function deleteBlogPost(
  id: string,
  revision: number,
  opts?: RequestOptions,
): Promise<void> {
  return request<void>(
    "DELETE",
    `/blog-posts/${encodeURIComponent(id)}`,
    undefined,
    {
      needsCSRF: true,
      signal: opts?.signal,
      extraHeaders: { "If-Match": `"${revision}"` },
    },
  );
}
