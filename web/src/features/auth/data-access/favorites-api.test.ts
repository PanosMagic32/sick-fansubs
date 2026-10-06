/**
 * Favorites endpoint suite — paths, methods, and
 * the CSRF knobs of the four favorites functions. Transport semantics live
 * in shared/api/client.test.ts.
 */

import { describe, expect, test, afterEach } from "bun:test";

import { clearCSRFToken, setCSRFToken } from "@shared/api/client.js";
import { mockFetchOnce, restoreMockFetch } from "@shared/api/test-utils.js";

import {
  addFavorite,
  favoriteStatus,
  listFavorites,
  removeFavorite,
} from "./favorites-api.js";

afterEach(() => {
  clearCSRFToken();
  restoreMockFetch();
});

const ITEM = {
  id: "b1",
  title: "B1",
  subtitle: "Sub B1",
  description: "blog one",
  thumbnailUrl: "https://fans.example/media/images/b1.jpg",
  publishedAt: "2023-11-14T22:13:20.000Z",
  favoritedAt: "2023-11-14T22:13:21.000Z",
};

describe("favorites API", () => {
  test("listFavorites sends GET with limit/after and returns the envelope", async () => {
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: {
        items: [ITEM],
        pageInfo: { hasNextPage: false, endCursor: null, total: 1 },
      },
    });

    const result = await listFavorites("blog-posts", {
      limit: 20,
      after: "cursor123",
    });

    expect(req.method).toBe("GET");
    expect(req.url).toBe(
      "/api/v1/users/me/favorites/blog-posts?limit=20&after=cursor123",
    );
    expect(req.headers["x-csrf-token"]).toBeUndefined();
    expect(result.items).toHaveLength(1);
    expect(result.items[0]?.subtitle).toBe("Sub B1");
    expect(result.pageInfo.hasNextPage).toBe(false);
  });

  test("favoriteStatus sends GET to the id path", async () => {
    const { req } = mockFetchOnce({
      ok: true,
      status: 200,
      jsonBody: { favorited: true },
    });

    const result = await favoriteStatus("projects", "p1");

    expect(req.method).toBe("GET");
    expect(req.url).toBe("/api/v1/users/me/favorites/projects/p1");
    expect(result.favorited).toBe(true);
  });

  test("addFavorite sends PUT with CSRF", async () => {
    setCSRFToken("tok");
    const { req } = mockFetchOnce({ ok: true, status: 204, jsonBody: "" });

    await addFavorite("blog-posts", "b1");

    expect(req.method).toBe("PUT");
    expect(req.url).toBe("/api/v1/users/me/favorites/blog-posts/b1");
    expect(req.headers["x-csrf-token"]).toBe("tok");
  });

  test("removeFavorite sends DELETE with CSRF", async () => {
    setCSRFToken("tok");
    const { req } = mockFetchOnce({ ok: true, status: 204, jsonBody: "" });

    await removeFavorite("projects", "p1");

    expect(req.method).toBe("DELETE");
    expect(req.url).toBe("/api/v1/users/me/favorites/projects/p1");
    expect(req.headers["x-csrf-token"]).toBe("tok");
  });
});
