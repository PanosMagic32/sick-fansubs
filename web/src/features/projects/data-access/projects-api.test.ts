import { afterEach, describe, expect, mock, test } from "bun:test";

import { ApiError, NetworkError, ValidationError } from "@shared/api/client.js";
import { API_REQUEST_TIMEOUT_MS } from "@shared/api/request-signal.js";
import {
  mockFetchImpl,
  mockFetchOnce,
  restoreMockFetch,
  stubAbortSignalTimeout,
} from "@shared/api/test-utils.js";
import { DEFAULT_PAGE_SIZE } from "@shared/utils/url-cursor-paging.js";

import { getProject, getProjects } from "./projects-api.js";
import type { ProjectDetail, ProjectList } from "./types.js";

const samplePage: ProjectList = {
  items: [
    {
      id: "pr1",
      title: "Τίτλος",
      description: "Περιγραφή",
      slug: "titlos",
      thumbnailUrl: "https://example.com/t.jpg",
      publishedAt: "2026-08-15T18:30:00.000Z",
      updatedAt: "2026-08-15T18:30:00.000Z",
      commentCount: 1,
      favoriteCount: 1,
      creator: null,
    },
  ],
  pageInfo: { hasNextPage: false, endCursor: null, total: 1 },
};

/** The AbortSignal.timeout spy of the current test — restored in afterEach. */
let timeoutSpy: ReturnType<typeof stubAbortSignalTimeout> | null = null;

afterEach(() => {
  timeoutSpy?.restore();
  timeoutSpy = null;
  mock.restore();
  restoreMockFetch();
});

describe("getProjects", () => {
  test("returns the parsed page from GET /api/v1/projects", async () => {
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: samplePage,
    });

    timeoutSpy = stubAbortSignalTimeout();
    const page = await getProjects();

    expect(page.items[0]!.id).toBe("pr1");
    expect(req.method).toBe("GET");
    expect(req.url).toBe("/api/v1/projects");
    expect(req.signal).toBeInstanceOf(AbortSignal);
    // No caller signal — the shared client deadline is the default.
    expect(timeoutSpy?.spy.mock.calls[0]?.[0]).toBe(API_REQUEST_TIMEOUT_MS);
  });

  test("sends limit and after as query parameters", async () => {
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: samplePage,
    });

    await getProjects({
      limit: DEFAULT_PAGE_SIZE,
      after: "pv1.abc",
    });

    // The cursor passes through verbatim — the pv1. namespace keeps
    // projects cursors from ever crossing into other endpoints.
    expect(req.url).toBe("/api/v1/projects?limit=10&after=pv1.abc");
  });

  test("omits the query string when no parameters are given", async () => {
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: samplePage,
    });

    await getProjects({});

    expect(req.url).toBe("/api/v1/projects");
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

    expect(getProjects()).rejects.toBeInstanceOf(ApiError);
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

    expect(getProjects({ limit: 101 })).rejects.toBeInstanceOf(ValidationError);
  });

  test("throws NetworkError when fetch itself fails", () => {
    mockFetchImpl(async () => {
      throw new TypeError("failed to fetch");
    });

    expect(getProjects()).rejects.toBeInstanceOf(NetworkError);
  });
});

// ── getProject ────────────────────────────────────────────────────

const sampleDetail: ProjectDetail = {
  id: "pr1",
  title: "Τίτλος",
  description: "Περιγραφή",
  slug: "titlos",
  thumbnailUrl: "https://example.com/t.jpg",
  publishedAt: "2026-08-15T18:30:00.000Z",
  updatedAt: "2026-08-20T10:00:00.000Z",
  commentCount: 1,
  favoriteCount: 1,
  creator: { id: "u1", username: "creator", avatarUrl: null },
  updater: null,
  downloads: [
    {
      name: "Batch 1",
      magnetUrl: "magnet:?xt=urn:btih:aaa",
      torrentUrl: null,
    },
  ],
};

describe("getProject", () => {
  test("returns the parsed project from GET /api/v1/projects/{id}", async () => {
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: sampleDetail,
    });

    timeoutSpy = stubAbortSignalTimeout();
    const project = await getProject("pr1");

    expect(project.title).toBe("Τίτλος");
    expect(project.creator?.username).toBe("creator");
    expect(req.method).toBe("GET");
    expect(req.url).toBe("/api/v1/projects/pr1");
    expect(req.signal).toBeInstanceOf(AbortSignal);
    expect(timeoutSpy?.spy.mock.calls[0]?.[0]).toBe(API_REQUEST_TIMEOUT_MS);
  });

  test("URL-encodes the id as a path segment", async () => {
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: sampleDetail,
    });

    await getProject("a b");

    expect(req.url).toBe("/api/v1/projects/a%20b");
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

    const err = await getProject("unknown").catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect((err as ApiError).type).toBe("/problems/not-found");
  });
});
