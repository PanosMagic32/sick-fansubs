/**
 * Staff user API suite — paths, methods, CSRF.
 * Transport semantics live in shared/api/client.test.ts.
 */

import { afterEach, describe, expect, test } from "bun:test";

import { clearCSRFToken, setCSRFToken } from "@shared/api/client.js";
import { mockFetchOnce, restoreMockFetch } from "@shared/api/test-utils.js";

import {
  changeUserRole,
  deleteUser,
  listStaffUsers,
  reactivateUser,
  resetUserPassword,
  suspendUser,
} from "./users-admin-api.js";

afterEach(() => {
  clearCSRFToken();
  restoreMockFetch();
});

describe("staff users API", () => {
  test("listStaffUsers GETs with limit and optional filters", async () => {
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: {
        items: [],
        pageInfo: { hasNextPage: false, endCursor: null, total: 0 },
      },
    });

    await listStaffUsers({ limit: 10, role: "moderator", status: "active" });

    expect(req.method).toBe("GET");
    expect(req.url).toBe("/api/v1/users?limit=10&role=moderator&status=active");
  });

  test("resetUserPassword POSTs with CSRF and returns the temp password", async () => {
    setCSRFToken("csrf-1");
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: { password: "AbcDef2345HjkLmn" },
    });

    const result = await resetUserPassword("u1");

    expect(req.method).toBe("POST");
    expect(req.url).toBe("/api/v1/users/u1/reset-password");
    expect(req.headers["x-csrf-token"]).toBe("csrf-1");
    expect(result.password).toBe("AbcDef2345HjkLmn");
  });

  test("suspendUser POSTs with CSRF and returns the updated projection", async () => {
    setCSRFToken("csrf-2");
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: {
        id: "u1",
        username: "Beta",
        email: "beta@example.com",
        role: "user",
        status: "suspended",
        createdAt: "2023-11-14T22:13:20.000Z",
      },
    });

    const result = await suspendUser("u1");

    expect(req.method).toBe("POST");
    expect(req.url).toBe("/api/v1/users/u1/suspend");
    expect(req.headers["x-csrf-token"]).toBe("csrf-2");
    expect(result.status).toBe("suspended");
  });

  test("reactivateUser POSTs with CSRF and returns the updated projection", async () => {
    setCSRFToken("csrf-3");
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: {
        id: "u2",
        username: "Beta",
        role: "user",
        status: "active",
        createdAt: "2023-11-14T22:13:20.000Z",
      },
    });

    const result = await reactivateUser("u2");

    expect(req.method).toBe("POST");
    expect(req.url).toBe("/api/v1/users/u2/reactivate");
    expect(req.headers["x-csrf-token"]).toBe("csrf-3");
    expect(result.status).toBe("active");
  });

  test("listStaffUsers forwards the after cursor", async () => {
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: {
        items: [],
        pageInfo: { hasNextPage: false, endCursor: null, total: 0 },
      },
    });

    await listStaffUsers({ limit: 10, after: "cursor-1" });

    expect(req.url).toBe("/api/v1/users?limit=10&after=cursor-1");
  });

  test("changeUserRole PATCHes the role with CSRF", async () => {
    setCSRFToken("csrf-4");
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: {
        id: "u1",
        username: "Beta",
        role: "moderator",
        status: "active",
        createdAt: "2023-11-14T22:13:20.000Z",
      },
    });

    const result = await changeUserRole("u1", "moderator");

    expect(req.method).toBe("PATCH");
    expect(req.url).toBe("/api/v1/users/u1/role");
    expect(req.headers["x-csrf-token"]).toBe("csrf-4");
    expect(req.body).toBe(JSON.stringify({ role: "moderator" }));
    expect(result.role).toBe("moderator");
  });

  test("deleteUser DELETEs with CSRF", async () => {
    setCSRFToken("csrf-5");
    const { req } = mockFetchOnce({ ok: true, status: 204, jsonBody: "" });

    await deleteUser("u2");

    expect(req.method).toBe("DELETE");
    expect(req.url).toBe("/api/v1/users/u2");
    expect(req.headers["x-csrf-token"]).toBe("csrf-5");
  });
});
