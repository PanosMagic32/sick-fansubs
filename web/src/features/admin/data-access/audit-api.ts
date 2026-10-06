/**
 * Staff audit browser endpoint — the tail of
 * the audit ledger, the second section of the super-admin logs tab.
 *
 * Wire facts:
 *  - Floor: a super-admin session (403 below, the generic 401 anonymous).
 *  - It is a bounded TAIL read like the log viewer: `items` only, newest
 *    first, no cursor and no total.
 *  - `event` is an exact match against the accepted vocabulary; absent or
 *    blank means every kind. The nullable identity columns arrive OMITTED.
 *  - These rows carry the forensic fields (requestId, remoteAddr) the
 *    notifications feed withholds — this is the surface they exist for.
 */

import { request, type RequestOptions } from "@shared/api/client.js";

/**
 * The accepted event vocabulary, in the server's declaration order
 * (`audit.EventNames`). Pinned equal to the Go list by
 * internal/audit/event_names_pin_test.go — add an event in both places.
 */
export const AUDIT_EVENTS = [
  "sign_in_success",
  "sign_in_failure",
  "sign_out",
  "sign_out_all",
  "password_changed",
  "password_reset",
  "role_changed",
  "user_suspended",
  "user_reactivated",
  "user_deleted",
  "content_created",
  "content_updated",
  "content_deleted",
  "comment_deleted",
  "password_reset_requested",
  "password_reset_completed",
  "email_verified",
  "email_changed",
  "session_revoked",
] as const;

export type AuditEventName = (typeof AUDIT_EVENTS)[number];

/** The `limit` bounds and the default the UI mirrors (server default 50). */
export const AUDIT_LIMIT_MIN = 1;
export const AUDIT_LIMIT_MAX = 200;
export const DEFAULT_AUDIT_LIMIT = 50;

/** One ledger row. The identity fields are absent when the column is NULL. */
export interface StaffAuditEvent {
  id: string;
  event: AuditEventName;
  result: "success" | "failure";
  actorId?: string;
  targetId?: string;
  targetRole?: string;
  requestId: string;
  /** The client address, without a port. */
  remoteAddr: string;
  /** Canonical UTC RFC 3339 instant with milliseconds. */
  createdAt: string;
}

/** The GET /api/v1/staff/audit-events body. */
export interface StaffAuditEvents {
  items: StaffAuditEvent[];
}

export interface StaffAuditQuery {
  /** Exact kind; absent or empty means every kind. */
  event?: string;
  /** How many of the newest matching rows to return (1–200). */
  limit?: number;
}

/** GET /api/v1/staff/audit-events — the newest ledger rows (super-admin). */
export function getStaffAuditEvents(
  query: StaffAuditQuery = {},
  opts?: RequestOptions,
): Promise<StaffAuditEvents> {
  const params = new URLSearchParams();
  if (query.event) params.set("event", query.event);
  if (query.limit !== undefined) params.set("limit", String(query.limit));

  const suffix = params.toString();

  return request<StaffAuditEvents>(
    "GET",
    suffix ? `/staff/audit-events?${suffix}` : "/staff/audit-events",
    undefined,
    { signal: opts?.signal },
  );
}
