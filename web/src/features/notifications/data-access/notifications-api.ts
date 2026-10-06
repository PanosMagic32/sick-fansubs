/**
 * Notifications feed endpoint functions — built on the shared transport like
 * the other data-access modules. The wire types live in `types.ts`; this
 * module owns the request functions and the kind-discriminator guards.
 *
 * Wire facts:
 *  - Floor: AUTHENTICATED (any signed-in user) — the server rejects
 *    anonymous with the generic 401. Non-staff viewers simply get no account
 *    events (the audit half is role-weighted).
 *  - POSTs require CSRF (the client attaches X-CSRF-Token automatically via
 *    needsCSRF) and are idempotent on the server (204 on repeat).
 *  - DELETION removes feed entries. `deleteNotification` removes one entry —
 *    an event row is deleted; a visible audit event gets a per-viewer
 *    dismissal (the ledger row itself survives). `clearReadNotifications`
 *    removes every READ event row; `deleteAllNotifications` empties the whole
 *    feed (every event row deleted, every visible audit event dismissed).
 *    A repeat delete is the masked 404 — the entry is gone.
 */

import { request, type RequestOptions } from "@shared/api/client.js";

import type {
  CommentNotificationItem,
  CommentRemovedNotificationItem,
  ContentUpdatedNotificationItem,
  DraftActivityNotificationItem,
  HeartNotificationItem,
  NewContentNotificationItem,
  NotificationItem,
  NotificationList,
} from "./types.js";

/** The kind discriminator guards (the OpenAPI oneOf mirrors the split). */
export function isComment(
  item: NotificationItem,
): item is CommentNotificationItem {
  return item.kind === "comment_reply" || item.kind === "comment";
}

export function isHeart(item: NotificationItem): item is HeartNotificationItem {
  return item.kind === "heart";
}

export function isCommentRemoved(
  item: NotificationItem,
): item is CommentRemovedNotificationItem {
  return item.kind === "comment_removed";
}

export function isContentUpdated(
  item: NotificationItem,
): item is ContentUpdatedNotificationItem {
  return item.kind === "content_updated";
}

export function isNewContent(
  item: NotificationItem,
): item is NewContentNotificationItem {
  return item.kind === "new_content";
}

export function isDraftActivity(
  item: NotificationItem,
): item is DraftActivityNotificationItem {
  return item.kind === "draft_activity";
}

/** True for the five account audit events — the LEDGER half of the feed.
 * The complement is a user_notifications row. Both halves are deletable:
 * an event row is deleted, a visible ledger row is dismissed for the
 * viewer (the audit record itself survives). */
export function isAccountEvent(item: NotificationItem): boolean {
  return (
    item.kind === "password_reset" ||
    item.kind === "role_changed" ||
    item.kind === "user_suspended" ||
    item.kind === "user_reactivated" ||
    item.kind === "user_deleted"
  );
}

/** GET /api/v1/notifications — keyset page of the unified feed. */
export function listNotifications(
  params: { limit: number; after?: string },
  opts?: RequestOptions,
): Promise<NotificationList> {
  const query = new URLSearchParams({ limit: String(params.limit) });
  if (params.after) query.set("after", params.after);
  return request<NotificationList>(
    "GET",
    `/notifications?${query.toString()}`,
    undefined,
    { signal: opts?.signal },
  );
}

/** GET /api/v1/notifications/unread-count — the header badge count (sums
 * both id spaces). */
export function unreadNotificationCount(
  opts?: RequestOptions,
): Promise<{ unread: number }> {
  return request<{ unread: number }>(
    "GET",
    "/notifications/unread-count",
    undefined,
    { signal: opts?.signal },
  );
}

/** POST /api/v1/notifications/{id}/read — mark one item read across BOTH id
 * spaces (audit events + user_notifications), 204. */
export function markNotificationRead(
  id: string,
  opts?: RequestOptions,
): Promise<void> {
  return request<void>(
    "POST",
    `/notifications/${encodeURIComponent(id)}/read`,
    undefined,
    { needsCSRF: true, signal: opts?.signal },
  );
}

/** POST /api/v1/notifications/read-all — mark every visible item read across
 * both id spaces, 204. */
export function markAllNotificationsRead(opts?: RequestOptions): Promise<void> {
  return request<void>("POST", "/notifications/read-all", undefined, {
    needsCSRF: true,
    signal: opts?.signal,
  });
}

/** DELETE /api/v1/notifications/{id} — remove ONE entry from the viewer's
 * feed, 204. An event row is deleted; a visible audit event is dismissed
 * for the viewer (the ledger keeps the row). Unknown, foreign, invisible,
 * and already-dismissed ids are the masked 404, and a repeat delete of the
 * same id is a 404 too — the entry is gone. */
export function deleteNotification(
  id: string,
  opts?: RequestOptions,
): Promise<void> {
  return request<void>(
    "DELETE",
    `/notifications/${encodeURIComponent(id)}`,
    undefined,
    { needsCSRF: true, signal: opts?.signal },
  );
}

/** POST /api/v1/notifications/delete-all — empty the viewer's feed, 204 and
 * idempotent: every event row is deleted and every visible audit event is
 * dismissed for the viewer. The audit ledger rows themselves stay. */
export function deleteAllNotifications(opts?: RequestOptions): Promise<void> {
  return request<void>("POST", "/notifications/delete-all", undefined, {
    needsCSRF: true,
    signal: opts?.signal,
  });
}

/** POST /api/v1/notifications/clear-read — delete every READ event row of the
 * viewer, 204 and idempotent. Unread entries survive by construction; the
 * audit half of the feed is untouched (delete-all owns the ledger
 * dismissal sweep). */
export function clearReadNotifications(opts?: RequestOptions): Promise<void> {
  return request<void>("POST", "/notifications/clear-read", undefined, {
    needsCSRF: true,
    signal: opts?.signal,
  });
}
