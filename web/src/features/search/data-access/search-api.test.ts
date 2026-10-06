import { afterEach, describe, expect, test } from "bun:test";

import { ApiError, NetworkError, ValidationError } from "@shared/api/client.js";
import { API_REQUEST_TIMEOUT_MS } from "@shared/api/request-signal.js";
import {
  mockFetchImpl,
  mockFetchOnce,
  restoreMockFetch,
  stubAbortSignalTimeout,
} from "@shared/api/test-utils.js";

import { searchContent } from "./search-api.js";
import type { SearchResultList } from "./types.js";

const samplePage: SearchResultList = {
  items: [
    {
      type: "project",
      id: "p1",
      title: "One Piece",
      subtitle: "",
      description: "Περιγραφή",
      thumbnailUrl: null,
      publishedAt: "2026-08-15T18:30:00.000Z",
      commentCount: 4,
      favoriteCount: 2,
    },
    {
      type: "post",
      id: "b1",
      title: "Ανάρτηση",
      subtitle: "Επεισόδιο 1.026",
      description: "Κείμενο",
      thumbnailUrl: "https://example.com/t.jpg",
      publishedAt: "2026-08-14T18:30:00.000Z",
      commentCount: 0,
      favoriteCount: 0,
    },
  ],
  pageInfo: { hasNextPage: false, endCursor: null, total: 2 },
};

/** The AbortSignal.timeout stub of the current test — restored in afterEach. */
let timeoutSpy: ReturnType<typeof stubAbortSignalTimeout> | null = null;

afterEach(() => {
  timeoutSpy?.restore();
  timeoutSpy = null;
  restoreMockFetch();
});

describe("searchContent", () => {
  test("returns the parsed page from GET /api/v1/search", async () => {
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: samplePage,
    });

    timeoutSpy = stubAbortSignalTimeout();
    const page = await searchContent({ q: "one piece" });

    expect(page.items[0]!.type).toBe("project");
    expect(page.items[0]!.thumbnailUrl).toBeNull();
    expect(req.method).toBe("GET");
    expect(req.url).toBe("/api/v1/search?q=one+piece");
    expect(req.signal).toBeInstanceOf(AbortSignal);
    // No caller signal — the shared client deadline is the default.
    expect(timeoutSpy?.spy.mock.calls[0]?.[0]).toBe(API_REQUEST_TIMEOUT_MS);
  });

  test("sends type, limit and after as query parameters", async () => {
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: samplePage,
    });

    await searchContent({
      q: "one piece",
      type: "projects",
      sort: "title",
      limit: 10,
      after: "st1.abc",
    });

    expect(req.url).toBe(
      "/api/v1/search?q=one+piece&type=projects&sort=title&limit=10&after=st1.abc",
    );
  });

  test("omits the sort parameter for the default ordering", async () => {
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: samplePage,
    });

    await searchContent({ q: "one piece", sort: "date" });

    expect(req.url).toBe("/api/v1/search?q=one+piece");
  });

  test("sends the from/to window when set and omits it when empty", async () => {
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: samplePage,
    });

    await searchContent({
      q: "one piece",
      from: "2025-01-01",
      to: "2025-01-31",
    });

    expect(req.url).toBe(
      "/api/v1/search?q=one+piece&from=2025-01-01&to=2025-01-31",
    );

    const { req: emptyReq } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: samplePage,
    });
    await searchContent({ q: "one piece", from: "", to: "" });
    expect(emptyReq.url).toBe("/api/v1/search?q=one+piece");
  });

  test("omits the type parameter when all is selected", async () => {
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: samplePage,
    });

    await searchContent({ q: "one piece", type: "all" });

    expect(req.url).toBe("/api/v1/search?q=one+piece");
  });

  test("URL-encodes Greek query text", async () => {
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: samplePage,
    });

    await searchContent({ q: "καλημέρα" });

    expect(req.url).toBe("/api/v1/search?q=" + encodeURIComponent("καλημέρα"));
  });

  test("throws ApiError on a problem response", async () => {
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

    await expect(searchContent({ q: "x" })).rejects.toBeInstanceOf(ApiError);
  });

  test("throws ValidationError on a 422 with violations", async () => {
    mockFetchOnce({
      ok: false,
      status: 422,
      headers: { "Content-Type": "application/problem+json" },
      jsonBody: {
        type: "/problems/validation",
        title: "Validation failed",
        status: 422,
        requestId: "rid",
        violations: [{ field: "q", code: "required" }],
      },
    });

    await expect(searchContent({ q: " " })).rejects.toBeInstanceOf(
      ValidationError,
    );
  });

  test("throws NetworkError when fetch itself fails", async () => {
    mockFetchImpl(async () => {
      throw new TypeError("failed to fetch");
    });

    await expect(searchContent({ q: "x" })).rejects.toBeInstanceOf(
      NetworkError,
    );
  });
});
