/**
 * Staff logs endpoint — the tail of the
 * application's rotated log file for the dashboard's log tab.
 *
 * Wire facts:
 *  - Floor: a super-admin session (the server rejects every lower role with
 *    403 and unauthenticated with the generic 401).
 *  - It is a bounded TAIL read, not a keyset collection: the response carries
 *    `items` only — no `pageInfo`, no `after`, no `total`. The server has no
 *    cursor to hand back, and a larger view is a larger `limit`.
 *  - `level` is a MINIMUM severity, `q` a case-insensitive substring over the
 *    whole record, `limit` the last-N cap (1–1000).
 */

import { request, type RequestOptions } from "@shared/api/client.js";

/** The severity vocabulary the server accepts (and returns, lower-cased). */
export const LOG_LEVELS = ["debug", "info", "warn", "error"] as const;

/** The `limit` bounds and the default the UI mirrors (server default 100). */
export const LOG_LIMIT_MIN = 1;
export const LOG_LIMIT_MAX = 1000;
export const DEFAULT_LOG_LIMIT = 100;

/** The `q` bound in characters — the server's `maxStaffLogQueryLen`. */
export const LOG_QUERY_MAX = 256;

/** The default floor of the view — the server's own default. */
export const DEFAULT_LOG_LEVEL = "warn";

export type StaffLogLevel = (typeof LOG_LEVELS)[number];

/** One log record: the reserved fields parsed out, everything else in `fields`. */
export interface StaffLogItem {
  /** Canonical UTC RFC 3339 instant with milliseconds. */
  time: string;
  level: StaffLogLevel;
  msg: string;
  /** Every remaining structured attribute, exactly as the process wrote it. */
  fields: Record<string, unknown>;
}

/** The GET /api/v1/staff/logs body. A fresh install answers `items: []`. */
export interface StaffLogs {
  items: StaffLogItem[];
}

export interface StaffLogsQuery {
  /** Minimum severity; the server defaults to warn. */
  level?: StaffLogLevel;
  /** Case-insensitive substring over the raw record; empty means no filter. */
  q?: string;
  /** How many of the newest matching records to return (1–1000). */
  limit?: number;
}

/** GET /api/v1/staff/logs — the newest matching log records (super-admin). */
export function getStaffLogs(
  query: StaffLogsQuery = {},
  opts?: RequestOptions,
): Promise<StaffLogs> {
  const params = new URLSearchParams();
  if (query.level !== undefined) params.set("level", query.level);
  if (query.q) params.set("q", query.q);
  if (query.limit !== undefined) params.set("limit", String(query.limit));

  const suffix = params.toString();

  return request<StaffLogs>(
    "GET",
    suffix ? `/staff/logs?${suffix}` : "/staff/logs",
    undefined,
    { signal: opts?.signal },
  );
}
