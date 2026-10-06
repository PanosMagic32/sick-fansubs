/**
 * Sessions endpoint suite — paths, methods, cursor
 * parameters, and the CSRF knobs of the two session functions. Transport
 * semantics live in shared/api/client.test.ts.
 */

import { describe, expect, test, afterEach } from "bun:test";

import { clearCSRFToken, setCSRFToken } from "@shared/api/client.js";
import { mockFetchOnce, restoreMockFetch } from "@shared/api/test-utils.js";

import {
  listSessions,
  revokeSession,
  SESSION_PAGE_SIZE,
} from "./sessions-api.js";

afterEach(() => {
  clearCSRFToken();
  restoreMockFetch();
});

describe("sessions API", () => {
  test("listSessions sends a CSRF-free GET with the limit", async () => {
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: {
        items: [],
        pageInfo: { hasNextPage: false, endCursor: null, total: 0 },
      },
    });

    const result = await listSessions({ limit: SESSION_PAGE_SIZE });

    expect(req.method).toBe("GET");
    expect(req.url).toBe("/api/v1/users/me/sessions?limit=20");
    expect(req.headers["x-csrf-token"]).toBeUndefined();
    expect(result.items).toEqual([]);
  });

  test("listSessions carries the cursor when one is supplied", async () => {
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: {
        items: [],
        pageInfo: { hasNextPage: false, endCursor: null, total: 0 },
      },
    });

    await listSessions({ limit: 20, after: "ss1.cursor" });

    expect(req.url).toBe("/api/v1/users/me/sessions?limit=20&after=ss1.cursor");
  });

  test("revokeSession sends the CSRF'd DELETE to the id path", async () => {
    setCSRFToken("tok");
    const { req } = mockFetchOnce({ ok: true, status: 204, jsonBody: "" });

    await revokeSession("s2");

    expect(req.method).toBe("DELETE");
    expect(req.url).toBe("/api/v1/users/me/sessions/s2");
    expect(req.headers["x-csrf-token"]).toBe("tok");
  });
});
