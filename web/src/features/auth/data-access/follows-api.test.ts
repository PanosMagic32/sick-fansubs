/**
 * Follows endpoint suite — paths,
 * methods, and the CSRF knobs of the three follow functions (the
 * favorites mirror). Transport semantics live in
 * shared/api/client.test.ts.
 */

import { describe, expect, test, afterEach } from "bun:test";

import { clearCSRFToken, setCSRFToken } from "@shared/api/client.js";
import { mockFetchOnce, restoreMockFetch } from "@shared/api/test-utils.js";

import { addFollow, followStatus, removeFollow } from "./follows-api.js";

afterEach(() => {
  clearCSRFToken();
  restoreMockFetch();
});

describe("follows API", () => {
  test("followStatus sends GET to the id path", async () => {
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: { following: true },
    });

    const result = await followStatus("projects", "p1");

    expect(req.method).toBe("GET");
    expect(req.url).toBe("/api/v1/users/me/follows/projects/p1");
    expect(req.headers["x-csrf-token"]).toBeUndefined();
    expect(result.following).toBe(true);
  });

  test("addFollow sends PUT with CSRF", async () => {
    setCSRFToken("tok");
    const { req } = mockFetchOnce({ ok: true, status: 204, jsonBody: "" });

    await addFollow("blog-posts", "b1");

    expect(req.method).toBe("PUT");
    expect(req.url).toBe("/api/v1/users/me/follows/blog-posts/b1");
    expect(req.headers["x-csrf-token"]).toBe("tok");
  });

  test("removeFollow sends DELETE with CSRF", async () => {
    setCSRFToken("tok");
    const { req } = mockFetchOnce({ ok: true, status: 204, jsonBody: "" });

    await removeFollow("projects", "p1");

    expect(req.method).toBe("DELETE");
    expect(req.url).toBe("/api/v1/users/me/follows/projects/p1");
    expect(req.headers["x-csrf-token"]).toBe("tok");
  });
});
