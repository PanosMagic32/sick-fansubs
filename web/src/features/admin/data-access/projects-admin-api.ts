/**
 * Project content-administration endpoints —
 * the blog-admin module's counterpart for projects.
 *
 * Wire facts:
 *  - The CREATE body has NO slug field (the server generates it from the
 *    title); the UPDATE body's slug is optional and admin+-only — the form
 *    omits it for moderators, and the server 403s a moderator-submitted
 *    slug.
 *  - Every mutation needs CSRF; update/delete additionally carry the
 *    revision-backed If-Match precondition (derived from the staff detail
 *    body's `revision` field — same policy as blog-admin-api).
 *  - 428/412 problems surface as ApiError with the problem types
 *    /problems/precondition-required|precondition-failed.
 */

import { request, type RequestOptions } from "@shared/api/client.js";
import type { UserRef } from "./blog-admin-api.js";

export type ProjectStatus = "draft" | "published" | "archived";

/** One row of the write DTO's downloads array. */
export interface ProjectDownloadWrite {
  name: string;
  magnetUrl: string | null;
  torrentUrl: string | null;
}

/** One stored download row as the staff detail returns it (links verbatim). */
export interface StaffProjectDownload {
  name: string;
  magnetUrl: string | null;
  torrentUrl: string | null;
}

/** The create body (no slug — generated server-side). */
export interface ProjectCreateBody {
  title: string;
  description: string;
  thumbnailPath: string;
  status: ProjectStatus;
  downloads: ProjectDownloadWrite[];
}

/** The update body: slug optional (admin+); absent keeps the existing. */
export interface ProjectUpdateBody {
  title: string;
  description: string;
  slug?: string;
  thumbnailPath: string;
  status: ProjectStatus;
  downloads: ProjectDownloadWrite[];
}

export interface StaffProject {
  id: string;
  title: string;
  description: string;
  slug: string;
  thumbnailPath: string;
  thumbnailUrl: string;
  status: ProjectStatus;
  publishedAt: string | null;
  createdAt: string;
  updatedAt: string;
  revision: number;
  creator: UserRef | null;
  updater: UserRef | null;
  downloads: StaffProjectDownload[];
}

export interface StaffProjectSummary {
  id: string;
  title: string;
  description: string;
  slug: string;
  thumbnailUrl: string | null;
  status: ProjectStatus;
  publishedAt: string | null;
  updatedAt: string;
  revision: number;
  creator: UserRef | null;
}

export interface StaffProjectList {
  items: StaffProjectSummary[];
  pageInfo: { hasNextPage: boolean; endCursor: string | null; total: number };
}

/** POST /api/v1/projects — create (admin+, CSRF); slug generated. */
export function createProject(
  body: ProjectCreateBody,
  opts?: RequestOptions,
): Promise<StaffProject> {
  return request<StaffProject>("POST", "/projects", body, {
    needsCSRF: true,
    signal: opts?.signal,
  });
}

/** GET /api/v1/admin/projects — staff keyset list (moderator+).
 * `status` and `q` are the optional narrowing filters: an exact status,
 * and a folded-title match in which every word of `q` must appear. Both
 * are omitted when empty — the server reads a blank value as "no filter"
 * too. */
export function listStaffProjects(
  params: { limit: number; after?: string; status?: ProjectStatus; q?: string },
  opts?: RequestOptions,
): Promise<StaffProjectList> {
  const query = new URLSearchParams({ limit: String(params.limit) });
  if (params.after) query.set("after", params.after);
  if (params.status) query.set("status", params.status);
  if (params.q) query.set("q", params.q);
  return request<StaffProjectList>(
    "GET",
    `/admin/projects?${query.toString()}`,
    undefined,
    { signal: opts?.signal },
  );
}

/** GET /api/v1/admin/projects/{id} — staff detail (moderator+, §2 gate). */
export function getStaffProject(
  id: string,
  opts?: RequestOptions,
): Promise<StaffProject> {
  return request<StaffProject>(
    "GET",
    `/admin/projects/${encodeURIComponent(id)}`,
    undefined,
    { signal: opts?.signal },
  );
}

/** PUT /api/v1/projects/{id} — update (moderator+, If-Match, CSRF; slug admin+). */
export function updateProject(
  id: string,
  revision: number,
  body: ProjectUpdateBody,
  opts?: RequestOptions,
): Promise<StaffProject> {
  return request<StaffProject>(
    "PUT",
    `/projects/${encodeURIComponent(id)}`,
    body,
    {
      needsCSRF: true,
      signal: opts?.signal,
      extraHeaders: { "If-Match": `"${revision}"` },
    },
  );
}

/** DELETE /api/v1/projects/{id} — hard-delete (admin+, If-Match, CSRF). */
export function deleteProject(
  id: string,
  revision: number,
  opts?: RequestOptions,
): Promise<void> {
  return request<void>(
    "DELETE",
    `/projects/${encodeURIComponent(id)}`,
    undefined,
    {
      needsCSRF: true,
      signal: opts?.signal,
      extraHeaders: { "If-Match": `"${revision}"` },
    },
  );
}
