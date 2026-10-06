/**
 * Comments endpoint suite — paths, methods, the CSRF
 * knobs of the write functions, and the 201 Location parsing. Transport
 * semantics live in shared/api/client.test.ts.
 */

import { afterEach, describe, expect, test } from "bun:test";

import { clearCSRFToken, setCSRFToken } from "@shared/api/client.js";
import { mockFetchOnce, restoreMockFetch } from "@shared/api/test-utils.js";

import {
  commentCount,
  createComment,
  deleteComment,
  focusFromLocation,
  listComments,
  listReplies,
  removeHeart,
  replyToComment,
  setHeart,
  updateComment,
} from "./comments-api.js";

afterEach(() => {
  clearCSRFToken();
  restoreMockFetch();
});

describe("comments API", () => {
  test("listComments sends the sort/limit and optional after/focus params", async () => {
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: {
        items: [],
        pageInfo: { hasNextPage: false, endCursor: null, total: 0 },
      },
    });

    const result = await listComments("blog-posts", "b1", {
      sort: "newest",
      limit: 20,
      after: "cursor",
    });

    expect(req.method).toBe("GET");
    expect(req.url).toBe(
      "/api/v1/blog-posts/b1/comments?sort=newest&limit=20&after=cursor",
    );
    expect(req.headers["x-csrf-token"]).toBeUndefined();
    expect(result.pageInfo.hasNextPage).toBe(false);

    const { req: focusReq } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: {
        items: [],
        pageInfo: { hasNextPage: true, endCursor: "c2", total: 30 },
      },
    });
    await listComments("projects", "p1", {
      sort: "top",
      limit: 20,
      focus: "c1",
    });
    expect(focusReq.url).toBe(
      "/api/v1/projects/p1/comments?sort=top&limit=20&focus=c1",
    );
  });

  test("listReplies sends the ascending reply page with its own params", async () => {
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: {
        items: [],
        pageInfo: { hasNextPage: false, endCursor: null, total: 0 },
      },
    });

    await listReplies("blog-posts", "b1", "c1", {
      limit: 20,
      after: "br1.cursor",
    });

    expect(req.method).toBe("GET");
    expect(req.url).toBe(
      "/api/v1/blog-posts/b1/comments/c1/replies?limit=20&after=br1.cursor",
    );
    expect(req.headers["x-csrf-token"]).toBeUndefined();
  });

  test("commentCount sends a plain GET to the count path", async () => {
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: { count: 12 },
    });

    const result = await commentCount("projects", "p1");

    expect(req.method).toBe("GET");
    expect(req.url).toBe("/api/v1/projects/p1/comments/count");
    expect(result.count).toBe(12);
  });

  test("createComment POSTs the body with CSRF and parses the Location focus", async () => {
    setCSRFToken("tok");
    const { req } = mockFetchOnce({
      ok: true,
      status: 201,
      headers: {
        Location: "/api/v1/blog-posts/b1/comments?focus=newcomment1",
      },
      jsonBody: "",
    });

    const result = await createComment("blog-posts", "b1", "Γεια!");

    expect(req.method).toBe("POST");
    expect(req.url).toBe("/api/v1/blog-posts/b1/comments");
    expect(req.headers["x-csrf-token"]).toBe("tok");
    expect(req.body).toBe(JSON.stringify({ body: "Γεια!" }));
    expect(result.focusId).toBe("newcomment1");
  });

  test("replyToComment POSTs to the reply path with CSRF and parses the Location focus", async () => {
    setCSRFToken("tok");
    const { req } = mockFetchOnce({
      ok: true,
      status: 201,
      headers: {
        Location: "/api/v1/blog-posts/b1/comments?focus=reply9",
      },
      jsonBody: "",
    });

    const result = await replyToComment("blog-posts", "b1", "c1", "Απάντηση");

    expect(req.method).toBe("POST");
    expect(req.url).toBe("/api/v1/blog-posts/b1/comments/c1/replies");
    expect(req.headers["x-csrf-token"]).toBe("tok");
    expect(req.body).toBe(JSON.stringify({ body: "Απάντηση" }));
    expect(result.focusId).toBe("reply9");
  });

  test("updateComment PATCHes the body with CSRF and returns the item", async () => {
    setCSRFToken("tok");
    const item = {
      id: "c1",
      body: "Νέο κείμενο",
      author: {
        id: "u1",
        username: "Katakuri",
        avatarUrl: null,
        isStaff: false,
      },
      createdAt: "2026-08-28T10:00:00.000Z",
      updatedAt: "2026-08-28T11:00:00.000Z",
      heartsCount: 0,
      hearted: false,
    };
    const { req } = mockFetchOnce({ ok: true, status: 200, jsonBody: item });

    const result = await updateComment("blog-posts", "b1", "c1", "Νέο κείμενο");

    expect(req.method).toBe("PATCH");
    expect(req.url).toBe("/api/v1/blog-posts/b1/comments/c1");
    expect(req.headers["x-csrf-token"]).toBe("tok");
    expect(req.body).toBe(JSON.stringify({ body: "Νέο κείμενο" }));
    expect(result.updatedAt).toBe("2026-08-28T11:00:00.000Z");
  });

  test("deleteComment sends DELETE with CSRF", async () => {
    setCSRFToken("tok");
    const { req } = mockFetchOnce({ ok: true, status: 204, jsonBody: "" });

    await deleteComment("blog-posts", "b1", "c1");

    expect(req.method).toBe("DELETE");
    expect(req.url).toBe("/api/v1/blog-posts/b1/comments/c1");
    expect(req.headers["x-csrf-token"]).toBe("tok");
  });

  test("setHeart sends PUT with CSRF and removeHeart DELETE with CSRF", async () => {
    setCSRFToken("tok");
    const { req: put } = mockFetchOnce({ ok: true, status: 204, jsonBody: "" });
    await setHeart("blog-posts", "b1", "c1");
    expect(put.method).toBe("PUT");
    expect(put.url).toBe("/api/v1/blog-posts/b1/comments/c1/heart");
    expect(put.headers["x-csrf-token"]).toBe("tok");

    const { req: del } = mockFetchOnce({ ok: true, status: 204, jsonBody: "" });
    await removeHeart("blog-posts", "b1", "c1");
    expect(del.method).toBe("DELETE");
    expect(del.url).toBe("/api/v1/blog-posts/b1/comments/c1/heart");
    expect(del.headers["x-csrf-token"]).toBe("tok");
  });
});

describe("focusFromLocation", () => {
  test("extracts the focus id from a relative Location", () => {
    expect(focusFromLocation("/api/v1/blog-posts/b1/comments?focus=c42")).toBe(
      "c42",
    );
  });

  test("returns null for a missing Location", () => {
    expect(focusFromLocation(null)).toBeNull();
  });

  test("returns null for an unparseable Location", () => {
    expect(focusFromLocation("not a url at all")).toBeNull();
  });
});
