/**
 * Blog admin API suite — paths, methods, CSRF, and
 * the If-Match precondition headers. Transport semantics live in
 * shared/api/client.test.ts.
 */

import { afterEach, describe, expect, test } from "bun:test";

import { clearCSRFToken, setCSRFToken } from "@shared/api/client.js";
import { mockFetchOnce, restoreMockFetch } from "@shared/api/test-utils.js";

import {
  createBlogPost,
  deleteBlogPost,
  getStaffBlogPost,
  listStaffBlogPosts,
  updateBlogPost,
} from "./blog-admin-api.js";

afterEach(() => {
  clearCSRFToken();
  restoreMockFetch();
});

const BODY = {
  title: "T",
  subtitle: "",
  description: "",
  thumbnailPath: "media/images/ab/abcdef0123456789abcdef0123456789.jpg",
  status: "published" as const,
  downloads: [
    { resolution: "1080p", magnetUrl: "magnet:?xt=x", torrentUrl: null },
  ],
};

describe("blog admin API", () => {
  test("createBlogPost POSTs with CSRF and returns the staff shape", async () => {
    setCSRFToken("csrf-1");
    const { req } = mockFetchOnce({
      ok: true,
      status: 201,
      jsonBody: { id: "p1", revision: 1, thumbnailPath: BODY.thumbnailPath },
    });

    const result = await createBlogPost(BODY);

    expect(req.method).toBe("POST");
    expect(req.url).toBe("/api/v1/blog-posts");
    expect(req.headers["x-csrf-token"]).toBe("csrf-1");
    expect(result.id).toBe("p1");
  });

  test("listStaffBlogPosts GETs with limit/after", async () => {
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: {
        items: [],
        pageInfo: { hasNextPage: false, endCursor: null, total: 0 },
      },
    });

    await listStaffBlogPosts({ limit: 20, after: "av1.abc" });

    expect(req.method).toBe("GET");
    expect(req.url).toBe("/api/v1/admin/blog-posts?limit=20&after=av1.abc");
    expect(req.headers["x-csrf-token"]).toBeUndefined();
  });

  test("getStaffBlogPost GETs the staff detail", async () => {
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: { id: "p1", revision: 3 },
    });

    await getStaffBlogPost("p1");

    expect(req.url).toBe("/api/v1/admin/blog-posts/p1");
  });

  test("updateBlogPost PUTs with CSRF and the If-Match precondition", async () => {
    setCSRFToken("csrf-1");
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: { id: "p1", revision: 2 },
    });

    await updateBlogPost("p1", 1, BODY);

    expect(req.method).toBe("PUT");
    expect(req.url).toBe("/api/v1/blog-posts/p1");
    expect(req.headers["x-csrf-token"]).toBe("csrf-1");
    expect(req.headers["if-match"]).toBe('"1"');
  });

  test("deleteBlogPost DELETEs with CSRF and the If-Match precondition", async () => {
    setCSRFToken("csrf-1");
    const { req } = mockFetchOnce({ ok: true, status: 204, jsonBody: "" });

    await deleteBlogPost("p1", 2);

    expect(req.method).toBe("DELETE");
    expect(req.url).toBe("/api/v1/blog-posts/p1");
    expect(req.headers["x-csrf-token"]).toBe("csrf-1");
    expect(req.headers["if-match"]).toBe('"2"');
  });
});
