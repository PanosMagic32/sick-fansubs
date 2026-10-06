/**
 * Staff logs API suite — path, method, and the
 * query-string shape. Transport semantics live in shared/api/client.test.ts.
 */

import { afterEach, describe, expect, test } from "bun:test";

import { mockFetchOnce, restoreMockFetch } from "@shared/api/test-utils.js";

import { getStaffLogs } from "./logs-api.js";

afterEach(() => {
  restoreMockFetch();
});

const LOGS_BODY = {
  items: [
    {
      time: "2026-09-18T10:00:00.000Z",
      level: "warn",
      msg: "staff logs read failed",
      fields: { error: "boom" },
    },
  ],
};

describe("staff logs API", () => {
  test("getStaffLogs GETs /staff/logs with no parameters when none are given", async () => {
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: LOGS_BODY,
    });

    const result = await getStaffLogs();

    expect(req.method).toBe("GET");
    expect(req.url).toBe("/api/v1/staff/logs");
    expect(result.items[0]?.level).toBe("warn");
    expect(result.items[0]?.fields.error).toBe("boom");
  });

  test("level, q, and limit ride the query string", async () => {
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: LOGS_BODY,
    });

    await getStaffLogs({ level: "error", q: "boom", limit: 50 });

    expect(req.url).toBe("/api/v1/staff/logs?level=error&q=boom&limit=50");
  });

  test("an empty q is dropped rather than sent as a blank filter", async () => {
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: LOGS_BODY,
    });

    await getStaffLogs({ level: "info", q: "" });

    expect(req.url).toBe("/api/v1/staff/logs?level=info");
  });
});
