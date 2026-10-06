/**
 * Shared content-page helpers: the date/time formatters and the
 * failure→Greek-copy mapper. Features never import from other features,
 * so every feature page resolves them through shared/utils/.
 */

import {
  ApiError,
  NetworkError,
  RequestTimeoutError,
} from "@shared/api/client.js";
import { el, problemMessage } from "@shared/catalog/el.js";

// Full date + time in Greek — the long form shared by the admin and
// account surfaces.
const dateFmt = new Intl.DateTimeFormat("el-GR", {
  dateStyle: "long",
  timeStyle: "short",
});

// Long date only (no time) — the account page's "member since" line:
// the account creation instant's DATE is the product-relevant
// part there, unlike content cards.
const dateOnlyFmt = new Intl.DateTimeFormat("el-GR", { dateStyle: "long" });

// Numeric date + time — every content list and the detail pages' meta
// lines: day, month, year as NUMBERS plus the 24-hour clock
// ("15/08/2026, 18:30").
//
// hourCycle is pinned to h23 on purpose: el-GR's default is the 12-hour
// clock with a «μ.μ.» suffix, which the compact form removes (and the
// 24-hour value needs no am/pm disambiguation).
const numericDateTimeFmt = new Intl.DateTimeFormat("el-GR", {
  day: "2-digit",
  month: "2-digit",
  year: "numeric",
  hour: "2-digit",
  minute: "2-digit",
  hourCycle: "h23",
});

/**
 * The avatar initial of a display name: the uppercased first character,
 * shared by the comment thread, the account profile, and the admin member
 * card so the three cannot drift.
 */
export function initialOf(name: string): string {
  return name.charAt(0).toUpperCase();
}

/**
 * Format an API instant (canonical UTC RFC 3339 with exactly millisecond
 * precision) with the Greek long date plus time. A non-finite instant
 * renders the empty string rather than throwing inside the Intl formatter.
 */
export function formatDate(iso: string): string {
  const then = new Date(iso);
  if (!Number.isFinite(then.getTime())) return "";
  return dateFmt.format(then);
}

/**
 * Format an API instant with the Greek long date only — used where the
 * time would be noise (the account page's member-since line). A non-finite
 * instant renders the empty string rather than throwing.
 */
export function formatDateOnly(iso: string): string {
  const then = new Date(iso);
  if (!Number.isFinite(then.getTime())) return "";
  return dateOnlyFmt.format(then);
}

/**
 * Format an API instant as the NUMERIC Greek date with the short time:
 * `15/08/2026, 18:30`. The content lists and the detail pages' meta lines
 * use it — the same instant the long form carries,
 * without the month word and without the seconds. A non-finite instant
 * (never produced by the server contract) renders the empty string rather
 * than throwing inside the Intl formatter.
 */
export function formatDateTimeNumeric(iso: string): string {
  const then = new Date(iso);
  if (!Number.isFinite(then.getTime())) return "";
  return numericDateTimeFmt.format(then);
}

/**
 * Relative-time formatter — the notifications
 * feed's per-item line. Intl.RelativeTimeFormat with the Greek locale
 * yields "πριν από X λεπτά" style output; the threshold switches to the
 * absolute date formatter past ~30 days, where relative wording is noise.
 */
const relativeFmt = new Intl.RelativeTimeFormat("el-GR", { numeric: "auto" });
const RELATIVE_CUTOFF_MS = 30 * 24 * 60 * 60 * 1000;

/**
 * Format an API instant (canonical UTC RFC 3339 with exactly millisecond
 * precision) relative to now: the catalog's just-now floor for under a
 * minute, Greek relative wording up to 30 days, then the absolute
 * date-only form. A non-finite instant (never produced by the server
 * contract) renders the empty string rather than throwing inside the
 * Intl formatter.
 */
export function formatRelativeTime(
  iso: string,
  now: Date = new Date(),
): string {
  const thenMs = new Date(iso).getTime();
  if (!Number.isFinite(thenMs)) return "";
  const diffMs = thenMs - now.getTime();
  const absMs = Math.abs(diffMs);

  if (absMs < 60_000) return el.ui.justNow;
  if (absMs >= RELATIVE_CUTOFF_MS) return dateOnlyFmt.format(thenMs);

  const diffSec = Math.round(diffMs / 1000);
  const absSec = Math.abs(diffSec);
  if (absSec < 3600)
    return relativeFmt.format(Math.round(diffSec / 60), "minute");
  if (absSec < 86_400)
    return relativeFmt.format(Math.round(diffSec / 3600), "hour");
  return relativeFmt.format(Math.round(diffSec / 86_400), "day");
}

/**
 * Map a fetch failure to catalog-resolved Greek copy: known problem types
 * via problemMessage, a failed or timed-out connection via the shared
 * server-connection message, everything else the generic error (raw
 * identifiers never reach the UI).
 */
export function mapError(err: unknown): string {
  if (err instanceof ApiError) return problemMessage(err.type);
  if (err instanceof NetworkError || err instanceof RequestTimeoutError) {
    return el.ui.serverConnectionFailed;
  }
  return el.ui.error;
}
