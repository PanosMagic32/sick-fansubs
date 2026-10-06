/**
 * Shared formatter tests — the numeric and long date forms, the relative
 * feed formatter, the failure→copy mapper, and the avatar initial.
 */

import { describe, expect, test } from "bun:test";

import {
  ApiError,
  NetworkError,
  RequestTimeoutError,
} from "@shared/api/client.js";
import { el, problemMessage } from "@shared/catalog/el.js";

import {
  formatDate,
  formatDateOnly,
  formatDateTimeNumeric,
  formatRelativeTime,
  initialOf,
  mapError,
} from "./format.js";

describe("initialOf", () => {
  test("uppercases the first character of a display name", () => {
    expect(initialOf("katakuri")).toBe("K");
    expect(initialOf("Ζορό")).toBe("Ζ");
  });

  test("an empty name renders an empty initial rather than throwing", () => {
    expect(initialOf("")).toBe("");
  });
});

describe("formatDateTimeNumeric", () => {
  /** The local-time parts of an instant — the formatter renders the viewer's
   * clock, so expectations are derived from the same instant rather than
   * hardcoded (a UTC+13 host legitimately sees the next day). */
  function localParts(iso: string): { d: string; m: string; y: string } {
    const at = new Date(iso);
    const pad = (n: number) => String(n).padStart(2, "0");
    return {
      d: pad(at.getDate()),
      m: pad(at.getMonth() + 1),
      y: String(at.getFullYear()),
    };
  }

  test("renders day/month/year as numbers plus the clock", () => {
    const iso = "2025-08-15T18:30:00.000Z";
    const out = formatDateTimeNumeric(iso);
    // Two-digit day and month, four-digit year, slash order, 24-hour time.
    expect(out).toMatch(/^\d{2}\/\d{2}\/\d{4}, \d{2}:\d{2}$/);
    const { d, m, y } = localParts(iso);
    expect(out).toContain(`${d}/${m}/${y}`);
  });

  test("never renders a month word", () => {
    const iso = "2026-01-05T10:00:00.000Z";
    const out = formatDateTimeNumeric(iso);
    // el-GR long form is "Ιανουαρίου", the short form "Ιαν" — neither may
    // reach a list or detail meta line.
    expect(out).not.toContain("Ιανουαρίου");
    expect(out).not.toContain("Ιαν");
    const { d, m, y } = localParts(iso);
    expect(out).toContain(`${d}/${m}/${y}`);
  });

  test("uses the 24-hour clock — no Greek am/pm marker", () => {
    const out = formatDateTimeNumeric("2026-01-05T18:30:00.000Z");
    expect(out).not.toContain("μ.μ.");
    expect(out).not.toContain("π.μ.");
  });

  test("a non-finite instant renders empty rather than throwing", () => {
    expect(formatDateTimeNumeric("not-a-date")).toBe("");
  });
});

describe("long formatters", () => {
  test("format the canonical instant in both forms", () => {
    const iso = "2025-08-15T18:30:00.000Z";
    const local = new Date(iso);
    const day = String(local.getDate());

    const long = formatDate(iso);
    expect(long).toContain(day);
    expect(long).toContain("Αυγούστου");
    expect(long).toContain(String(local.getFullYear()));

    const only = formatDateOnly(iso);
    expect(only).toContain(day);
    expect(only).toContain("Αυγούστου");
  });

  test("a non-finite instant renders empty rather than throwing", () => {
    expect(formatDate("not-a-date")).toBe("");
    expect(formatDateOnly("not-a-date")).toBe("");
  });
});

describe("formatRelativeTime", () => {
  const now = new Date("2026-03-01T12:00:00.000Z");

  test("under a minute reads as the just-now floor", () => {
    expect(formatRelativeTime("2026-03-01T11:59:31.000Z", now)).toBe(
      el.ui.justNow,
    );
  });

  test("future instants keep the relative wording", () => {
    expect(formatRelativeTime("2026-03-01T12:30:00.000Z", now)).toContain(
      "λεπ",
    );
  });

  test("past the 30-day cutoff switches to the absolute date", () => {
    const longAgo = "2026-01-01T12:00:00.000Z";
    expect(formatRelativeTime(longAgo, now)).toBe(formatDateOnly(longAgo));
  });

  test("a non-finite instant renders empty", () => {
    expect(formatRelativeTime("nope", now)).toBe("");
  });
});

describe("mapError", () => {
  test("known problem types resolve through the catalog", () => {
    const err = new ApiError({
      type: "/problems/forbidden",
      title: "Forbidden",
      status: 403,
    });
    expect(mapError(err)).toBe(problemMessage("/problems/forbidden"));
  });

  test("connection failures and timeouts share the connection copy", () => {
    expect(mapError(new NetworkError("down"))).toBe(
      el.ui.serverConnectionFailed,
    );
    expect(mapError(new RequestTimeoutError())).toBe(
      el.ui.serverConnectionFailed,
    );
  });

  test("anything else falls back to the generic error", () => {
    expect(mapError(new Error("boom"))).toBe(el.ui.error);
  });
});
