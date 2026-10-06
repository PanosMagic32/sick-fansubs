/**
 * Comments endpoint functions — built on the shared
 * transport like the other data-access modules.
 *
 * Wire facts:
 *  - The thread list and the count are PUBLIC reads (the session is
 *    optional server-side — `hearted` resolves when present, no CSRF, no
 *    401 suppression: the anonymous 401 never happens here and a dead
 *    session heals through the global transition).
 *  - Every write (create, reply, update, delete, heart) requires CSRF
 *    (the client attaches X-CSRF-Token automatically via needsCSRF).
 *  - Create/reply return NO body — the 201 Location header is the
 *    deep-link resource (`?focus=<new id>`). The transport's
 *    onResponse hook captures it; the parsed focus id is what the thread
 *    uses to jump to the new comment.
 *  - Heart PUT/DELETE are idempotent 204s (the favorites pattern).
 *  - Cancellation/timeout: page-level reads pass their own composed
 *    signal; the quick mutations pass no signal (the favorites toggle
 *    precedent).
 */

import { request, type RequestOptions } from "@shared/api/client.js";

import type {
  CommentItem,
  CommentKind,
  CommentList,
  CommentSort,
} from "./types.js";

/** The thread's keyset page size — 20 top-level comments. */
export const COMMENT_PAGE_SIZE = 20;

/** The replies operation's page size — the
 * append after the item's inline window of 3. A CLIENT page-size choice, so
 * it is deliberately its own constant rather than reusing
 * COMMENT_PAGE_SIZE; the server default (20) does not dictate it. */
export const REPLY_PAGE_SIZE = 20;

/** The list+count base path for one content id. */
function threadBase(kind: CommentKind, contentId: string): string {
  return `/${kind}/${encodeURIComponent(contentId)}/comments`;
}

/**
 * Parse the new comment's id out of a 201 Location header
 * (`…/comments?focus=<id>`). Never throws — a missing/unparseable header
 * degrades to null (the thread then just reloads without a jump).
 */
export function focusFromLocation(location: string | null): string | null {
  if (!location) return null;
  try {
    // Parse the query directly — no base-URL resolution, so the result
    // never depends on the page's current location (happy-dom's isolated
    // test runs start at about:blank, which would throw on new URL).
    const query = location.slice(location.indexOf("?") + 1);
    return new URLSearchParams(query).get("focus");
  } catch {
    return null;
  }
}

/** The list params. `after` and `focus` are mutually exclusive on the
 * server (strict-params 400) — the union makes sending both a
 * type error instead of a convention. */
export type CommentListParams =
  | { sort: CommentSort; limit: number; after: string; focus?: never }
  | { sort: CommentSort; limit: number; after?: never; focus?: string };

/** GET /{kind}/{id}/comments — one keyset page of the thread. */
export function listComments(
  kind: CommentKind,
  contentId: string,
  params: CommentListParams,
  opts?: RequestOptions,
): Promise<CommentList> {
  const query = new URLSearchParams({
    sort: params.sort,
    limit: String(params.limit),
  });
  if (params.after) query.set("after", params.after);
  if (params.focus) query.set("focus", params.focus);
  return request<CommentList>(
    "GET",
    `${threadBase(kind, contentId)}?${query.toString()}`,
    undefined,
    { signal: opts?.signal },
  );
}

/** GET /{kind}/{id}/comments/count — top-level + replies. */
export function commentCount(
  kind: CommentKind,
  contentId: string,
  opts?: RequestOptions,
): Promise<{ count: number }> {
  return request<{ count: number }>(
    "GET",
    `${threadBase(kind, contentId)}/count`,
    undefined,
    { signal: opts?.signal },
  );
}

export interface CommentRepliesParams {
  limit: number;
  /** The item's `repliesEndCursor` (opaque, server-minted) or a previous
   * reply page's `pageInfo.endCursor`. */
  after: string;
}

/**
 * GET /{kind}/{id}/comments/{commentId}/replies — one ascending keyset page
 * of a top-level comment's replies. Public
 * like the thread list (the optional session fills `hearted`); the cursor
 * namespace is br1./pr1., so a thread cursor is rejected server-side.
 */
export function listReplies(
  kind: CommentKind,
  contentId: string,
  commentId: string,
  params: CommentRepliesParams,
  opts?: RequestOptions,
): Promise<CommentList> {
  const query = new URLSearchParams({
    limit: String(params.limit),
    after: params.after,
  });
  return request<CommentList>(
    "GET",
    `${threadBase(kind, contentId)}/${encodeURIComponent(commentId)}/replies?${query.toString()}`,
    undefined,
    { signal: opts?.signal },
  );
}

/** The parsed create/reply result — the new comment's deep-link id. */
export interface CommentCreateResult {
  focusId: string | null;
}

/** The shared create/reply POST: CSRF + body, NO response body — the 201
 * Location's focus id is the deep-link resource. Extracted so
 * the two call sites cannot drift on the Location capture. */
async function postWithLocation(
  path: string,
  body: string,
  opts?: RequestOptions,
): Promise<CommentCreateResult> {
  let location: string | null = null;
  await request<void>(
    "POST",
    path,
    { body },
    {
      needsCSRF: true,
      signal: opts?.signal,
      onResponse: (res) => {
        location = res.headers.get("Location");
      },
    },
  );
  return { focusId: focusFromLocation(location) };
}

/** POST /{kind}/{id}/comments — top-level comment (201 + Location). */
export function createComment(
  kind: CommentKind,
  contentId: string,
  body: string,
  opts?: RequestOptions,
): Promise<CommentCreateResult> {
  return postWithLocation(threadBase(kind, contentId), body, opts);
}

/** POST /{kind}/{id}/comments/{commentId}/replies — reply to a TOP-LEVEL
 * comment (201 + Location; reply-to-reply is a server 422). */
export function replyToComment(
  kind: CommentKind,
  contentId: string,
  commentId: string,
  body: string,
  opts?: RequestOptions,
): Promise<CommentCreateResult> {
  return postWithLocation(
    `${threadBase(kind, contentId)}/${encodeURIComponent(commentId)}/replies`,
    body,
    opts,
  );
}

/** PATCH /{kind}/{id}/comments/{commentId} — author only (200 + item). */
export function updateComment(
  kind: CommentKind,
  contentId: string,
  commentId: string,
  body: string,
  opts?: RequestOptions,
): Promise<CommentItem> {
  return request<CommentItem>(
    "PATCH",
    `${threadBase(kind, contentId)}/${encodeURIComponent(commentId)}`,
    { body },
    { needsCSRF: true, signal: opts?.signal },
  );
}

/** DELETE /{kind}/{id}/comments/{commentId} — author or moderator+;
 * hard cascade, 204. */
export function deleteComment(
  kind: CommentKind,
  contentId: string,
  commentId: string,
  opts?: RequestOptions,
): Promise<void> {
  return request<void>(
    "DELETE",
    `${threadBase(kind, contentId)}/${encodeURIComponent(commentId)}`,
    undefined,
    { needsCSRF: true, signal: opts?.signal },
  );
}

/** PUT /{kind}/{id}/comments/{commentId}/heart — idempotent 204. */
export function setHeart(
  kind: CommentKind,
  contentId: string,
  commentId: string,
  opts?: RequestOptions,
): Promise<void> {
  return request<void>(
    "PUT",
    `${threadBase(kind, contentId)}/${encodeURIComponent(commentId)}/heart`,
    undefined,
    { needsCSRF: true, signal: opts?.signal },
  );
}

/** DELETE /{kind}/{id}/comments/{commentId}/heart — idempotent 204. */
export function removeHeart(
  kind: CommentKind,
  contentId: string,
  commentId: string,
  opts?: RequestOptions,
): Promise<void> {
  return request<void>(
    "DELETE",
    `${threadBase(kind, contentId)}/${encodeURIComponent(commentId)}/heart`,
    undefined,
    { needsCSRF: true, signal: opts?.signal },
  );
}
