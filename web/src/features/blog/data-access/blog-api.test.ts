import { afterEach, describe, expect, test } from "bun:test";

import { ApiError, NetworkError, ValidationError } from "@shared/api/client.js";
import { API_REQUEST_TIMEOUT_MS } from "@shared/api/request-signal.js";
import {
  mockFetchImpl,
  mockFetchOnce,
  restoreMockFetch,
  stubAbortSignalTimeout,
} from "@shared/api/test-utils.js";
import { DEFAULT_PAGE_SIZE } from "@shared/utils/url-cursor-paging.js";

import { getBlogPost, listBlogPosts } from "./blog-api.js";
import type { BlogPostDetail, BlogPostList } from "./types.js";

const samplePage: BlogPostList = {
  items: [
    {
      id: "p1",
      title: "Τίτλος",
      subtitle: "Υπότιτλος",
      description: "Περιγραφή",
      thumbnailUrl: "https://example.com/t.jpg",
      publishedAt: "2026-08-15T18:30:00.000Z",
      updatedAt: "2026-08-15T18:30:00.000Z",
      commentCount: 3,
      favoriteCount: 2,
      creator: null,
    },
  ],
  pageInfo: { hasNextPage: false, endCursor: null, total: 1 },
};

/** The AbortSignal.timeout stub of the current test — restored in afterEach. */
let timeoutSpy: ReturnType<typeof stubAbortSignalTimeout> | null = null;

afterEach(() => {
  timeoutSpy?.restore();
  timeoutSpy = null;
  restoreMockFetch();
});

describe("listBlogPosts", () => {
  test("returns the parsed page from GET /api/v1/blog-posts", async () => {
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: samplePage,
    });

    timeoutSpy = stubAbortSignalTimeout();
    const page = await listBlogPosts();

    expect(page.items[0]!.id).toBe("p1");
    expect(req.method).toBe("GET");
    expect(req.url).toBe("/api/v1/blog-posts");
    expect(req.signal).toBeInstanceOf(AbortSignal);
    // No caller signal — the shared client deadline is the default.
    expect(timeoutSpy.spy.mock.calls[0]?.[0]).toBe(API_REQUEST_TIMEOUT_MS);
  });

  test("sends limit and after as query parameters", async () => {
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: samplePage,
    });

    await listBlogPosts({
      limit: DEFAULT_PAGE_SIZE,
      after: "v1.abc",
    });

    expect(req.url).toBe("/api/v1/blog-posts?limit=10&after=v1.abc");
  });

  test("omits the query string when no parameters are given", async () => {
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: samplePage,
    });

    await listBlogPosts({});

    expect(req.url).toBe("/api/v1/blog-posts");
  });

  test("throws ApiError on a problem response", () => {
    mockFetchOnce({
      ok: false,
      status: 400,
      headers: { "Content-Type": "application/problem+json" },
      jsonBody: {
        type: "/problems/bad-request",
        title: "Bad Request",
        status: 400,
        requestId: "rid",
      },
    });

    expect(listBlogPosts()).rejects.toBeInstanceOf(ApiError);
  });

  test("throws ValidationError on a 422 with violations", () => {
    mockFetchOnce({
      ok: false,
      status: 422,
      headers: { "Content-Type": "application/problem+json" },
      jsonBody: {
        type: "/problems/validation",
        title: "Validation failed",
        status: 422,
        requestId: "rid",
        violations: [{ field: "limit", code: "outOfRange" }],
      },
    });

    expect(listBlogPosts({ limit: 101 })).rejects.toBeInstanceOf(
      ValidationError,
    );
  });

  test("throws NetworkError when fetch itself fails", () => {
    mockFetchImpl(async () => {
      throw new TypeError("failed to fetch");
    });

    expect(listBlogPosts()).rejects.toBeInstanceOf(NetworkError);
  });
});

// ── Detail ───────────────────────────────────────────────────────

const sampleDetail: BlogPostDetail = {
  id: "p1",
  title: "Τίτλος",
  subtitle: "Υπότιτλος",
  description: "Περιγραφή",
  thumbnailUrl: "https://example.com/t.jpg",
  publishedAt: "2026-08-15T18:30:00.000Z",
  updatedAt: "2026-08-20T10:00:00.000Z",
  commentCount: 3,
  favoriteCount: 2,
  creator: { id: "u1", username: "creator", avatarUrl: null },
  updater: null,
  downloads: [
    {
      resolution: "1080p",
      magnetUrl: "magnet:?xt=urn:btih:aaa",
      torrentUrl: null,
    },
  ],
};

describe("getBlogPost", () => {
  test("returns the parsed post from GET /api/v1/blog-posts/{id}", async () => {
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: sampleDetail,
    });

    timeoutSpy = stubAbortSignalTimeout();
    const post = await getBlogPost("p1");

    expect(post.title).toBe("Τίτλος");
    expect(post.creator?.username).toBe("creator");
    expect(req.method).toBe("GET");
    expect(req.url).toBe("/api/v1/blog-posts/p1");
    expect(req.signal).toBeInstanceOf(AbortSignal);
    expect(timeoutSpy?.spy.mock.calls[0]?.[0]).toBe(API_REQUEST_TIMEOUT_MS);
  });

  test("URL-encodes the id as a path segment", async () => {
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: sampleDetail,
    });

    await getBlogPost("a b");

    expect(req.url).toBe("/api/v1/blog-posts/a%20b");
  });

  test("throws ApiError with the not-found type on a 404 problem", async () => {
    mockFetchOnce({
      ok: false,
      status: 404,
      headers: { "Content-Type": "application/problem+json" },
      jsonBody: {
        type: "/problems/not-found",
        title: "Not found",
        status: 404,
        requestId: "rid",
      },
    });

    const err = await getBlogPost("unknown").catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect((err as ApiError).type).toBe("/problems/not-found");
  });
});
