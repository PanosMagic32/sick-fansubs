/**
 * Staff metrics API suite — path, method, and the
 * promoted payload. Transport semantics live in shared/api/client.test.ts.
 */

import { afterEach, describe, expect, test } from "bun:test";

import { mockFetchOnce, restoreMockFetch } from "@shared/api/test-utils.js";

import { getStaffMetrics } from "./metrics-api.js";

afterEach(() => {
  restoreMockFetch();
});

describe("staff metrics API", () => {
  test("getStaffMetrics GETs /staff/metrics with no query parameters", async () => {
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: {
        totals: {
          users: {
            total: 4,
            byRole: { user: 2, moderator: 1, admin: 1, superAdmin: 0 },
            byStatus: { active: 3, suspended: 1 },
          },
          blogPosts: { total: 10, published: 7, draft: 2, archived: 1 },
          projects: { total: 3, published: 2, draft: 1, archived: 0 },
          comments: 5,
          favorites: 6,
        },
        activity30d: {
          since: "2026-08-18T09:00:00.000Z",
          registrations: 2,
          activeUsers: 3,
          signInFailures: 1,
          contentCreated: 4,
          contentUpdated: 5,
          contentDeleted: 0,
          commentDeletions: 1,
          usersSuspended: 1,
          usersReactivated: 0,
          usersDeleted: 0,
          roleChanges: 2,
        },
      },
    });

    const result = await getStaffMetrics();

    expect(req.method).toBe("GET");
    expect(req.url).toBe("/api/v1/staff/metrics");
    expect(result.totals.users.byRole.superAdmin).toBe(0);
    expect(result.totals.blogPosts.archived).toBe(1);
    expect(result.activity30d.since).toBe("2026-08-18T09:00:00.000Z");
    expect(result.activity30d.roleChanges).toBe(2);
  });
});
