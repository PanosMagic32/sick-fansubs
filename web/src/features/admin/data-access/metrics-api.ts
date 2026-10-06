/**
 * Staff dashboard metrics endpoint — the
 * live totals and last-30-days activity for the dashboard's "Στατιστικά" tab.
 *
 * Wire facts:
 *  - Floor: a moderator+ session (the server rejects below-moderator with
 *    403 and unauthenticated with the generic 401).
 *  - Every number is server-derived from live tables and the audit ledger —
 *    nothing is stored, so each request returns fresh values.
 *  - The endpoint accepts no query parameters; any parameter answers 400.
 */

import { request, type RequestOptions } from "@shared/api/client.js";

/** One count per accepted role (camelCase key, the role value stays `super-admin`). */
export interface StaffMetricsRoleCounts {
  user: number;
  moderator: number;
  admin: number;
  superAdmin: number;
}

/** One count per accepted account status. */
export interface StaffMetricsStatusCounts {
  active: number;
  suspended: number;
}

export interface StaffMetricsUserTotals {
  total: number;
  byRole: StaffMetricsRoleCounts;
  byStatus: StaffMetricsStatusCounts;
}

/** One content type's status breakdown — archived included, so total is the sum of its parts. */
export interface StaffMetricsContentTotals {
  total: number;
  published: number;
  draft: number;
  archived: number;
}

export interface StaffMetricsTotals {
  users: StaffMetricsUserTotals;
  blogPosts: StaffMetricsContentTotals;
  projects: StaffMetricsContentTotals;
  /** Blog-post comments plus project comments. */
  comments: number;
  /** Blog-post favorites plus project favorites. */
  favorites: number;
}

/**
 * The last-30-days window. `since` is the window's opening instant (the same
 * instant the counts used); the change counters cover successful audit events
 * only, except signInFailures.
 */
export interface StaffMetricsActivity {
  since: string;
  registrations: number;
  activeUsers: number;
  signInFailures: number;
  contentCreated: number;
  contentUpdated: number;
  contentDeleted: number;
  commentDeletions: number;
  usersSuspended: number;
  usersReactivated: number;
  usersDeleted: number;
  roleChanges: number;
}

export interface StaffMetrics {
  totals: StaffMetricsTotals;
  activity30d: StaffMetricsActivity;
}

/** GET /api/v1/staff/metrics — the dashboard's live metrics (moderator+). */
export function getStaffMetrics(opts?: RequestOptions): Promise<StaffMetrics> {
  return request<StaffMetrics>("GET", "/staff/metrics", undefined, {
    signal: opts?.signal,
  });
}
